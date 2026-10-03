from datetime import UTC, datetime

import pytest
from google.protobuf import wrappers_pb2

from ai.gateway.types import Usage
from ai.gen.compliance.v1 import agents_pb2
from ai.runtime.nats_sink import PROPOSAL_AGENTS, NatsSink
from ai.runtime.proposals import EvidenceDraft, FieldChangeDraft, ProposalDraft
from ai.runtime.types import (
    Budget,
    GraphNodeSpec,
    RunIdentity,
    RunOutcome,
    RunStatus,
    RunTotals,
    StepKind,
    StepRecord,
    StepStatus,
)

RUN = "11111111-1111-4111-8111-111111111111"
IDENT = RunIdentity(run_id=RUN, firm_id="f1", client_company_id="c1", workflow="document_ingestion@1",
                    subject_type="document", subject_id="d1", delivery_attempt=2)


class FakeJS:
    def __init__(self, fail: bool = False) -> None:
        self.calls: list[dict] = []
        self.fail = fail

    async def publish(self, subject, payload=b"", timeout=None, stream=None, headers=None):
        if self.fail:
            raise TimeoutError("nats down")
        self.calls.append({"subject": subject, "data": payload, "timeout": timeout, "headers": headers})


def _step_rec() -> StepRecord:
    return StepRecord(
        run_id=RUN, firm_id="f1", step_id="s1", seq=7, node_id="extract", depends_on=("intake",),
        agent="extraction", action="extract", kind=StepKind.LLM, status=StepStatus.SUCCEEDED, attempt=1,
        at=datetime(2026, 1, 2, 3, 4, 5, tzinfo=UTC), duration_ms=120,
        usage=Usage(model="claude-sonnet-5", prompt_id="extract", prompt_version=1, input_tokens=10,
                    output_tokens=5, cost_micro_usd=42, llm_calls=1),
        message_key="extracted", message_args={"n": "3"}, error_code="")


def _draft(kind: str = "document.attribution") -> ProposalDraft:
    return ProposalDraft(
        kind=kind, target_type="document", target_id="d1", summary_key="attr", summary_args={"a": "b"},
        rationale="why", confidence=0.9, changes=(FieldChangeDraft("client_company_id", "", "c2"),),
        detail=wrappers_pb2.StringValue(value="x"), evidence=(EvidenceDraft("page", "p1", "ex"),),
        expires_in_s=3600)


async def test_run_started():
    js = FakeJS()
    plan = (GraphNodeSpec("intake", "intake", "classify", StepKind.LLM, ()),
            GraphNodeSpec("extract", "extraction", "extract", StepKind.LLM, ("intake",)))
    await NatsSink(js, publish_timeout_s=1.5).run_started(IDENT, Budget(max_steps=9), plan)
    (call,) = js.calls
    assert call["subject"] == "agent.run.started"
    assert call["headers"]["Nats-Msg-Id"] == f"agent.run.started:{RUN}"
    assert call["timeout"] == 1.5
    m = agents_pb2.AgentRunStarted()
    m.ParseFromString(call["data"])
    assert (m.run_id, m.firm_id, m.client_company_id, m.workflow) == (RUN, "f1", "c1", "document_ingestion@1")
    assert (m.subject_type, m.subject_id, m.delivery_attempt) == ("document", "d1", 2)
    assert m.budget.max_steps == 9 and m.budget.deadline_seconds == 300
    assert [n.node_id for n in m.plan] == ["intake", "extract"]
    assert m.plan[1].kind == agents_pb2.STEP_KIND_LLM and list(m.plan[1].depends_on) == ["intake"]
    assert m.started_at.seconds > 0


async def test_step():
    js = FakeJS()
    await NatsSink(js).step(_step_rec())
    (call,) = js.calls
    assert call["subject"] == "agent.run.step"
    assert call["headers"]["Nats-Msg-Id"] == f"agent.run.step:{RUN}:7"
    m = agents_pb2.AgentStepEvent()
    m.ParseFromString(call["data"])
    assert (m.run_id, m.step_id, m.seq, m.node_id) == (RUN, "s1", 7, "extract")
    assert m.status == agents_pb2.AGENT_STEP_STATUS_SUCCEEDED and m.kind == agents_pb2.STEP_KIND_LLM
    assert m.usage.cost_micro_usd == 42 and m.usage.model == "claude-sonnet-5" and m.usage.llm_calls == 1
    assert dict(m.message_args) == {"n": "3"} and m.message_key == "extracted"
    assert m.at.ToDatetime(tzinfo=UTC) == datetime(2026, 1, 2, 3, 4, 5, tzinfo=UTC)
    assert m.duration_ms == 120 and list(m.depends_on) == ["intake"]


