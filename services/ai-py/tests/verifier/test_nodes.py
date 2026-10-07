"""checks_as_nodes inside a real GraphExecutor run: accept, critic confirmation, one revision, escalation,
critic failure, a missing producer, and the inline form."""

import uuid
from decimal import Decimal

import pytest

from ai.agents.extraction.schema import ExtractionOutput, FieldConfidence, flatten
from ai.agents.verifier.agent import VerifierAgent
from ai.agents.verifier.core import final_verdict
from ai.agents.verifier.critic import CriticOutput, CriticReview, InvoiceCritic
from ai.agents.verifier.invoice import CRITICAL_PATHS, invoice_profile
from ai.agents.verifier.proto import to_proto
from ai.gateway.errors import PermanentModelError
from ai.gateway.fake import FakeGateway
from ai.gateway.types import OPUS, SONNET, DocumentPart, ModelRequest
from ai.gen.compliance.v1 import agents_pb2
from ai.runtime.clock import FakeClock
from ai.runtime.events import MemorySink
from ai.runtime.graph import Expand, GraphExecutor, Node, TaskGraph
from ai.runtime.registry import AgentRegistry
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import Budget, RunIdentity, RunStatus, StepKind, StepStatus
from ai.synthetic.generator import generate

DOC = DocumentPart.of(b"%PDF-1.7 verifier test")
RUN = RunIdentity(str(uuid.uuid4()), "firm-1", "cc-1", "verify_test@1", "document", "doc-1")


def output(inv) -> ExtractionOutput:
    return ExtractionOutput(invoice=inv, language="en",
                            field_confidence=[FieldConfidence(path=p, confidence=0.95) for p in CRITICAL_PATHS])


def reader(printed):
    """A critic script: the document prints `printed`; it answers every path listed in the request."""
    values = flatten(printed)

    def script(req: ModelRequest) -> CriticOutput:
        text = req.messages[0].parts[-1].text
        paths = [line[2:].split(": ", 1)[0] for line in text.splitlines() if line.startswith("- ")]
        return CriticOutput(reviews=[CriticReview(path=p, document_value=values.get(p, ""), matches=True)
                                     for p in paths])

    return script


def by_prompt(scripts):
    return lambda req: scripts[req.prompt_id](req)


def agent(*, escalation: bool = True) -> VerifierAgent:
    esc = InvoiceCritic(model=OPUS, prompt_id="verifier.escalate") if escalation else None
    return VerifierAgent([invoice_profile(InvoiceCritic(model=SONNET), esc)])


def produce(value) -> Node:
    async def fn(ctx):
        return value

    return Node("extraction.extract", "extraction", "extract", StepKind.LLM, fn)


def verify_nodes(va: VerifierAgent, **kw) -> tuple[Node, ...]:
    return va.checks_as_nodes("verify", output_type=ExtractionOutput, output_node="extraction.extract",
                              evidence=lambda ctx: (DOC,), **kw)


def first_with_defect(defect: str):
    return next(s for s in (generate(i, "en") for i in range(400)) if s.defects == (defect,))


async def run(nodes, gateway):
    sink = MemorySink()
    ex = GraphExecutor(gateway=gateway, sink=sink, tools=ToolRegistry(), clock=FakeClock())
    return await ex.run(TaskGraph("verify_test@1", nodes), RUN, Budget()), sink


async def test_clean_output_is_accepted_without_a_model_call():
    inv = generate(11, "en", defect_rate=0.0).truth
    gw = FakeGateway(reader(inv))
    out, _ = await run([produce(output(inv)), *verify_nodes(agent())], gw)
    assert out.status is RunStatus.SUCCEEDED and gw.calls == []
    assert list(out.node_status) == ["extraction.extract", "verify.arithmetic", "verify.identifiers",
                                     "verify.dates_codes", "verify.critic", "verify.join"]
    assert out.node_status["verify.critic"] is StepStatus.SKIPPED
    fv = final_verdict(out.results)
    assert fv is not None and fv.stage == 1 and fv.output_node == "extraction.extract"
    assert fv.verdict.verdict == "accept" and fv.verdict.confidence == 0.95


async def test_printed_inconsistency_is_confirmed_by_the_critic_and_accepted():
    s = first_with_defect("total_off_by_cent")
    gw = FakeGateway(reader(s.truth))
    out, sink = await run([produce(output(s.truth)), *verify_nodes(agent())], gw)
    assert [c.prompt_id for c in gw.calls] == ["verifier.critic"] and gw.calls[0].model == SONNET
    fv = final_verdict(out.results)
    assert fv is not None and fv.verdict.verdict == "accept"
    assert any(f.code == "arithmetic.total.confirmed_by_critic" and f.severity == "info" for f in fv.verdict.findings)
    lines = {(r.node_id, r.message_key) for r in sink.steps if r.message_key}
    assert {("verify.arithmetic", "verifier.check.flagged"), ("verify.critic", "verifier.critic.agreed"),
            ("verify.join", "verifier.verdict.accept")} <= lines


