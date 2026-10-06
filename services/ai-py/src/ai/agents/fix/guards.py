"""Pure guards over what the model proposes and over which issues the model is asked about (spec 5.7.2).

The guards run before anything is verified or proposed, and the FixSuite uses the same target selection so the
eval sees exactly the request production sends.
"""

from __future__ import annotations

from collections.abc import Iterable

from ai.agents.fix import paths
from ai.agents.fix.convert import error_issues, invoice_json
from ai.agents.fix.types import Change, LLMFixInput, LLMFixOutput, LLMIssue
from ai.gen.compliance.v1 import invoice_pb2, validator_pb2

MAX_LLM_CHANGES = 20
MAX_VALUE_CHARS = 500
MAX_RATIONALE_CHARS = 300


def llm_targets(issues: Iterable[validator_pb2.ValidationIssue]) -> tuple[validator_pb2.ValidationIssue, ...]:
    """The error issues the model may be asked about: fixable, no computed suggested value, on a path an agent
    may change, one per (rule, path)."""
    out: list[validator_pb2.ValidationIssue] = []
    seen: set[tuple[str, str]] = set()
    for i in issues:
        key = (i.rule_id, i.path)
        if (i.severity == validator_pb2.SEVERITY_ERROR and i.fixable and not i.suggested_value
                and paths.is_valid_path(i.path) and not paths.is_forbidden(i.path) and key not in seen):
            seen.add(key)
            out.append(i)
    return tuple(out)


def llm_targets_of_run(run: validator_pb2.ValidationRun) -> tuple[validator_pb2.ValidationIssue, ...]:
    return llm_targets(error_issues(run))


def llm_input(inv: invoice_pb2.Invoice, targets: Iterable[validator_pb2.ValidationIssue]) -> LLMFixInput:
    return LLMFixInput(
        invoice_json=invoice_json(inv),
        issues=[LLMIssue(rule_id=i.rule_id, path=i.path, business_term=i.business_term, message=i.message,
                         current_value=paths.get_value(inv, i.path)) for i in targets])


def guard_changes(out: LLMFixOutput, inv: invoice_pb2.Invoice,
                  targets: Iterable[validator_pb2.ValidationIssue]) -> tuple[tuple[Change, ...], int]:
    """(surviving changes, number dropped). A change is dropped when its path breaks the CI section 12 grammar,
    is AgentForbidden, is not the path of a target issue, repeats an earlier path, leaves the value as it is,
    is not a valid value for its field (a bool takes true/false; values are bounded), or cannot be applied.
    At most MAX_LLM_CHANGES survive."""
    by_path: dict[str, set[str]] = {}
    for t in targets:
        by_path.setdefault(t.path, set()).add(t.rule_id)
    scratch = paths.clone(inv)
    kept: list[Change] = []
    seen: set[str] = set()
    for c in out.changes:
        p = c.path
        if (p in seen or p not in by_path or not paths.is_valid_path(p) or paths.is_forbidden(p)
                or len(c.new_value) > MAX_VALUE_CHARS or "\x00" in c.new_value):
            continue
        current = paths.get_value(inv, p)
        if c.new_value == current:
            continue
        try:
            paths.set_value(scratch, p, c.new_value)
        except ValueError:
            continue
        seen.add(p)
        kept.append(Change(p, current, c.new_value, tuple(sorted(by_path[p])), "llm",
                           c.rationale.strip()[:MAX_RATIONALE_CHARS]))
        if len(kept) == MAX_LLM_CHANGES:
            break
    return tuple(kept), len(out.changes) - len(kept)
