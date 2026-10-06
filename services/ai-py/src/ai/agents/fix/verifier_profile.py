"""The `FixCandidate` verifier profile (agent-runtime contract section 6, spec 5.7.2).

Every check is a pure function of the candidate: the validator call happened in `fix.verify`, because a contract
section 6 `Check` must not do I/O. There is no critic: validator-rs, not a model, judges whether the issues went
away. `fix.no_new_errors`, `fix.improves` and `fix.paths_targeted` are the contract's three; `fix.resolves_target`
(an issue the agent was asked about must be gone) and `fix.forbidden_paths` (the AgentForbidden guard, enforced
here before `propose` and again by api-go) are additive.
"""

from __future__ import annotations

from typing import ClassVar

from ai.agents.fix import paths
from ai.agents.fix.types import FixCandidate
from ai.agents.verifier.core import Finding, VerifierProfile


class NoNewErrors:
    code_family: ClassVar[str] = "fix_no_new_errors"

    def __call__(self, out: FixCandidate) -> list[Finding]:
        new = sorted(set(out.after_rule_ids) - set(out.before_rule_ids))
        return [Finding("", "fix.no_new_errors", "block", observed=",".join(new))] if new else []


class Improves:
    code_family: ClassVar[str] = "fix_improves"

    def __call__(self, out: FixCandidate) -> list[Finding]:
        if out.errors_after >= out.errors_before:
            return [Finding("", "fix.improves", "block", observed=str(out.errors_after),
                            expected=f"< {out.errors_before}")]
        return []


class ResolvesTarget:
    code_family: ClassVar[str] = "fix_resolves_target"

    def __call__(self, out: FixCandidate) -> list[Finding]:
        if out.target_keys and set(out.target_keys) <= set(out.after_keys):
            return [Finding("", "fix.resolves_target", "block", observed=str(len(out.target_keys)))]
        return []


class PathsTargeted:
    code_family: ClassVar[str] = "fix_paths_targeted"

    def __call__(self, out: FixCandidate) -> list[Finding]:
        targets = set(out.target_paths)
        return [Finding(c.path, "fix.paths_targeted", "block", observed=c.path)
                for c in out.changes if c.source == "llm" and c.path not in targets]


class ForbiddenPaths:
    code_family: ClassVar[str] = "fix_forbidden_paths"

    def __call__(self, out: FixCandidate) -> list[Finding]:
        return [Finding(c.path, "fix.forbidden_paths", "block", observed=c.path)
                for c in out.changes if paths.is_forbidden(c.path) or not paths.is_valid_path(c.path)]


def producer_confidence(out: FixCandidate) -> float:
    """The LLM's confidence when an LLM change is present, else 1.0 (every deterministic change is computed)."""
    return out.confidence if any(c.source == "llm" for c in out.changes) else 1.0


def fix_profile() -> VerifierProfile[FixCandidate]:
    return VerifierProfile(
        output_type=FixCandidate,
        checks=(NoNewErrors(), Improves(), ResolvesTarget(), PathsTargeted(), ForbiddenPaths()),
        critic=None, producer_confidence=producer_confidence)
