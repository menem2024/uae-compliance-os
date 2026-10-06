"""`python -m ai.evals sample`: a small live accuracy measurement of the extraction path on a deterministic
stratified sample of `extraction-v1`.

Each case goes through the real agent code: the Extraction agent (one node through GraphExecutor, exactly as the
`extraction` suite runs it), scored by the binding field-accuracy scorer, then the production Verifier profile
(deterministic checks plus the critic where `critic_when` holds) for the accept/review decision. The gateway is
built through the gateway factory (`--gateway`), so `openai_compat` (Gemini) works. Nothing is recorded or
cached: the report carries case ids, field names, pass flags, verdicts and usage only, never document values.

Selection: cases are grouped by (lang, format, has-defect); each group is sorted by case id and the first
`n / groups` cases are taken, so the same dataset always yields the same sample.
"""

from __future__ import annotations

import asyncio
import json
import os
import sys
from collections import Counter, defaultdict
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field, replace
from pathlib import Path
from typing import Any

from ai.agents.extraction.schema import ExtractionOutput, flatten, leaf
from ai.agents.verifier.agent import VerifierAgent
from ai.agents.verifier.core import VerdictResult
from ai.agents.verifier.critic import InvoiceCritic
from ai.agents.verifier.invoice import invoice_profile
from ai.evals.core import CaseFailed, DatasetStale, DocRef, EvalCase, EvalEnv, run_node
from ai.evals.scoring import field_accuracy
from ai.evals.suites.extraction import ExtractionSuite, source_of
from ai.gateway.errors import TransientModelError
from ai.gateway.factory import build_gateway
from ai.gateway.types import ModelGateway
from ai.runtime.context import NodeContext
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.types import RunTotals, StepKind
from ai.settings import Settings

DEFAULT_OUT = Path("evals/reports")
BACKOFF_S = (30.0, 60.0, 120.0)  # case-level waits after a node exhausted its own retries on a transient error


def stratum(tags: frozenset[str]) -> tuple[str, str, bool]:
    lang = next(t for t in tags if t.startswith("lang:"))
    fmt = next(t for t in tags if t.startswith("format:"))
    return lang, fmt, any(t.startswith("defect:") for t in tags)


def select_sample(cases: Sequence[EvalCase[Any, Any]], n: int) -> list[EvalCase[Any, Any]]:
    """`n` cases spread evenly over the (lang, format, defect) strata, lowest case ids first; the result is
    sorted by case id. A stratum smaller than its share just gives what it has."""
    groups: dict[tuple[str, str, bool], list[EvalCase[Any, Any]]] = defaultdict(list)
    for c in sorted(cases, key=lambda c: c.case_id):
        groups[stratum(c.tags)].append(c)
    keys = sorted(groups)
    share, extra = divmod(n, len(keys))
    chosen: list[EvalCase[Any, Any]] = []
    for i, k in enumerate(keys):
        chosen.extend(groups[k][: share + (1 if i < extra else 0)])
    return sorted(chosen, key=lambda c: c.case_id)


@dataclass(slots=True)
class CaseResult:
    case_id: str
    tags: list[str]
    error: str = ""
    attempts: int = 1
    counted: int = 0
    correct: int = 0
    wrong_fields: list[str] = field(default_factory=list)  # field names only
    exact: bool = False
    verdict: str = ""  # accept | revise | escalate | "" (not verified)
    verifier_error: str = ""
    finding_codes: list[str] = field(default_factory=list)
    extraction_usage: RunTotals = field(default_factory=RunTotals)
    verifier_usage: RunTotals = field(default_factory=RunTotals)
    per_field: dict[str, tuple[int, int]] = field(default_factory=dict)  # leaf -> (counted, correct)


def field_outcomes(truth_flat: Mapping[str, str], pred_flat: Mapping[str, str]) -> dict[str, tuple[int, int]]:
    """Per field-set name (`lines[].net_amount` style, no row index) `(counted, correct)`, through the binding
    scorer applied one path at a time; the sum equals `score_invoices` on the same pair."""
    out: dict[str, list[int]] = defaultdict(lambda: [0, 0])
    for path in dict.fromkeys([*truth_flat, *pred_flat]):
        _, d = field_accuracy(truth_flat, pred_flat, [path])
        name = leaf(path)
        out[name][0] += int(d["counted"])  # type: ignore[call-overload]
        out[name][1] += int(d["correct"])  # type: ignore[call-overload]
    return {k: (v[0], v[1]) for k, v in out.items() if v[0]}


