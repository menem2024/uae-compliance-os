"""The five nodes of `fix_invoice@1` (spec 5.7.2) and the pure helpers behind them.

The agent only PROPOSES. Every candidate is re-validated by validator-rs through the `validate_invoice` tool
before a proposal is emitted, and a candidate that adds an error rule id, does not reduce the error count, or
leaves every target issue standing is never proposed. A model never decides validity (ADR 006).
"""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass

from ai.agents.fix import paths
from ai.agents.fix.agent import TOOL_NAME, FixAgent
from ai.agents.fix.config import FixConfig
from ai.agents.fix.convert import error_issues, invoice_json, run_from_json
from ai.agents.fix.guards import guard_changes, llm_input, llm_targets_of_run
from ai.agents.fix.types import (
    Change,
    ChangeSet,
    DeterministicResult,
    FixCandidate,
    LLMResult,
    ProposeResult,
    VerifiedResult,
)
from ai.agents.verifier.agent import VerifierAgent
from ai.gateway.types import ToolCall
from ai.gen.compliance.v1 import invoice_pb2, validator_pb2
from ai.runtime.context import NodeContext
from ai.runtime.errors import AgentRuntimeError, ToolTransientError
from ai.runtime.proposals import FieldChangeDraft, ProposalDraft

NODE_DETERMINISTIC = "fix.deterministic"
NODE_LLM = "fix.llm"
NODE_VERIFY = "fix.verify"
NODE_PROPOSE = "fix.propose"
NODE_RESULT = "fix.result"

MAX_ROUNDS = 3  # the totals chain IBT-106 -> 109 -> 112 -> 115 settles in at most 3
MAX_EVALUATIONS = 21  # one with every LLM change, then one per change dropped (at most 20 changes)
RATIONALE_CHARS = 1000

PROPOSAL_KIND = "invoice.field_fix"
SUMMARY_KEY = "P2Review.proposal.summary"

# Feed lines (agent-runtime contract section 7.7): the namespace is P2Agents.feed.fix.*, args are counts only.
FEED = "P2Agents.feed.fix."
FEED_DETERMINISTIC = FEED + "deterministic_applied"  # {count}
FEED_LLM = FEED + "llm_requested"
FEED_VERIFIED = FEED + "verified"  # {before, after}
FEED_PROPOSED = FEED + "proposed"  # {changes}
FEED_NO_FIX = FEED + "no_fix"


class ValidatorRejected(AgentRuntimeError):
    """validator-rs refused the request (INVALID_ARGUMENT, for example an unknown RuleSet version)."""

    code = "bad_request"


async def validate(ctx: NodeContext, inv: invoice_pb2.Invoice, ruleset_version: str) -> validator_pb2.ValidationRun:
    """Re-validates `inv` through the `validate_invoice` tool. A transient validator error propagates as
    ToolTransientError (the node's RetryPolicy retries); a rejected request fails the task as bad_request."""
    part = await ctx.tools.invoke(ctx, ToolCall(
        ctx.step_id, TOOL_NAME, {"invoice_json": invoice_json(inv), "ruleset_version": ruleset_version}))
    if part.is_error:
        if part.content.startswith("TimeoutError"):
            raise ToolTransientError("validate_invoice timed out")
        raise ValidatorRejected("validate_invoice failed")
    return run_from_json(part.content)


# ----------------------------------------------------------------------------- deterministic suggestions
@dataclass(frozen=True, slots=True)
class Suggestion:
    path: str
    value: str
    rule_ids: tuple[str, ...]
    rationale: str


def suggestions(run: validator_pb2.ValidationRun, inv: invoice_pb2.Invoice) -> list[Suggestion]:
    """The new value each error issue computes for its path: distinct paths (the first value wins), AgentForbidden
    and invalid paths skipped, a value the field already has skipped."""
    found: dict[str, Suggestion] = {}
    for i in error_issues(run):
        value = i.suggested_value
        if (not value or not paths.is_valid_path(i.path) or paths.is_forbidden(i.path)
                or (paths.is_bool_path(i.path) and value not in ("true", "false"))):
            continue
        prev = found.get(i.path)
        if prev is not None:
            if prev.value == value and i.rule_id not in prev.rule_ids:
                found[i.path] = Suggestion(i.path, value, (*prev.rule_ids, i.rule_id), prev.rationale)
            continue
        if paths.get_value(inv, i.path) != value:
            found[i.path] = Suggestion(i.path, value, (i.rule_id,), i.message[:200])
    return list(found.values())


