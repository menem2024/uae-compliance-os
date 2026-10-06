"""Eval runner: resolves the mode, runs every case through the suite (and so through GraphExecutor), aggregates a
Report. `exit_criterion_eligible` is computed here from what actually ran; nothing can set it from outside."""

from __future__ import annotations

import asyncio
import os
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field, replace
from pathlib import Path
from typing import Literal

from ai.evals.core import CaseFailed, CaseScore, DatasetStale, EvalCase, EvalEnv, Mode, Subset, Suite
from ai.evals.manifest import load_manifest, matches_current, prompt_versions, write_manifest
from ai.evals.scoring import micro_average
from ai.gateway.anthropic_gw import make_gateway
from ai.gateway.errors import RecordingMissing, SpendCapExceeded
from ai.gateway.fake import FakeGateway
from ai.gateway.limits import ConcurrencyLimitedGateway, MemorySpendLimiter, SpendLimitedGateway
from ai.gateway.recorded import RecordingGateway, ReplayGateway
from ai.gateway.types import ModelGateway
from ai.runtime.evalhooks import CollectingHook
from ai.settings import Settings

type RunMode = Literal["auto", "fake", "replay", "live"]

DEFAULT_MAX_COST_USD = 10.0
MICRO = 1_000_000


class LiveNotConfirmed(Exception):
    """`live` needs EVALS_LIVE_CONFIRM=1 and the provider's API key (ANTHROPIC_API_KEY by default); nothing is built or called without them."""


class LiveSpendCapExceeded(Exception):
    """The dedicated live spend limiter reached --max-cost-usd; the run stopped."""


@dataclass(frozen=True, slots=True)
class Report:
    suite: str
    mode: str  # the effective mode: fake, replay or live ("auto" resolves to fake or replay)
    subset: str
    totals: dict[str, float]
    per_tag: dict[str, dict[str, float]]
    cases: int
    exit_criterion_eligible: bool
    status: Literal["ok", "skipped"] = "ok"
    requested_mode: str = ""
    errors: int = 0
    missing_recordings: int = 0
    failed_cases: list[str] = field(default_factory=list)  # case ids and codes only, never model output
    prompt_versions: dict[str, int] = field(default_factory=dict)
    models: list[str] = field(default_factory=list)
    usage: dict[str, int] = field(default_factory=dict)


def live_stack(inner: ModelGateway, recordings: Path, limiter: MemorySpendLimiter,
               concurrency: int) -> ModelGateway:
    """The live gateway: provider -> dedicated spend cap -> recording -> concurrency limit. Each call reserves
    its upper-bound output cost before it runs and raises (SpendCapExceeded) once the cap is reached, so the
    overshoot is at most one reservation plus the input cost of the calls in flight."""
    return ConcurrencyLimitedGateway(RecordingGateway(SpendLimitedGateway(inner, limiter), recordings),
                                     concurrency)


def _require_live(env: Mapping[str, str], key_env: str = "ANTHROPIC_API_KEY") -> None:
    if env.get("EVALS_LIVE_CONFIRM") != "1" or not env.get(key_env):
        raise LiveNotConfirmed(f"live mode needs EVALS_LIVE_CONFIRM=1 and {key_env} in the environment")


def _code_of(exc: BaseException) -> str:
    code = getattr(exc, "code", None)
    return code if isinstance(code, str) and code else "internal"


