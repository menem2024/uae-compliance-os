"""Fix agent types: the LLM step's pydantic models, the `FixCandidate` the Verifier judges, and the plain
results the graph nodes hand to each other.

Money is never a float here; the only floats are the model's / verdict's `confidence` probabilities
(agent-runtime contract rule 5).
"""

from __future__ import annotations

from collections.abc import Iterable
from dataclasses import dataclass, field
from typing import Literal

from pydantic import BaseModel

from ai.gen.compliance.v1 import invoice_pb2, validator_pb2

type Source = Literal["deterministic", "llm"]
type Outcome = Literal["proposed", "no_fix", "not_improving"]


# ----------------------------------------------------------------------------- the LLM step
class LLMIssue(BaseModel):
    rule_id: str
    path: str
    business_term: str
    message: str
    current_value: str  # "" when the field is absent


class LLMFixInput(BaseModel):
    invoice_json: str  # the (deterministically patched) invoice, protojson with proto field names
    issues: list[LLMIssue]


class LLMChange(BaseModel):
    path: str
    new_value: str
    rule_ids: list[str]
    rationale: str


class LLMFixOutput(BaseModel):
    """Structured output of `fix.invoice_fields`. Empty string for absent (strict-schema rule)."""

    changes: list[LLMChange]
    confidence: float  # probability in [0, 1] that every change is correct; 0 with no changes


# ----------------------------------------------------------------------------- the verified candidate
class CandidateChange(BaseModel):
    """One field change plus its note (what FixChangeNote carries in the proposal detail)."""

    path: str
    old_value: str
    new_value: str
    rule_ids: list[str]
    source: Source
    rationale: str


class FixCandidate(BaseModel):
    """What the `FixCandidate` verifier profile judges: pure data, the validator call already happened."""

    changes: list[CandidateChange]
    errors_before: int
    errors_after: int
    before_rule_ids: list[str]
    after_rule_ids: list[str]
    target_rule_ids: list[str]
    target_paths: list[str]  # the paths of the issues the agent was asked to fix
    target_keys: list[str]  # "<rule_id>@<path>" of those issues
    after_keys: list[str]  # "<rule_id>@<path>" of the errors left after the changes
    confidence: float = 1.0  # the LLM's confidence when an LLM change is present, else 1.0


# ----------------------------------------------------------------------------- node results
@dataclass(frozen=True, slots=True)
class Change:
    path: str
    old_value: str
    new_value: str
    rule_ids: tuple[str, ...]
    source: Source
    rationale: str

    def to_candidate(self) -> CandidateChange:
        return CandidateChange(path=self.path, old_value=self.old_value, new_value=self.new_value,
                               rule_ids=list(self.rule_ids), source=self.source, rationale=self.rationale)


class ChangeSet:
    """Changes by path, in first-seen order. A path changed twice keeps its first old value and its last new
    value; a change that ends where it began is dropped by `final()`."""

    def __init__(self, initial: Iterable[Change] = ()) -> None:
        self._by_path: dict[str, Change] = {c.path: c for c in initial}

    def copy(self) -> ChangeSet:
        return ChangeSet(self._by_path.values())

    def record(self, path: str, old_value: str, new_value: str, rule_ids: Iterable[str], source: Source,
               rationale: str) -> None:
        prev = self._by_path.get(path)
        if prev is None:
            self._by_path[path] = Change(path, old_value, new_value, tuple(dict.fromkeys(rule_ids)), source,
                                         rationale)
            return
        merged = tuple(dict.fromkeys((*prev.rule_ids, *rule_ids)))
        self._by_path[path] = Change(path, prev.old_value, new_value, merged,
                                     "llm" if "llm" in (prev.source, source) else "deterministic",
                                     prev.rationale if prev.source == "llm" and source != "llm" else rationale)

    def final(self) -> tuple[Change, ...]:
        return tuple(c for c in self._by_path.values() if c.old_value != c.new_value)


@dataclass(frozen=True, slots=True)
class DeterministicResult:
    invoice: invoice_pb2.Invoice  # the patched invoice
    changes: tuple[Change, ...]
    before: validator_pb2.ValidationRun  # the original invoice, re-validated
    after: validator_pb2.ValidationRun  # the patched invoice


@dataclass(frozen=True, slots=True)
class LLMResult:
    changes: tuple[Change, ...]  # survivors of the guards
    confidence: float
    target_paths: tuple[str, ...]
    dropped: int = 0
    model: str = field(default="")


@dataclass(frozen=True, slots=True)
class VerifiedResult:
    candidate: FixCandidate
    verdict: Literal["accept", "revise", "escalate"]
    verdict_confidence: float
    changes: tuple[Change, ...]


@dataclass(frozen=True, slots=True)
class ProposeResult:
    outcome: Outcome
    proposal_id: str = ""
    changes: int = 0