async def settle(ctx: NodeContext, ruleset_version: str, inv: invoice_pb2.Invoice,
                 run: validator_pb2.ValidationRun, changes: ChangeSet) -> validator_pb2.ValidationRun:
    """Applies the run's suggestions to `inv` (in place) and re-validates, until none is new or MAX_ROUNDS rounds
    are used. Suggestions are computed from the inputs of the moment, so chained rules settle round by round.
    Returns the validation of the final `inv`."""
    for _ in range(MAX_ROUNDS):
        todo = suggestions(run, inv)
        if not todo:
            break
        for s in todo:
            changes.record(s.path, paths.get_value(inv, s.path), s.value, s.rule_ids, "deterministic",
                           s.rationale)
            paths.set_value(inv, s.path, s.value)
        run = await validate(ctx, inv, ruleset_version)
    return run


async def deterministic(ctx: NodeContext, inp: validator_pb2.FixTaskInput) -> DeterministicResult:
    inv = paths.clone(inp.invoice)
    before = await validate(ctx, inv, inp.ruleset_version)
    changes = ChangeSet()
    after = await settle(ctx, inp.ruleset_version, inv, before, changes)
    final = changes.final()
    if final:
        ctx.emit(FEED_DETERMINISTIC, count=str(len(final)))
    return DeterministicResult(inv, final, before, after)


# ----------------------------------------------------------------------------- the model step
def llm_wanted(results: Mapping[str, object], cfg: FixConfig, mode: str) -> bool:
    det = results.get(NODE_DETERMINISTIC)
    return (isinstance(det, DeterministicResult) and cfg.llm_allowed(mode)
            and bool(llm_targets_of_run(det.after)))


def clamp_confidence(value: float) -> float:
    if 0.0 <= value <= 1.0:
        return round(value, 3)
    return 1.0 if value > 1.0 else 0.0  # NaN is neither: 0


async def llm(ctx: NodeContext, agent: FixAgent) -> LLMResult:
    det = ctx.result(NODE_DETERMINISTIC, DeterministicResult)
    targets = llm_targets_of_run(det.after)
    ctx.emit(FEED_LLM)
    out = await agent.run(ctx, llm_input(det.invoice, targets))
    kept, dropped = guard_changes(out, det.invoice, targets)
    return LLMResult(kept, clamp_confidence(out.confidence), tuple(sorted({t.path for t in targets})), dropped,
                     agent.model)


# ----------------------------------------------------------------------------- verification
@dataclass(frozen=True, slots=True)
class Evaluation:
    changes: tuple[Change, ...]
    run: validator_pb2.ValidationRun


async def evaluate(ctx: NodeContext, ruleset_version: str, det: DeterministicResult,
                   llm_changes: list[Change]) -> Evaluation:
    """The deterministic result with `llm_changes` on top, re-validated, then settled again: an LLM change can
    make a computed suggestion new (a lower-case unit code changes what the base-unit rule suggests)."""
    if not llm_changes:
        return Evaluation(det.changes, det.after)
    inv = paths.clone(det.invoice)
    cs = ChangeSet(det.changes)
    for c in llm_changes:
        paths.set_value(inv, c.path, c.new_value)
        cs.record(c.path, c.old_value, c.new_value, c.rule_ids, "llm", c.rationale)
    run = await validate(ctx, inv, ruleset_version)
    run = await settle(ctx, ruleset_version, inv, run, cs)
    return Evaluation(cs.final(), run)


def _common_prefix(a: str, b: str) -> int:
    n = 0
    for x, y in zip(a.split("."), b.split("."), strict=False):
        if x != y:
            break
        n += 1
    return n


def culprit_index(changes: list[Change], new_issues: list[validator_pb2.ValidationIssue]) -> int:
    """The LLM change most likely responsible for new error ids: the one whose path shares the longest leading
    path with a new issue's path (the earliest on a tie); the last change when none shares anything."""
    scores = [max(_common_prefix(c.path, i.path) for i in new_issues) for c in changes]
    best = max(scores)
    return scores.index(best) if best > 0 else len(changes) - 1


def _rule_ids(run: validator_pb2.ValidationRun) -> set[str]:
    return {i.rule_id for i in error_issues(run)}


def _keys(issues: tuple[validator_pb2.ValidationIssue, ...]) -> list[str]:
    return sorted({f"{i.rule_id}@{i.path}" for i in issues})