async def run_suite(suite: Suite, *, mode: RunMode, subset: Subset, settings: Settings,  # type: ignore[type-arg]
                    max_cost_usd: float = DEFAULT_MAX_COST_USD, env: Mapping[str, str] | None = None,
                    provider: Callable[[], ModelGateway] = make_gateway,
                    limiter: MemorySpendLimiter | None = None,
                    key_env: str = "ANTHROPIC_API_KEY") -> Report:
    environ = os.environ if env is None else env
    root = Path(settings.recordings_dir)
    versions = prompt_versions(suite.prompt_ids)
    manifest = load_manifest(root, suite.name)
    effective: Mode
    if mode == "auto":
        effective = "replay" if matches_current(manifest, versions) else "fake"
    else:
        effective = mode
    fake_script = getattr(suite, "fake_script", None)
    if effective == "fake" and fake_script is None:
        return Report(suite.name, "fake", subset, {}, {}, 0, False, status="skipped", requested_mode=mode,
                      prompt_versions=versions)

    live: ModelGateway | None = None
    if effective == "live":
        _require_live(environ, key_env)  # before anything is built
        limiter = limiter or MemorySpendLimiter(int(max_cost_usd * MICRO))
        live = live_stack(provider(), root / suite.name, limiter, settings.max_concurrent_llm_calls)
    replay = ReplayGateway(root / suite.name) if effective == "replay" else None

    sem = asyncio.Semaphore(settings.max_concurrent_llm_calls)
    stop = asyncio.Event()
    fatal: list[BaseException] = []
    results: list[tuple[CaseScore, CollectingHook] | None] = []

    async def one(case: EvalCase) -> tuple[CaseScore, CollectingHook] | None:  # type: ignore[type-arg]
        async with sem:
            if stop.is_set():
                return None
            gateway: ModelGateway = (FakeGateway(fake_script(case)) if effective == "fake"  # type: ignore[misc]
                                     else live or replay)  # type: ignore[assignment]
            hook = CollectingHook()
            code = ""
            output: object = None
            try:
                output = await suite.run_case(case, EvalEnv(gateway, effective, settings.max_concurrent_llm_calls),
                                              hook)
            except DatasetStale as exc:
                fatal.append(exc)
                stop.set()
                return None
            except CaseFailed as exc:
                code = exc.code
            except RecordingMissing:
                code = RecordingMissing.code
            except Exception as exc:  # noqa: BLE001 - a case error scores zero, it never aborts the suite
                code = _code_of(exc)
            if code == SpendCapExceeded.code and effective == "live":
                stop.set()
            if code:
                on_error = getattr(suite, "error_score", None)
                score = (on_error(case, code) if on_error else
                         CaseScore(case.case_id, case.tags, {}, {}, error=code))
            else:
                score = suite.score(case, output)
            if hook.outcome is not None:
                score = replace(score, usage=hook.outcome.totals)
            return score, hook

    results = list(await asyncio.gather(*(one(c) for c in suite.cases(subset))))
    if fatal:
        raise fatal[0]
    if stop.is_set():
        raise LiveSpendCapExceeded(f"live spend cap of USD {max_cost_usd} reached; run stopped")
    done = [r for r in results if r is not None]
    scores = [s for s, _ in done]
    missing = sum(1 for s in scores if s.error == RecordingMissing.code)

    if effective == "live":
        write_manifest(root, suite.name, versions)
    eligible = (effective in ("replay", "live") and subset == "full" and missing == 0
                and (effective == "live" or matches_current(manifest, versions)))
    metrics = sorted({m for s in scores for m in s.metrics if "#" not in m})
    tags = sorted({t for s in scores for t in s.tags})
    per_tag = {t: {m: micro_average([s for s in scores if t in s.tags], m)
                   for m in metrics if any(t in s.tags and m in s.metrics for s in scores)} for t in tags}
    return Report(
        suite=suite.name, mode=effective, subset=subset,
        totals={m: micro_average(scores, m) for m in metrics}, per_tag=per_tag, cases=len(scores),
        exit_criterion_eligible=eligible, requested_mode=mode,
        errors=sum(1 for s in scores if s.error), missing_recordings=missing,
        failed_cases=sorted(f"{s.case_id}:{s.error}" for s in scores if s.error)[:50],
        prompt_versions=versions,
        models=sorted({req.model for _, h in done for req, _ in h.calls}),
        usage={"input_tokens": sum(s.usage.input_tokens for s in scores),
               "output_tokens": sum(s.usage.output_tokens for s in scores),
               "llm_calls": sum(s.usage.llm_calls for s in scores),
               "cost_micro_usd": sum(s.usage.cost_micro_usd for s in scores)})
