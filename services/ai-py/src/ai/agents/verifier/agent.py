"""VerifierAgent: checks as graph nodes (with the bounded revise and escalation loops) and the inline form
(agent-runtime contract v0.2, section 6)."""

from __future__ import annotations

from collections.abc import Callable, Iterable, Mapping, Sequence
from dataclasses import replace
from typing import Any, ClassVar

from pydantic import BaseModel

from ai.agents.verifier.core import (
    Check,
    CriticResult,
    Finding,
    StageVerdict,
    VerdictResult,
    VerifierProfile,
    confirm_by_critic,
    decide,
    paths_of,
)
from ai.gateway.types import Part
from ai.runtime.agent import AgentContext
from ai.runtime.context import NodeContext
from ai.runtime.graph import Expand, Node
from ai.runtime.registry import AgentRegistry
from ai.runtime.types import StepKind

type Evidence = Callable[[NodeContext], Sequence[Part]]
type OnRevise = Callable[[VerdictResult, NodeContext], Expand]

AGENT = "verifier"
CRITIC_UNAVAILABLE = "verifier.critic_unavailable"


def _collect(results: Mapping[str, object], node_ids: Iterable[str]) -> tuple[Finding, ...]:
    out: list[Finding] = []
    for nid in node_ids:
        value = results.get(nid)
        if isinstance(value, tuple):
            out.extend(f for f in value if isinstance(f, Finding))
    return tuple(out)


def _critic_model(critic: object) -> str:
    model = getattr(critic, "model", "")
    return model if isinstance(model, str) else ""