def build_candidate(inp: validator_pb2.FixTaskInput, det: DeterministicResult, ev: Evaluation,
                    llm_result: LLMResult | None, changes: tuple[Change, ...]) -> FixCandidate:
    before, after = error_issues(det.before), error_issues(ev.run)
    targets = tuple(i for i in inp.issues if i.severity == validator_pb2.SEVERITY_ERROR)
    llm_paths = set(llm_result.target_paths) if llm_result else set()
    return FixCandidate(
        changes=[c.to_candidate() for c in changes],
        errors_before=len(before), errors_after=len(after),
        before_rule_ids=sorted({i.rule_id for i in before}), after_rule_ids=sorted({i.rule_id for i in after}),
        target_rule_ids=sorted({i.rule_id for i in targets}),
        target_paths=sorted({i.path for i in targets} | llm_paths),
        target_keys=_keys(targets), after_keys=_keys(after),
        confidence=llm_result.confidence if llm_result else 1.0)


async def verify(ctx: NodeContext, inp: validator_pb2.FixTaskInput, verifier: VerifierAgent) -> VerifiedResult:
    det = ctx.result(NODE_DETERMINISTIC, DeterministicResult)
    got = ctx.results.get(NODE_LLM)
    llm_result = got if isinstance(got, LLMResult) else None
    remaining = list(llm_result.changes) if llm_result else []
    original_ids = _rule_ids(det.before)
    evaluations = 0
    while True:
        ev = await evaluate(ctx, inp.ruleset_version, det, remaining)
        evaluations += 1
        new = [i for i in error_issues(ev.run) if i.rule_id not in original_ids]
        if not new or not remaining or evaluations >= MAX_EVALUATIONS:
            break
        remaining.pop(culprit_index(remaining, new))
    cand = build_candidate(inp, det, ev, llm_result, ev.changes)
    verdict = await verifier.verify(ctx, cand, ())
    changes = ev.changes
    if verdict.verdict != "accept" and any(c.source == "llm" for c in changes):
        # the model's changes sink the candidate (low confidence, no gain): the computed ones may still stand
        det_ev = Evaluation(det.changes, det.after)
        det_cand = build_candidate(inp, det, det_ev, None, det.changes)
        det_verdict = await verifier.verify(ctx, det_cand, ())
        if det_verdict.verdict == "accept":
            cand, verdict, changes = det_cand, det_verdict, det.changes
    ctx.emit(FEED_VERIFIED, before=str(cand.errors_before), after=str(cand.errors_after))
    return VerifiedResult(cand, verdict.verdict, verdict.confidence, changes)


# ----------------------------------------------------------------------------- propose and result
async def propose(ctx: NodeContext, inp: validator_pb2.FixTaskInput) -> ProposeResult:
    v = ctx.result(NODE_VERIFY, VerifiedResult)
    if not v.changes:
        ctx.emit(FEED_NO_FIX)
        return ProposeResult("no_fix")
    # the AgentForbidden guard runs here, before anything is published (api-go enforces it again on accept)
    if (v.verdict != "accept"
            or any(paths.is_forbidden(c.path) or not paths.is_valid_path(c.path) for c in v.changes)):
        ctx.emit(FEED_NO_FIX)
        return ProposeResult("not_improving")
    det = ctx.result(NODE_DETERMINISTIC, DeterministicResult)
    cand = v.candidate
    detail = validator_pb2.FixProposalDetail(
        validation_run_id=inp.validation_run_id, payload_version=inp.payload_version,
        ruleset_version=inp.ruleset_version or det.after.ruleset_version,
        errors_before=cand.errors_before, errors_after=cand.errors_after,
        resolved_rule_ids=sorted(set(cand.before_rule_ids) - set(cand.after_rule_ids)))
    for c in v.changes:
        detail.notes.add(path=c.path, rule_ids=list(c.rule_ids), source=c.source, rationale=c.rationale)
    rationale = "; ".join(f"{c.path}: {c.rationale}" for c in v.changes if c.rationale)[:RATIONALE_CHARS]
    draft = ProposalDraft(
        kind=PROPOSAL_KIND, target_type="invoice", target_id=inp.invoice_id, summary_key=SUMMARY_KEY,
        summary_args={"changes": str(len(v.changes)), "resolved": str(cand.errors_before - cand.errors_after)},
        rationale=rationale, confidence=v.verdict_confidence,
        changes=tuple(FieldChangeDraft(c.path, c.old_value, c.new_value) for c in v.changes), detail=detail)
    pid = await ctx.propose(draft)
    ctx.emit(FEED_PROPOSED, changes=str(len(v.changes)))
    return ProposeResult("proposed", pid, len(v.changes))


async def result(ctx: NodeContext) -> validator_pb2.FixTaskResult:
    got = ctx.results.get(NODE_PROPOSE)
    p = got if isinstance(got, ProposeResult) else ProposeResult("no_fix")
    return validator_pb2.FixTaskResult(outcome=p.outcome, proposal_id=p.proposal_id, changes=p.changes)
