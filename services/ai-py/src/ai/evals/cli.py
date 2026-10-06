"""`uv run python -m ai.evals run <suite>|--all --mode auto|fake|replay|live [--subset pr|full] [--max-cost-usd N]`.

Exits 1 when any threshold fails or a recording is missing in replay mode; writes
`evals/reports/<suite>-<mode>.json`.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
import tomllib
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import asdict, dataclass, replace
from importlib.metadata import entry_points
from pathlib import Path
from typing import Any, Literal

from ai.evals.core import Suite
from ai.evals.runner import (
    DEFAULT_MAX_COST_USD,
    MICRO,
    LiveNotConfirmed,
    LiveSpendCapExceeded,
    Report,
    run_suite,
)
from ai.gateway.factory import provider_gateway
from ai.gateway.limits import MemorySpendLimiter
from ai.settings import Settings

EVALS_GROUP = "compliance.evals"
DEFAULT_THRESHOLDS = Path("evals/thresholds.toml")
DEFAULT_REPORTS = Path("evals/reports")
LIVE_KEY_ENV = {"anthropic": "ANTHROPIC_API_KEY", "openai_compat": "AI_OPENAI_API_KEY"}
_OPS = {"min": ">=", "max": "<=", "eq": "=="}


@dataclass(frozen=True, slots=True)
class Threshold:
    suite: str
    metric: str
    op: Literal[">=", "<=", "=="]
    value: float
    tags: tuple[str, ...] = ()  # () = the suite total; otherwise each listed tag's value must satisfy it

    def holds(self, actual: float) -> bool:
        match self.op:
            case ">=":
                return actual >= self.value
            case "<=":
                return actual <= self.value
            case _:
                return actual == self.value


def _rule(suite: str, metric: str, table: Mapping[str, Any], tags: tuple[str, ...]) -> Threshold:
    ops = [k for k in _OPS if k in table]
    if len(ops) != 1:
        raise ValueError(f"[{suite}.{metric}]: exactly one of min, max, eq is required")
    return Threshold(suite, metric, _OPS[ops[0]], float(table[ops[0]]), tags)  # type: ignore[arg-type]


def load_thresholds(path: Path = DEFAULT_THRESHOLDS) -> list[Threshold]:
    """`[<suite>.<metric>]` with `min`, `max` or `eq`; an optional `[<suite>.<metric>.per_tag]` adds the same
    kind of rule over a list of `tags`."""
    out: list[Threshold] = []
    for suite, metrics in tomllib.loads(path.read_text(encoding="utf-8")).items():
        for metric, table in metrics.items():
            out.append(_rule(suite, metric, {k: v for k, v in table.items() if k in _OPS}, ()))
            if "per_tag" in table:
                out.append(_rule(suite, metric, table["per_tag"], tuple(table["per_tag"]["tags"])))
    return out


def failures(report: Report, thresholds: Iterable[Threshold]) -> list[str]:
    """Human-readable threshold misses of one report (empty when it passes); a skipped report has none."""
    if report.status == "skipped":
        return []
    out: list[str] = []
    for t in thresholds:
        if t.suite != report.suite:
            continue
        values = ({tag: report.per_tag.get(tag, {}).get(t.metric) for tag in t.tags} if t.tags
                  else {"total": report.totals.get(t.metric)})
        for where, actual in values.items():
            if actual is None:
                out.append(f"{t.suite}.{t.metric} [{where}] is missing (needs {t.op} {t.value})")
            elif not t.holds(actual):
                out.append(f"{t.suite}.{t.metric} [{where}] = {actual:.4f}, needs {t.op} {t.value}")
    if report.mode == "replay" and report.missing_recordings:
        out.append(f"{report.suite}: {report.missing_recordings} recording(s) missing; run `make evals-live`")
    return out


def exit_code(reports: Iterable[Report], thresholds: Sequence[Threshold]) -> int:
    return 1 if any(failures(r, thresholds) for r in reports) else 0


def load_suites(eps: Iterable[Any] | None = None) -> dict[str, Suite]:  # type: ignore[type-arg]
    found = list(eps) if eps is not None else list(entry_points(group=EVALS_GROUP))
    return {ep.name: ep.load()() for ep in sorted(found, key=lambda e: e.name)}


def write_report(report: Report, reports_dir: Path) -> Path:
    reports_dir.mkdir(parents=True, exist_ok=True)
    path = reports_dir / f"{report.suite}-{report.mode}.json"
    path.write_text(json.dumps(asdict(report), indent=1, sort_keys=True, ensure_ascii=False) + "\n",
                    encoding="utf-8")
    return path


def _summary(r: Report) -> str:
    if r.status == "skipped":
        return f"{r.suite:<10} {r.mode:<6} {r.subset:<4} SKIPPED (no fake_script)"
    totals = " ".join(f"{k}={v:.4f}" for k, v in sorted(r.totals.items()))
    return (f"{r.suite:<10} {r.mode:<6} {r.subset:<4} cases={r.cases} {totals} errors={r.errors} "
            f"eligible={str(r.exit_criterion_eligible).lower()}")


def live_provider_args(gateway: str, settings: Settings) -> dict[str, Any]:
    """`run_suite` keyword arguments for the live provider. `anthropic` (the default) changes nothing; any
    other provider is built through the gateway factory, with the API key it needs checked up front."""
    if gateway == "anthropic":
        return {}
    chosen = replace(settings, gateway=gateway)  # type: ignore[type-var]
    return {"provider": lambda: provider_gateway(chosen), "key_env": LIVE_KEY_ENV[gateway]}


async def _run_all(suites: Mapping[str, Suite], args: argparse.Namespace,  # type: ignore[type-arg]
                   settings: Settings) -> list[Report]:
    limiter = MemorySpendLimiter(int(args.max_cost_usd * MICRO))  # one cap for the whole invocation
    extra = live_provider_args(args.gateway, settings)
    return [await run_suite(s, mode=args.mode, subset=args.subset, settings=settings,
                            max_cost_usd=args.max_cost_usd, limiter=limiter, **extra) for s in suites.values()]


def main(argv: Sequence[str] | None = None, *, suites: Mapping[str, Suite] | None = None,  # type: ignore[type-arg]
         settings: Settings | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m ai.evals")
    sub = p.add_subparsers(dest="cmd", required=True)
    r = sub.add_parser("run", help="run eval suites and check thresholds")
    r.add_argument("suite", nargs="?", help="suite name (see the compliance.evals entry points)")
    r.add_argument("--all", action="store_true", help="run every registered suite")
    r.add_argument("--mode", choices=("auto", "fake", "replay", "live"), default="auto")
    r.add_argument("--gateway", choices=tuple(LIVE_KEY_ENV), default="anthropic",
                   help="provider for --mode live, built through the gateway factory (default: anthropic)")
    r.add_argument("--subset", choices=("pr", "full"), default="full")
    r.add_argument("--max-cost-usd", type=float, default=DEFAULT_MAX_COST_USD)
    r.add_argument("--thresholds", type=Path, default=DEFAULT_THRESHOLDS)
    r.add_argument("--reports-dir", type=Path, default=DEFAULT_REPORTS)
    sm = sub.add_parser("sample", help="live accuracy measurement on a stratified sample of extraction-v1")
    sm.add_argument("--gateway", choices=tuple(LIVE_KEY_ENV), default="openai_compat")
    sm.add_argument("--n", type=int, default=24, help="cases in the sample (default 24)")
    sm.add_argument("--concurrency", type=int, default=2)
    sm.add_argument("--out-dir", type=Path, default=DEFAULT_REPORTS)
    args = p.parse_args(argv)
    if args.cmd == "sample":
        from ai.evals.sample import main as sample_main

        return sample_main(args, settings)
    if bool(args.suite) == bool(args.all):
        p.error("give exactly one of <suite> or --all")
    registry = dict(suites) if suites is not None else load_suites()
    if args.all:
        chosen = registry
    elif args.suite in registry:
        chosen = {args.suite: registry[args.suite]}
    else:
        p.error(f"unknown suite {args.suite!r}; registered: {', '.join(sorted(registry)) or 'none'}")
    thresholds = load_thresholds(args.thresholds)
    settings = settings or Settings.from_env()
    if args.gateway != "anthropic":  # never mix another model's recordings or reports into the Anthropic ones
        settings = replace(settings, recordings_dir=str(Path(settings.recordings_dir) / args.gateway))
        if args.reports_dir == DEFAULT_REPORTS:
            args.reports_dir = DEFAULT_REPORTS / args.gateway
    try:
        reports = asyncio.run(_run_all(chosen, args, settings))
    except (LiveNotConfirmed, LiveSpendCapExceeded) as exc:
        print(f"evals: {exc}", file=sys.stderr)
        return 1
    for rep in reports:
        write_report(rep, args.reports_dir)
        print(_summary(rep))
        for line in failures(rep, thresholds):
            print(f"  FAIL {line}", file=sys.stderr)
    return exit_code(reports, thresholds)
