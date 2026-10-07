"""Verifier core types and the binding verdict rule (agent-runtime contract v0.2, section 6).

Pure: no I/O and no model calls. `decide` is the verdict rule; `confirm_by_critic` is the critic-confirmation
rule that runs before it.
"""

from __future__ import annotations

from collections.abc import Callable, Iterable, Mapping, Sequence
from dataclasses import dataclass, replace
from typing import ClassVar, Literal, Protocol

from pydantic import BaseModel

from ai.gateway.types import Part
from ai.runtime.agent import AgentContext

type Severity = Literal["info", "warn", "block"]
type VerdictName = Literal["accept", "revise", "escalate"]

BLOCK_FACTOR = 0.5
WARN_FACTOR = 0.9
CONFIRMED_SUFFIX = ".confirmed_by_critic"


@dataclass(frozen=True, slots=True)
class Finding:
    path: str
    code: str  # "<family>.<check>", e.g. "arithmetic.tax_total"
    severity: Severity
    observed: str = ""
    expected: str = ""
    evidence_ref: str = ""
    source: Literal["check", "critic"] = "check"
    related: tuple[str, ...] = ()  # other paths the finding depends on (critic focus)


class Check[OutT: BaseModel](Protocol):
    code_family: ClassVar[str]

    def __call__(self, output: OutT) -> list[Finding]: ...


class Critic[OutT: BaseModel](Protocol):
    async def review(self, ctx: AgentContext, output: OutT, evidence: Sequence[Part],
                     focus: Sequence[Finding]) -> list[Finding]: ...


def _any_non_info(_out: object, findings: Sequence[Finding]) -> bool:
    return any(f.severity != "info" for f in findings)


def _full_confidence(_out: object) -> float:
    return 1.0


@dataclass(frozen=True, slots=True)
class VerifierProfile[OutT: BaseModel]:
    output_type: type[OutT]
    checks: tuple[Check[OutT], ...]
    critic: Critic[OutT] | None = None
    critic_when: Callable[[OutT, Sequence[Finding]], bool] = _any_non_info
    escalation_critic: Critic[OutT] | None = None  # second opinion (OPUS) when a stage ends in "escalate"
    accept_threshold: float = 0.85
    max_revisions: int = 1
    producer_confidence: Callable[[OutT], float] = _full_confidence


@dataclass(frozen=True, slots=True)
class VerdictResult:
    verdict: VerdictName
    confidence: float
    findings: tuple[Finding, ...]
    critic_model: str = ""
    revisions: int = 0


@dataclass(frozen=True, slots=True)
class StageVerdict:
    """Result of a `<prefix>.join` / `<prefix>.final` node."""

    stage: int
    output_node: str  # the node whose output this stage verified
    verdict: VerdictResult
    final: bool  # False when the join expanded (a revise or an escalation follows)


@dataclass(frozen=True, slots=True)
class CriticResult:
    """Result of a `<prefix>.critic` / `<prefix>.escalate` node: disagreements plus what was re-read."""

    findings: tuple[Finding, ...]
    reviewed: frozenset[str]
    model: str = ""


def paths_of(findings: Iterable[Finding]) -> frozenset[str]:
    """Every path a set of findings depends on (its own path plus `related`)."""
    return frozenset(p for f in findings for p in (f.path, *f.related) if p)


def confirm_by_critic(check_findings: Iterable[Finding], critic: CriticResult | None) -> tuple[Finding, ...]:
    """Critic confirmation (contract section 6): a non-info check finding whose path and every related path
    the critic re-read and found equal to the source becomes `info` with code `+ ".confirmed_by_critic"`.
    The source itself is inconsistent then, which is validator-rs's question, not an extraction error."""
    if critic is None:
        return tuple(check_findings)
    disputed = {f.path for f in critic.findings}
    out: list[Finding] = []
    for f in check_findings:
        paths = [p for p in (f.path, *f.related) if p]
        if (f.severity != "info" and f.source == "check" and paths
                and all(p in critic.reviewed and p not in disputed for p in paths)):
            f = replace(f, severity="info", code=f.code + CONFIRMED_SUFFIX)
        out.append(f)
    return tuple(out)


def decide[OutT: BaseModel](profile: VerifierProfile[OutT], output: OutT, findings: Sequence[Finding], *,
                            revisions: int, critic_model: str = "") -> VerdictResult:
    """The verdict rule (contract section 6, binding):

    confidence = producer_confidence x 0.5 per block x 0.9 per warn. accept: no block and confidence >=
    accept_threshold. revise: revisions < max_revisions and a critic finding carries an expected value.
    Otherwise escalate (a human looks).
    """
    conf = min(1.0, max(0.0, float(profile.producer_confidence(output))))
    for f in findings:
        if f.severity == "block":
            conf *= BLOCK_FACTOR
        elif f.severity == "warn":
            conf *= WARN_FACTOR
    conf = round(conf, 3)
    verdict: VerdictName
    if not any(f.severity == "block" for f in findings) and conf >= profile.accept_threshold:
        verdict = "accept"
    elif revisions < profile.max_revisions and any(f.source == "critic" and f.expected for f in findings):
        verdict = "revise"
    else:
        verdict = "escalate"
    return VerdictResult(verdict=verdict, confidence=conf, findings=tuple(findings), critic_model=critic_model,
                         revisions=revisions)


def final_verdict(results: Mapping[str, object]) -> StageVerdict | None:
    """The final StageVerdict among a run's node results (highest stage wins), or None."""
    finals = [v for v in results.values() if isinstance(v, StageVerdict) and v.final]
    return max(finals, key=lambda v: v.stage) if finals else None