async def test_step_without_usage():
    js = FakeJS()
    rec = _step_rec()
    object.__setattr__(rec, "usage", None)
    await NatsSink(js).step(rec)
    m = agents_pb2.AgentStepEvent()
    m.ParseFromString(js.calls[0]["data"])
    assert not m.HasField("usage")


async def test_run_finished():
    js = FakeJS()
    out = RunOutcome(identity=IDENT, status=RunStatus.BUDGET_EXCEEDED, results={}, node_status={},
                     totals=RunTotals(steps=3, llm_calls=2, response_cache_hits=1, input_tokens=100,
                                      output_tokens=20, cost_micro_usd=777), error_code="budget_cost")
    await NatsSink(js).run_finished(out)
    (call,) = js.calls
    assert call["subject"] == "agent.run.finished"
    assert call["headers"]["Nats-Msg-Id"] == f"agent.run.finished:{RUN}"
    m = agents_pb2.AgentRunFinished()
    m.ParseFromString(call["data"])
    assert m.status == agents_pb2.AGENT_RUN_STATUS_BUDGET_EXCEEDED and m.error_code == "budget_cost"
    assert (m.totals.steps, m.totals.llm_calls, m.totals.response_cache_hits, m.totals.cost_micro_usd) == (
        3, 2, 1, 777)
    assert (m.subject_type, m.subject_id) == ("document", "d1")


async def test_proposal():
    js = FakeJS()
    await NatsSink(js).proposal(IDENT, _draft(), "pid-1")
    (call,) = js.calls
    assert call["subject"] == "agent.proposal.created"
    assert call["headers"]["Nats-Msg-Id"] == "agent.proposal.created:pid-1"
    m = agents_pb2.ProposalCreated()
    m.ParseFromString(call["data"])
    p = m.proposal
    assert (p.proposal_id, p.firm_id, p.client_company_id, p.run_id) == ("pid-1", "f1", "c1", RUN)
    assert p.agent == "intake" and p.kind == "document.attribution"
    assert (p.target_type, p.target_id, p.summary_key) == ("document", "d1", "attr")
    assert p.confidence == pytest.approx(0.9) and p.rationale == "why"
    assert [(c.path, c.old_value, c.new_value) for c in p.changes] == [("client_company_id", "", "c2")]
    assert [(e.kind, e.ref, e.excerpt) for e in p.evidence] == [("page", "p1", "ex")]
    sv = wrappers_pb2.StringValue()
    assert p.detail.Unpack(sv) and sv.value == "x"
    assert p.expires_at.seconds - p.created_at.seconds == 3600


async def test_proposal_no_expiry_no_detail():
    js = FakeJS()
    d = ProposalDraft(kind="x.y", target_type="document", target_id="d1", summary_key="k",
                      expires_in_s=None)
    await NatsSink(js).proposal(IDENT, d, "pid-2")
    m = agents_pb2.ProposalCreated()
    m.ParseFromString(js.calls[0]["data"])
    assert not m.proposal.HasField("expires_at") and not m.proposal.HasField("detail")


async def test_unmapped_kind_falls_back_to_domain():
    js = FakeJS()
    await NatsSink(js).proposal(IDENT, _draft("invoice.field_fix"), "pid-3")
    m = agents_pb2.ProposalCreated()
    m.ParseFromString(js.calls[0]["data"])
    assert m.proposal.agent == "invoice"
    assert PROPOSAL_AGENTS == {"document.attribution": "intake"}


async def test_publish_failures_swallowed_except_proposal(caplog):
    js = FakeJS(fail=True)
    sink = NatsSink(js)
    out = RunOutcome(identity=IDENT, status=RunStatus.SUCCEEDED, results={}, node_status={},
                     totals=RunTotals())
    await sink.run_started(IDENT, Budget(), ())
    await sink.step(_step_rec())
    await sink.run_finished(out)
    with pytest.raises(TimeoutError):
        await sink.proposal(IDENT, _draft(), "pid-4")


async def test_trace_headers_injected():
    from opentelemetry import trace

    js = FakeJS()
    with trace.get_tracer("t").start_as_current_span("parent") as span:
        await NatsSink(js).step(_step_rec())
        tid = f"{span.get_span_context().trace_id:032x}"
    assert tid in js.calls[0]["headers"]["traceparent"]


async def test_run_started_trace_id():
    from opentelemetry import trace

    js = FakeJS()
    with trace.get_tracer("t").start_as_current_span("parent") as span:
        await NatsSink(js).run_started(IDENT, Budget(), ())
        tid = f"{span.get_span_context().trace_id:032x}"
    m = agents_pb2.AgentRunStarted()
    m.ParseFromString(js.calls[0]["data"])
    assert m.trace_id == tid