async def run_case(suite: ExtractionSuite, verifier: VerifierAgent, case: EvalCase[DocRef, Any],
                   env: EvalEnv, sleep: Any = asyncio.sleep) -> CaseResult:
    res = CaseResult(case.case_id, sorted(case.tags))
    output: ExtractionOutput | None = None
    for attempt in range(len(BACKOFF_S) + 1):
        hook = CollectingHook()
        res.attempts = attempt + 1
        try:
            output = await suite.run_case(case, env, hook)
            res.error = ""
        except CaseFailed as exc:
            res.error = exc.code
        except DatasetStale:
            raise
        except Exception as exc:  # noqa: BLE001 - one case error is a result, never an abort
            res.error = getattr(exc, "code", "internal")
        if hook.outcome is not None:
            res.extraction_usage = _add(res.extraction_usage, hook.outcome.totals)
        if not res.error:
            break
        if res.error != TransientModelError.code or attempt == len(BACKOFF_S):
            break
        await sleep(BACKOFF_S[attempt])
    truth_flat = flatten(case.truth)
    if output is None:
        res.per_field = field_outcomes(truth_flat, {})
        res.counted = sum(c for c, _ in res.per_field.values())
        res.wrong_fields = sorted(res.per_field)
        return res
    pred_flat = flatten(output.invoice)
    res.per_field = field_outcomes(truth_flat, pred_flat)
    res.counted = sum(c for c, _ in res.per_field.values())
    res.correct = sum(k for _, k in res.per_field.values())
    res.wrong_fields = sorted(f for f, (c, k) in res.per_field.items() if c != k)
    res.exact = res.counted == res.correct
    await _verify(res, verifier, case, output, env)
    return res


async def _verify(res: CaseResult, verifier: VerifierAgent, case: EvalCase[DocRef, Any], output: ExtractionOutput,
                  env: EvalEnv) -> None:
    evidence = source_of(case.input).parts()
    hook = CollectingHook()

    async def fn(ctx: NodeContext) -> VerdictResult:
        return await verifier.verify(ctx, output, evidence, revisions=0)

    try:
        verdict = await run_node(env, hook, suite="verifier", case_id=case.case_id, agent="verifier",
                                 action="verify", kind=StepKind.LLM, fn=fn)
        res.verdict = verdict.verdict
        res.finding_codes = sorted({f.code for f in verdict.findings})
    except CaseFailed as exc:
        res.verifier_error = exc.code
    except Exception as exc:  # noqa: BLE001
        res.verifier_error = getattr(exc, "code", "internal")
    if hook.outcome is not None:
        res.verifier_usage = hook.outcome.totals


def _add(a: RunTotals, b: RunTotals) -> RunTotals:
    return RunTotals(steps=a.steps + b.steps, llm_calls=a.llm_calls + b.llm_calls,
                     response_cache_hits=a.response_cache_hits + b.response_cache_hits,
                     input_tokens=a.input_tokens + b.input_tokens, output_tokens=a.output_tokens + b.output_tokens,
                     cost_micro_usd=a.cost_micro_usd + b.cost_micro_usd)


def _usage(rs: Sequence[CaseResult], which: str) -> dict[str, int]:
    us = [getattr(r, which) for r in rs]
    return {"llm_calls": sum(u.llm_calls for u in us), "input_tokens": sum(u.input_tokens for u in us),
            "output_tokens": sum(u.output_tokens for u in us)}


def _ratio(num: float, den: float) -> float | None:
    return round(num / den, 4) if den else None


