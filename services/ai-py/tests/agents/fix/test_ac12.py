"""AC-12: with FakeGateway and FakeValidatorClient the graph produces exactly the expected ProposalDraft and
FixTaskResult, and the proposal only contains re-validated changes."""

from fixhelpers import (
    EMIRATE,
    FIRM,
    INVOICE_ID,
    PAYABLE,
    llm_answer,
    make_invoice,
    mini_engine,
    request,
    task_input,
)

from ai.agents.fix import paths
from ai.gen.compliance.v1 import agents_pb2, validator_pb2
from ai.runtime.proposals import FieldChangeDraft, ProposalDraft, proposal_id
from ai.runtime.types import RunStatus
from ai.service import tasks_consumer


async def test_the_graph_proposes_exactly_the_expected_draft_and_result(harness):
    inv = make_invoice(seller__postal_address__country_subdivision="", totals__payable_amount="5.00")
    h = harness(answer=llm_answer((EMIRATE, "SHJ"), confidence=0.95))
    inp = task_input(inv)
    plan = await h.handler(request(inp))
    assert (plan.graph.workflow, plan.workflow_subject, plan.output_node) == (
        "fix_invoice@1", ("invoice", INVOICE_ID), "fix.result")
    out = await h.run(inp)

    assert out.status is RunStatus.SUCCEEDED
    ((ident, draft, pid),) = h.sink.proposals
    expected_detail = validator_pb2.FixProposalDetail(
        validation_run_id="run-9", payload_version=3, ruleset_version="pint-ae@test", errors_before=2,
        errors_after=0, resolved_rule_ids=["ibr-143-ae", "ibr-co-16"])
    expected_detail.notes.add(path=PAYABLE, rule_ids=["ibr-co-16"], source="deterministic",
                              rationale="ibr-co-16 failed")
    expected_detail.notes.add(path=EMIRATE, rule_ids=["ibr-143-ae"], source="llm", rationale=f"set {EMIRATE}")
    assert draft == ProposalDraft(
        kind="invoice.field_fix", target_type="invoice", target_id=INVOICE_ID,
        summary_key="P2Review.proposal.summary", summary_args={"changes": "2", "resolved": "2"},
        rationale=f"{PAYABLE}: ibr-co-16 failed; {EMIRATE}: set {EMIRATE}", confidence=0.95,
        changes=(FieldChangeDraft(PAYABLE, "5.00", "100.00"), FieldChangeDraft(EMIRATE, "", "SHJ")),
        detail=expected_detail)
    assert pid == proposal_id(ident, draft)

    result = out.results["fix.result"]
    assert result == validator_pb2.FixTaskResult(outcome="proposed", proposal_id=pid, changes=2)

    # what api-go reads: AgentTaskCompleted.output = Any(FixTaskResult)
    done = tasks_consumer._from_outcome(request(inp), plan, out)
    assert done.status == agents_pb2.AGENT_RUN_STATUS_SUCCEEDED and done.output.Is(
        validator_pb2.FixTaskResult.DESCRIPTOR)
    unpacked = validator_pb2.FixTaskResult()
    done.output.Unpack(unpacked)
    assert unpacked == result


async def test_every_proposed_change_was_revalidated_and_the_final_state_is_clean(harness):
    inv = make_invoice(seller__postal_address__country_subdivision="", totals__payable_amount="5.00",
                       buyer__postal_address__country_code="ae")
    h = harness(answer=llm_answer((EMIRATE, "SHJ"), ("buyer.postal_address.country_code", "AE")))
    out = await h.run(task_input(inv))
    ((_, draft, _),) = h.sink.proposals
    patched = paths.apply_changes(inv, [(c.path, c.new_value) for c in draft.changes])
    assert mini_engine(patched).issues == []  # the engine agrees: nothing left
    last_invoice, _ = h.validator.calls[-1]
    assert last_invoice == patched  # the very state that was proposed is the one validated last
    assert out.results["fix.result"].outcome == "proposed"


async def test_the_proposal_never_touches_an_agent_forbidden_path(harness):
    inv = make_invoice(invoice_number="", totals__payable_amount="5.00")
    h = harness(answer=llm_answer(("invoice_number", "INV-9")))
    out = await h.run(task_input(inv))
    ((_, draft, _),) = h.sink.proposals
    assert [c.path for c in draft.changes] == [PAYABLE]
    assert FIRM and out.results["fix.result"].changes == 1


async def test_a_redelivered_task_publishes_the_same_proposal_id(harness):
    inv = make_invoice(totals__payable_amount="5.00")
    ids = []
    for _ in range(2):
        h = harness()
        await h.run(task_input(inv))
        ids.append(h.sink.proposals[0][2])
    assert ids[0] == ids[1]  # deterministic id: api-go's insert is a no-op the second time