async def test_extraction_error_goes_through_one_revision():
    truth = generate(12, "en", defect_rate=0.0).truth
    bad = truth.model_copy(deep=True)
    bad.total_amount = str(Decimal(truth.total_amount) + 100)
    va = agent()
    feedback = []

    def on_revise(v, ctx):
        feedback.extend(f for f in v.findings if f.source == "critic" and f.expected)

        async def revise(c):
            return output(truth)

        node = Node("extraction.revise#1", "extraction", "revise", StepKind.LLM, revise, depends_on=("verify.join",))
        stage2 = va.checks_as_nodes("verify2", output_type=ExtractionOutput, output_node=node.id,
                                    evidence=lambda c: (DOC,), stage=2, revisions=1)
        return Expand((node, *stage2), join="verify2.join")

    seen = {}

    async def emit(ctx):
        seen["final"] = final_verdict(ctx.results)
        return "done"

    emit_node = Node("emit", "orchestrator", "emit", StepKind.DETERMINISTIC, emit, depends_on=("verify.join",),
                     trigger="all_done")
    gw = FakeGateway(reader(truth))
    out, _ = await run([produce(output(bad)), *verify_nodes(va, on_revise=on_revise), emit_node], gw)
    assert out.status is RunStatus.SUCCEEDED
    assert [c.prompt_id for c in gw.calls] == ["verifier.critic"]  # stage 2 is clean: no second critic call
    assert [(f.path, f.expected) for f in feedback] == [("total_amount", truth.total_amount)]
    stage1 = out.results["verify.join"]
    assert stage1.final is False and stage1.verdict.verdict == "revise"
    final = seen["final"]  # emit waited for the stage-2 join (semantics rule 10)
    assert final.stage == 2 and final.output_node == "extraction.revise#1" and final.verdict.verdict == "accept"


async def test_escalation_critic_gives_the_final_word():
    inv = generate(13, "en", defect_rate=0.0).truth.model_copy(deep=True)
    inv.buyer_trn = inv.buyer_trn[:14]  # the document really prints 14 digits
    unread = inv.model_copy(deep=True)
    unread.buyer_trn = ""  # the first critic cannot find it: a disagreement without an expected value
    gw = FakeGateway(by_prompt({"verifier.critic": reader(unread), "verifier.escalate": reader(inv)}))
    out, _ = await run([produce(output(inv)), *verify_nodes(agent())], gw)
    assert [(c.prompt_id, c.model) for c in gw.calls] == [("verifier.critic", SONNET), ("verifier.escalate", OPUS)]
    join = out.results["verify.join"]
    assert join.final is False and join.verdict.verdict == "escalate"
    assert list(out.node_status)[-2:] == ["verify.escalate", "verify.final"]
    fv = final_verdict(out.results)
    assert fv is not None and fv.verdict.verdict == "accept" and fv.verdict.critic_model == OPUS


async def test_failed_critic_sends_the_invoice_to_a_human():
    s = first_with_defect("total_off_by_cent")
    gw = FakeGateway({"verifier.critic": [PermanentModelError("provider said no")]})
    out, _ = await run([produce(output(s.truth)), *verify_nodes(agent(escalation=False))], gw)
    assert out.status is RunStatus.SUCCEEDED and out.node_status["verify.critic"] is StepStatus.FAILED
    fv = final_verdict(out.results)
    assert fv is not None and fv.verdict.verdict == "escalate"
    assert any(f.code == "verifier.critic_unavailable" for f in fv.verdict.findings)


async def test_missing_producer_output_escalates_without_model_calls():
    async def boom(ctx):
        raise PermanentModelError("no output")

    producer = Node("extraction.extract", "extraction", "extract", StepKind.LLM, boom, critical=False)
    gw = FakeGateway({})
    out, _ = await run([producer, *verify_nodes(agent())], gw)
    assert out.node_status["verify.arithmetic"] is StepStatus.SKIPPED and gw.calls == []
    fv = final_verdict(out.results)
    assert fv is not None and fv.verdict.verdict == "escalate" and fv.verdict.confidence == 0.0


async def test_inline_verify_for_other_tracks():
    s = first_with_defect("total_off_by_cent")
    va = agent()
    gw = FakeGateway(reader(s.truth))
    seen = {}

    async def fn(ctx):
        seen["v"] = await va.verify(ctx, output(s.truth), [DOC])

    await run([Node("inline", "fix", "verify", StepKind.LLM, fn)], gw)
    assert seen["v"].verdict == "accept" and len(gw.calls) == 1


def test_profiles_register_once_and_convert_to_proto():
    va = agent()
    with pytest.raises(ValueError, match="duplicate"):
        va.register(invoice_profile())
    with pytest.raises(ValueError, match="no verifier profile"):
        va.profile(CriticOutput)
    reg = AgentRegistry()
    reg.add_verifier_profile(invoice_profile())
    assert VerifierAgent.from_registry(reg).profile(ExtractionOutput).output_type is ExtractionOutput
    v = agent().profile(ExtractionOutput)
    assert v.max_revisions == 1 and v.accept_threshold == 0.85
    from ai.agents.verifier.core import Finding, VerdictResult
    msg = to_proto(VerdictResult("escalate", 0.4, (Finding("total_amount", "critic.mismatch", "block", "1", "2",
                                                            source="critic"),), critic_model=OPUS, revisions=1))
    assert msg.verdict == agents_pb2.VERDICT_ESCALATE and msg.revisions == 1 and msg.critic_model == OPUS
    assert (msg.findings[0].path, msg.findings[0].source, msg.findings[0].expected) == ("total_amount", "critic", "2")
