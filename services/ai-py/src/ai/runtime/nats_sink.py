"""NatsSink: the production EventSink (contract section 3.6). Publishes agents.proto messages on AGENTS.

Subjects and Nats-Msg-Ids are exactly contract section 4. Only proposal() may raise (a lost proposal must
retry its node); the other three log and swallow publish failures. No prompt text or document content is
ever put in a published field or a log line (rule 6).
"""

import logging
from datetime import UTC, datetime, timedelta

from google.protobuf import any_pb2
from google.protobuf.message import Message
from google.protobuf.timestamp_pb2 import Timestamp
from opentelemetry import trace
from opentelemetry.propagate import inject

from ai.gateway.types import Usage
from ai.gen.compliance.v1 import agents_pb2
from ai.runtime.proposals import ProposalDraft
from ai.runtime.types import Budget, GraphNodeSpec, RunIdentity, RunOutcome, StepRecord

logger = logging.getLogger(__name__)

# ProposalDraft carries no agent name; resolve by kind. Tracks C/D add an entry per new kind.
PROPOSAL_AGENTS: dict[str, str] = {"document.attribution": "intake"}


def _ts(dt: datetime) -> Timestamp:
    ts = Timestamp()
    ts.FromDatetime(dt.astimezone(UTC))
    return ts


def _usage(u: Usage) -> agents_pb2.ModelUsage:
    return agents_pb2.ModelUsage(
        model=u.model, prompt_id=u.prompt_id, prompt_version=u.prompt_version, input_tokens=u.input_tokens,
        output_tokens=u.output_tokens, cache_read_input_tokens=u.cache_read_input_tokens,
        cache_creation_input_tokens=u.cache_creation_input_tokens, cost_micro_usd=u.cost_micro_usd,
        response_cache_hit=u.response_cache_hit, llm_calls=u.llm_calls)


class NatsSink:
    def __init__(self, js, *, publish_timeout_s: float = 2.0) -> None:
        self._js = js
        self._timeout = publish_timeout_s

    async def _publish(self, subject: str, msg_id: str, msg: Message) -> None:
        headers: dict[str, str] = {}
        inject(headers)
        headers["Nats-Msg-Id"] = msg_id
        await self._js.publish(subject, msg.SerializeToString(), timeout=self._timeout, headers=headers)

    async def _publish_quiet(self, subject: str, msg_id: str, msg: Message) -> None:
        try:
            await self._publish(subject, msg_id, msg)
        except Exception:
            logger.warning("event publish failed", extra={"subject": subject, "msg_id": msg_id}, exc_info=True)

    async def run_started(self, identity: RunIdentity, budget: Budget,
                          plan: tuple[GraphNodeSpec, ...]) -> None:
        ctx = trace.get_current_span().get_span_context()
        msg = agents_pb2.AgentRunStarted(
            run_id=identity.run_id, firm_id=identity.firm_id, client_company_id=identity.client_company_id,
            workflow=identity.workflow, subject_type=identity.subject_type, subject_id=identity.subject_id,
            budget=agents_pb2.RunBudget(
                max_steps=budget.max_steps, max_llm_calls=budget.max_llm_calls,
                max_input_tokens=budget.max_input_tokens, max_output_tokens=budget.max_output_tokens,
                max_cost_micro_usd=budget.max_cost_micro_usd, deadline_seconds=int(budget.deadline_s)),
            started_at=_ts(datetime.now(UTC)), trace_id=f"{ctx.trace_id:032x}" if ctx.is_valid else "",
            delivery_attempt=identity.delivery_attempt,
            plan=[agents_pb2.GraphNode(
                node_id=n.node_id, agent=n.agent, action=n.action,
                kind=agents_pb2.StepKind.Value(f"STEP_KIND_{n.kind.name}"), depends_on=list(n.depends_on))
                for n in plan])
        await self._publish_quiet("agent.run.started", f"agent.run.started:{identity.run_id}", msg)

    async def step(self, rec: StepRecord) -> None:
        msg = agents_pb2.AgentStepEvent(
            run_id=rec.run_id, firm_id=rec.firm_id, step_id=rec.step_id, seq=rec.seq, node_id=rec.node_id,
            depends_on=list(rec.depends_on), agent=rec.agent, action=rec.action,
            kind=agents_pb2.StepKind.Value(f"STEP_KIND_{rec.kind.name}"),
            status=agents_pb2.AgentStepStatus.Value(f"AGENT_STEP_STATUS_{rec.status.name}"),
            attempt=rec.attempt, at=_ts(rec.at), duration_ms=rec.duration_ms, message_key=rec.message_key,
            message_args=dict(rec.message_args), error_code=rec.error_code)
        if rec.usage is not None:
            msg.usage.CopyFrom(_usage(rec.usage))
        await self._publish_quiet("agent.run.step", f"agent.run.step:{rec.run_id}:{rec.seq}", msg)

    async def run_finished(self, outcome: RunOutcome) -> None:
        ident, t = outcome.identity, outcome.totals
        msg = agents_pb2.AgentRunFinished(
            run_id=ident.run_id, firm_id=ident.firm_id,
            status=agents_pb2.AgentRunStatus.Value(f"AGENT_RUN_STATUS_{outcome.status.name}"),
            totals=agents_pb2.RunTotals(
                steps=t.steps, llm_calls=t.llm_calls, response_cache_hits=t.response_cache_hits,
                input_tokens=t.input_tokens, output_tokens=t.output_tokens, cost_micro_usd=t.cost_micro_usd),
            finished_at=_ts(datetime.now(UTC)), error_code=outcome.error_code,
            subject_type=ident.subject_type, subject_id=ident.subject_id)
        await self._publish_quiet("agent.run.finished", f"agent.run.finished:{ident.run_id}", msg)

    async def proposal(self, identity: RunIdentity, draft: ProposalDraft, proposal_id: str) -> None:
        """Unlike the other methods, a publish failure propagates so the node retries (rule 9)."""
        now = datetime.now(UTC)
        p = agents_pb2.Proposal(
            proposal_id=proposal_id, firm_id=identity.firm_id, client_company_id=identity.client_company_id,
            run_id=identity.run_id, agent=PROPOSAL_AGENTS.get(draft.kind, draft.kind.split(".")[0]),
            kind=draft.kind, target_type=draft.target_type, target_id=draft.target_id,
            summary_key=draft.summary_key, summary_args=dict(draft.summary_args), rationale=draft.rationale,
            confidence=draft.confidence, created_at=_ts(now),
            changes=[agents_pb2.FieldChange(path=c.path, old_value=c.old_value, new_value=c.new_value)
                     for c in draft.changes],
            evidence=[agents_pb2.Evidence(kind=e.kind, ref=e.ref, excerpt=e.excerpt) for e in draft.evidence])
        if draft.detail is not None:
            detail = any_pb2.Any()
            detail.Pack(draft.detail)
            p.detail.CopyFrom(detail)
        if draft.expires_in_s is not None:
            p.expires_at.CopyFrom(_ts(now + timedelta(seconds=draft.expires_in_s)))
        await self._publish("agent.proposal.created", f"agent.proposal.created:{proposal_id}",
                            agents_pb2.ProposalCreated(proposal=p))