def summarize(results: Sequence[CaseResult], *, gateway: str, models: Sequence[str], planned: int) -> dict[str, Any]:
    ok = [r for r in results if not r.error]
    counted, correct = sum(r.counted for r in results), sum(r.correct for r in results)
    by_field: dict[str, list[int]] = defaultdict(lambda: [0, 0])
    for r in results:
        for name, (c, k) in r.per_field.items():
            by_field[name][0] += c
            by_field[name][1] += k
    per_field = {n: {"counted": c, "correct": k, "accuracy": _ratio(k, c)} for n, (c, k) in sorted(by_field.items())}
    verified = [r for r in results if r.verdict]
    verdicts = Counter(r.verdict for r in verified)
    by_kind: dict[str, dict[str, Any]] = {}
    for kind, pick in (("clean", lambda r: not any(t.startswith("defect:") for t in r.tags)),
                       ("defect", lambda r: any(t.startswith("defect:") for t in r.tags))):
        sel = [r for r in verified if pick(r)]
        v = Counter(r.verdict for r in sel)
        by_kind[kind] = {"verified": len(sel), "accept": v["accept"], "revise": v["revise"],
                         "escalate": v["escalate"], "accept_rate": _ratio(v["accept"], len(sel))}
    by_tag: dict[str, dict[str, Any]] = {}
    for tag in sorted({t for r in results for t in r.tags if t.split(":")[0] in ("lang", "format")}):
        sel = [r for r in results if tag in r.tags]
        by_tag[tag] = {"cases": len(sel), "field_accuracy": _ratio(sum(r.correct for r in sel),
                                                                     sum(r.counted for r in sel)),
                       "exact_match_rate": _ratio(sum(r.exact for r in sel), len(sel))}
    eu, vu = _usage(results, "extraction_usage"), _usage(results, "verifier_usage")
    return {
        "gateway": gateway, "models": sorted(models), "cases_planned": planned, "cases_run": len(results),
        "cases_errored": len(results) - len(ok),
        "overall": {"field_accuracy": _ratio(correct, counted), "fields_counted": counted,
                    "fields_correct": correct,
                    "invoice_exact_match_rate": _ratio(sum(r.exact for r in results), len(results)),
                    "invoice_exact_match_rate_excluding_errors": _ratio(sum(r.exact for r in ok), len(ok))},
        "failure_causes": {"errors": dict(Counter(r.error for r in results if r.error)),
                           "cases_with_wrong_values": sum(1 for r in ok if not r.exact),
                           "retried_cases": sum(1 for r in results if r.attempts > 1)},
        "per_field": per_field, "per_tag": by_tag,
        "verifier": {"verified": len(verified), "verdicts": dict(verdicts),
                     "accept_rate": _ratio(verdicts["accept"], len(verified)),
                     "review_rate": _ratio(len(verified) - verdicts["accept"], len(verified)),
                     "verifier_errors": dict(Counter(r.verifier_error for r in results if r.verifier_error)),
                     "by_kind": by_kind,
                     "finding_codes": dict(Counter(c for r in verified for c in r.finding_codes))},
        "usage": {"extraction": eu, "verifier": vu,
                  "total": {k: eu[k] + vu[k] for k in eu}},
        "cases": [{"case_id": r.case_id, "pass": r.exact, "error": r.error, "attempts": r.attempts,
                   "fields_correct": r.correct, "fields_counted": r.counted, "wrong_fields": r.wrong_fields,
                   "verdict": r.verdict, "verifier_error": r.verifier_error, "finding_codes": r.finding_codes,
                   "tags": r.tags} for r in results],
    }


