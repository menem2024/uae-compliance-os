"""`intake.attribute` (spec section 5.2) and `direction_of`: a document.attribution proposal when exactly one
other candidate's TRN matches the extraction and the chosen ClientCompany's does not."""

import uuid

import pytest

from ai.agents.extraction.schema import ExtractedInvoice, ExtractionOutput, FieldConfidence
from ai.agents.orchestrator.attribute import AttributionResult, attribute_node, direction_of
from ai.gen.compliance.v1 import documents_pb2
from ai.runtime.clock import FakeClock
from ai.runtime.events import MemorySink
from ai.runtime.graph import GraphExecutor, Node, TaskGraph
from ai.runtime.proposals import FieldChangeDraft
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import Budget, RunIdentity, RunStatus, StepKind, StepStatus

DOC = "00000000-0000-4000-8000-00000000d001"
CHOSEN = "00000000-0000-4000-8000-00000000c001"
OTHER = "00000000-0000-4000-8000-00000000c002"
THIRD = "00000000-0000-4000-8000-00000000c003"
SELLER = "100000000000001"
BUYER = "100000000000002"
UNRELATED = "100000000000009"


def cand(cc: str, trn: str) -> documents_pb2.ClientCompanyRef:
    return documents_pb2.ClientCompanyRef(client_company_id=cc, name=f"Company {cc[-1]}", trn=trn)


def extraction(seller: str = SELLER, buyer: str = BUYER) -> ExtractionOutput:
    return ExtractionOutput(invoice=ExtractedInvoice(invoice_number="INV-1", seller_trn=seller, buyer_trn=buyer),
                            field_confidence=[FieldConfidence(path="seller_trn", confidence=0.97)], language="en")


async def attribute(candidates, out=None, sink=None):
    async def produce(ctx):
        return out or extraction()

    sink = sink or MemorySink()
    nodes = [Node("extraction.extract", "extraction", "extract", StepKind.LLM, produce),
             attribute_node(candidates, CHOSEN, DOC)]
    ex = GraphExecutor(gateway=None, sink=sink, tools=ToolRegistry(), clock=FakeClock(),  # type: ignore[arg-type]
                       sleep=_no_sleep)
    run = RunIdentity(str(uuid.uuid4()), "firm-1", CHOSEN, "attribute_test@1", "document", DOC)
    return await ex.run(TaskGraph("attribute_test@1", nodes), run, Budget()), sink


async def _no_sleep(_s: float) -> None:
    return None


async def test_one_other_matching_candidate_raises_one_proposal():
    out, sink = await attribute([cand(CHOSEN, UNRELATED), cand(OTHER, SELLER), cand(THIRD, "")])
    assert out.results["intake.attribute"] == AttributionResult(OTHER)
    assert len(sink.proposals) == 1
    _run, draft, _pid = sink.proposals[0]
    assert (draft.kind, draft.target_type, draft.target_id) == ("document.attribution", "document", DOC)
    assert draft.changes == (FieldChangeDraft("client_company_id", CHOSEN, OTHER),
                             FieldChangeDraft("direction", "unknown", "issued"))
    assert draft.summary_key == "P1Agents.proposal.attribution" and dict(draft.summary_args) == {"trn": SELLER}
    assert draft.confidence == 0.97
    assert "Company" not in repr(draft)  # names never travel in the proposal (PII)
    lines = [(r.node_id, r.message_key) for r in sink.steps if r.message_key]
    assert ("intake.attribute", "proposal.created") in lines


async def test_buyer_match_proposes_received():
    _out, sink = await attribute([cand(CHOSEN, UNRELATED), cand(OTHER, BUYER)])
    assert sink.proposals[0][1].changes[1] == FieldChangeDraft("direction", "unknown", "received")


async def test_chosen_candidate_already_matches():
    out, sink = await attribute([cand(CHOSEN, BUYER), cand(OTHER, SELLER)])
    assert out.results["intake.attribute"] == AttributionResult(None) and sink.proposals == []


async def test_no_other_candidate_matches():
    out, sink = await attribute([cand(CHOSEN, UNRELATED), cand(OTHER, "100000000000008")])
    assert out.results["intake.attribute"] == AttributionResult(None) and sink.proposals == []


async def test_two_matching_candidates_are_ambiguous():
    _out, sink = await attribute([cand(CHOSEN, UNRELATED), cand(OTHER, SELLER), cand(THIRD, BUYER)])
    assert sink.proposals == []


async def test_a_failed_proposal_publish_is_retried_as_transient_and_never_fails_the_run():
    class BrokenSink(MemorySink):
        async def proposal(self, identity, draft, proposal_id):
            raise ConnectionError("nats down")

    out, sink = await attribute([cand(CHOSEN, UNRELATED), cand(OTHER, SELLER)], sink=BrokenSink())
    assert out.status is RunStatus.SUCCEEDED
    assert out.node_status["intake.attribute"] is StepStatus.FAILED
    assert sink.statuses("intake.attribute") == ["started", "retrying", "started", "retrying", "started", "failed"]
    assert [r.error_code for r in sink.steps if r.node_id == "intake.attribute"][-1] == "tool_transient"


def test_attribute_is_a_non_critical_intake_node_after_the_first_extraction():
    node = attribute_node([], CHOSEN, DOC)
    assert (node.id, node.agent, node.kind, node.depends_on, node.critical) == (
        "intake.attribute", "intake", StepKind.DETERMINISTIC, ("extraction.extract",), False)


@pytest.mark.parametrize(("trn", "want"), [(SELLER, "issued"), (BUYER, "received"), (UNRELATED, "unknown"),
                                          ("", "unknown")])
def test_direction_of(trn, want):
    assert direction_of(ExtractedInvoice(seller_trn=SELLER, buyer_trn=BUYER), trn) == want