class VerifierAgent:
    name: ClassVar[str] = AGENT

    def __init__(self, profiles: Iterable[VerifierProfile[Any]] = ()) -> None:
        self._profiles: dict[type[BaseModel], VerifierProfile[Any]] = {}
        for p in profiles:
            self.register(p)

    @classmethod
    def from_registry(cls, reg: AgentRegistry) -> VerifierAgent:
        profiles = [p for p in reg.verifier_profiles.values() if isinstance(p, VerifierProfile)]
        return cls(profiles)

    def register(self, profile: VerifierProfile[Any]) -> None:
        if profile.output_type in self._profiles:
            raise ValueError(f"duplicate verifier profile for {profile.output_type.__name__}")
        self._profiles[profile.output_type] = profile

    def profile(self, output_type: type[BaseModel]) -> VerifierProfile[Any]:
        try:
            return self._profiles[output_type]
        except KeyError:
            raise ValueError(f"no verifier profile for {output_type.__name__}") from None

    # ------------------------------------------------------------------ inline form (Tracks C and D)
    async def verify(self, ctx: AgentContext, output: BaseModel, evidence: Sequence[Part], *,
                     revisions: int = 0) -> VerdictResult:
        """Checks, then the critic when critic_when holds, then decide(). No loops."""
        profile = self.profile(type(output))
        checks = tuple(f for c in profile.checks for f in c(output))
        critic: CriticResult | None = None
        if profile.critic is not None and profile.critic_when(output, checks):
            focus = tuple(f for f in checks if f.severity != "info")
            found = await profile.critic.review(ctx, output, evidence, focus)
            critic = CriticResult(tuple(found), paths_of(focus), _critic_model(profile.critic))
        findings = confirm_by_critic(checks, critic) + (critic.findings if critic else ())
        return decide(profile, output, findings, revisions=revisions,
                      critic_model=critic.model if critic else "")

    # ------------------------------------------------------------------ graph form
    def checks_as_nodes(self, prefix: str, *, output_type: type[BaseModel], output_node: str,
                        evidence: Evidence, stage: int = 1, revisions: int = 0,
                        on_revise: OnRevise | None = None) -> tuple[Node, ...]:
        """One node `<prefix>.<code_family>` per Check (parallel), `<prefix>.critic` (when critic_when holds over
        the check results; non-critical) and `<prefix>.join` (trigger all_done), which stores a StageVerdict.
        A "revise" join with `on_revise` returns on_revise(...) re-labelled with this stage's verdict; an
        "escalate" join with an escalation critic returns Expand([<prefix>.escalate, <prefix>.final])."""
        profile = self.profile(output_type)
        check_ids = tuple(f"{prefix}.{c.code_family}" for c in profile.checks)
        critic_id, join_id = f"{prefix}.critic", f"{prefix}.join"
        nodes = [Node(nid, AGENT, f"check.{c.code_family}", StepKind.DETERMINISTIC,
                      self._check_fn(c, output_node, output_type), depends_on=(output_node,))
                 for nid, c in zip(check_ids, profile.checks, strict=True)]
        join_deps = check_ids
        if profile.critic is not None:
            nodes.append(Node(critic_id, AGENT, "critic", StepKind.LLM,
                              self._critic_fn(profile, output_node, output_type, check_ids, evidence),
                              depends_on=check_ids, critical=False,
                              when=lambda r: profile.critic_when(r[output_node], _collect(r, check_ids))))
            join_deps = (*check_ids, critic_id)
        nodes.append(Node(join_id, AGENT, "join", StepKind.DETERMINISTIC,
                          self._join_fn(profile, prefix, output_node, output_type, check_ids, evidence, stage,
                                        revisions, on_revise),
                          depends_on=join_deps, trigger="all_done"))
        return tuple(nodes)

    @staticmethod
    def _check_fn(check: Check[Any], output_node: str,
                  output_type: type[BaseModel]) -> Callable[[NodeContext], Any]:
        async def fn(ctx: NodeContext) -> tuple[Finding, ...]:
            found = tuple(check(ctx.result(output_node, output_type)))
            flagged = sum(1 for f in found if f.severity != "info")
            if flagged:
                ctx.emit("verifier.check.flagged", family=check.code_family, count=str(flagged))
            else:
                ctx.emit("verifier.check.passed", family=check.code_family)
            return found

        return fn

    @staticmethod
    def _critic_fn(profile: VerifierProfile[Any], output_node: str, output_type: type[BaseModel],
                   check_ids: tuple[str, ...], evidence: Evidence) -> Callable[[NodeContext], Any]:
        async def fn(ctx: NodeContext) -> CriticResult:
            assert profile.critic is not None
            focus = tuple(f for f in _collect(ctx.results, check_ids) if f.severity != "info")
            found = tuple(await profile.critic.review(ctx, ctx.result(output_node, output_type), evidence(ctx),
                                                      focus))
            res = CriticResult(found, paths_of(focus), _critic_model(profile.critic))
            if found:
                ctx.emit("verifier.critic.disagreed", count=str(len(found)))
            else:
                ctx.emit("verifier.critic.agreed", paths=str(len(res.reviewed)))
            return res

        return fn

    def _join_fn(self, profile: VerifierProfile[Any], prefix: str, output_node: str,
                 output_type: type[BaseModel], check_ids: tuple[str, ...], evidence: Evidence, stage: int,
                 revisions: int, on_revise: OnRevise | None) -> Callable[[NodeContext], Any]:
        critic_id = f"{prefix}.critic"

        async def fn(ctx: NodeContext) -> StageVerdict | Expand:
            output = ctx.results.get(output_node)
            if not isinstance(output, output_type):  # the producer failed: nothing to accept
                v = VerdictResult("escalate", 0.0, (), revisions=revisions)
                ctx.emit("verifier.verdict.escalate", confidence="0.000")
                return StageVerdict(stage, output_node, v, final=True)
            checks = _collect(ctx.results, check_ids)
            critic = ctx.results.get(critic_id)
            critic = critic if isinstance(critic, CriticResult) else None
            findings = confirm_by_critic(checks, critic) + (critic.findings if critic else ())
            if critic is None and profile.critic is not None and profile.critic_when(output, checks):
                # the critic was needed but failed (refusal, spend cap, outage): a human looks
                findings += (Finding("", CRITIC_UNAVAILABLE, "block"),)
            v = decide(profile, output, findings, revisions=revisions, critic_model=critic.model if critic else "")
            if v.verdict == "revise" and on_revise is not None:
                exp = on_revise(v, ctx)
                ctx.emit("verifier.verdict.revise",
                         count=str(sum(1 for f in v.findings if f.source == "critic" and f.expected)))
                return Expand(exp.nodes, result=StageVerdict(stage, output_node, v, final=False), join=exp.join)
            if v.verdict == "escalate" and profile.escalation_critic is not None:
                nodes = self._escalation_nodes(profile, prefix, output_node, output_type, checks, critic, v,
                                               evidence, stage)
                ctx.emit("verifier.verdict.escalating", confidence=f"{v.confidence:.3f}")
                return Expand(nodes, result=StageVerdict(stage, output_node, v, final=False),
                              join=f"{prefix}.final")
            ctx.emit(f"verifier.verdict.{v.verdict}", confidence=f"{v.confidence:.3f}")
            return StageVerdict(stage, output_node, v, final=True)

        return fn

    @staticmethod
    def _escalation_nodes(profile: VerifierProfile[Any], prefix: str, output_node: str,
                          output_type: type[BaseModel], checks: tuple[Finding, ...], critic: CriticResult | None,
                          first: VerdictResult, evidence: Evidence, stage: int) -> tuple[Node, ...]:
        esc_id, final_id = f"{prefix}.escalate", f"{prefix}.final"
        focus = tuple(f for f in checks if f.severity != "info") + (critic.findings if critic else ())

        async def escalate(ctx: NodeContext) -> CriticResult:
            assert profile.escalation_critic is not None
            found = tuple(await profile.escalation_critic.review(
                ctx, ctx.result(output_node, output_type), evidence(ctx), focus))
            res = CriticResult(found, paths_of(focus), _critic_model(profile.escalation_critic))
            if found:
                ctx.emit("verifier.critic.disagreed", count=str(len(found)))
            else:
                ctx.emit("verifier.critic.agreed", paths=str(len(res.reviewed)))
            return res

        async def final(ctx: NodeContext) -> StageVerdict:
            esc = ctx.results.get(esc_id)
            if not isinstance(esc, CriticResult):  # the second opinion failed: keep the first verdict
                ctx.emit("verifier.verdict.escalate", confidence=f"{first.confidence:.3f}")
                return StageVerdict(stage, output_node, first, final=True)
            output = ctx.result(output_node, output_type)
            findings = confirm_by_critic(checks, esc) + esc.findings
            v = decide(profile, output, findings, revisions=first.revisions, critic_model=esc.model)
            if v.verdict == "revise":  # the final node never loops
                v = replace(v, verdict="escalate")
            ctx.emit(f"verifier.verdict.{v.verdict}", confidence=f"{v.confidence:.3f}")
            return StageVerdict(stage, output_node, v, final=True)

        return (
            Node(esc_id, AGENT, "escalate", StepKind.LLM, escalate, depends_on=(f"{prefix}.join",), critical=False),
            Node(final_id, AGENT, "final", StepKind.DETERMINISTIC, final, depends_on=(esc_id,), trigger="all_done"),
        )