def to_markdown(s: Mapping[str, Any]) -> str:
    o, v, u = s["overall"], s["verifier"], s["usage"]
    pct = lambda x: "n/a" if x is None else f"{x * 100:.1f}%"
    lines = [f"# Extraction accuracy sample ({s['gateway']})", "",
             (f"Models: {', '.join(s['models']) or 'n/a'}. Cases run {s['cases_run']} of {s['cases_planned']} "
              f"planned, {s['cases_errored']} errored."), "",
             ("Synthetic dataset `extraction-v1`; stratified by (lang, format, defect), lowest case ids first. "
              "Only case ids, field names and pass flags are recorded."), "",
             "## Overall", "",
             f"- Field accuracy: {pct(o['field_accuracy'])} ({o['fields_correct']}/{o['fields_counted']})",
             (f"- Invoice exact match: {pct(o['invoice_exact_match_rate'])} "
              f"(excluding errored cases: {pct(o['invoice_exact_match_rate_excluding_errors'])})"),
             (f"- Verifier: accept {pct(v['accept_rate'])}, review (revise or escalate) {pct(v['review_rate'])} "
              f"over {v['verified']} verified; verdicts {v['verdicts']}"),
             (f"- Calls and tokens: {u['total']['llm_calls']} calls, {u['total']['input_tokens']} in, "
              f"{u['total']['output_tokens']} out (extraction {u['extraction']['llm_calls']} calls, "
              f"verifier {u['verifier']['llm_calls']} calls)"), "",
             "## By language and format", "", "| tag | cases | field accuracy | exact match |", "|---|---|---|---|"]
    lines += [f"| {t} | {d['cases']} | {pct(d['field_accuracy'])} | {pct(d['exact_match_rate'])} |"
              for t, d in s["per_tag"].items()]
    lines += ["", "## Per field", "", "| field | correct | counted | accuracy |", "|---|---|---|---|"]
    lines += [f"| {n} | {d['correct']} | {d['counted']} | {pct(d['accuracy'])} |" for n, d in s["per_field"].items()]
    lines += ["", "## Verifier by kind", "", "| kind | verified | accept | revise | escalate |", "|---|---|---|---|---|"]
    lines += [f"| {k} | {d['verified']} | {d['accept']} | {d['revise']} | {d['escalate']} |"
              for k, d in v["by_kind"].items()]
    lines += ["", "## Failure causes", "", f"- Errors: {s['failure_causes']['errors'] or 'none'}",
              f"- Cases with wrong values (no error): {s['failure_causes']['cases_with_wrong_values']}",
              f"- Cases retried after a transient error: {s['failure_causes']['retried_cases']}", "",
              "## Cases", "", "| case | pass | error | verdict | wrong fields |", "|---|---|---|---|---|"]
    lines += [f"| {c['case_id']} | {'yes' if c['pass'] else 'no'} | {c['error'] or '-'} | {c['verdict'] or '-'} | "
              f"{', '.join(c['wrong_fields']) or '-'} |" for c in s["cases"]]
    return "\n".join(lines) + "\n"


async def run_sample(suite: ExtractionSuite, cases: Sequence[EvalCase[DocRef, Any]], gateway: ModelGateway,
                     settings: Settings, concurrency: int, sleep: Any = asyncio.sleep) -> list[CaseResult]:
    verifier = VerifierAgent([invoice_profile(critic=InvoiceCritic(model=settings.model_critic))])
    env = EvalEnv(gateway, "live", concurrency)
    sem = asyncio.Semaphore(concurrency)

    async def one(c: EvalCase[DocRef, Any]) -> CaseResult:
        async with sem:
            return await run_case(suite, verifier, c, env, sleep)

    return list(await asyncio.gather(*(one(c) for c in cases)))


def main(args: Any, settings: Settings | None = None, environ: Mapping[str, str] | None = None) -> int:
    from ai.evals.cli import LIVE_KEY_ENV  # local: cli imports this module's subcommand

    env = os.environ if environ is None else environ
    key_env = LIVE_KEY_ENV[args.gateway]
    if env.get("EVALS_LIVE_CONFIRM") != "1" or not env.get(key_env):
        print(f"evals: sample needs EVALS_LIVE_CONFIRM=1 and {key_env} in the environment", file=sys.stderr)
        return 1
    base = settings or Settings.from_env()
    settings = replace(base, gateway=args.gateway, cache="none", max_concurrent_llm_calls=args.concurrency)  # type: ignore[type-var]
    suite = ExtractionSuite(settings)
    cases = select_sample(list(suite.cases("full")), args.n)
    gateway = build_gateway(settings)  # the factory: provider -> spend cap -> concurrency limit
    results = asyncio.run(run_sample(suite, cases, gateway, settings, args.concurrency))
    models = {settings.openai_model_fast, settings.openai_model_smart} if args.gateway == "openai_compat" else set()
    summary = summarize(results, gateway=args.gateway, models=sorted(models), planned=len(cases))
    out: Path = args.out_dir / args.gateway
    out.mkdir(parents=True, exist_ok=True)
    stem = f"extraction-sample{len(cases)}"
    (out / f"{stem}.json").write_text(json.dumps(summary, indent=1, sort_keys=True, ensure_ascii=False) + "\n",
                                      encoding="utf-8")
    (out / f"{stem}.md").write_text(to_markdown(summary), encoding="utf-8")
    o = summary["overall"]
    print(f"sample {args.gateway}: cases={summary['cases_run']} errors={summary['cases_errored']} "
          f"field_accuracy={o['field_accuracy']} exact={o['invoice_exact_match_rate']} -> {out / (stem + '.md')}")
    return 0
