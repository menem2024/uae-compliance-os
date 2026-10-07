"""The fix_invoice@1 graph, node by node, against the scripted rule engine and a FakeGateway."""

import pytest
from fixhelpers import (
    BUYER_COUNTRY,
    EMIRATE,
    LINE_EXT,
    PAYABLE,
    TAX_EXCL,
    issue,
    llm_answer,
    make_invoice,
    mini_engine,
    task_input,
)

from ai.agents.fix import nodes, paths
from ai.agents.fix.config import FixConfig
from ai.agents.fix.convert import error_issues
from ai.agents.fix.types import DeterministicResult, LLMResult, VerifiedResult
from ai.gen.compliance.v1 import validator_pb2
from ai.runtime.errors import ToolTransientError
from ai.runtime.types import Budget, RunStatus, StepStatus

NO_EMIRATE = {"seller__postal_address__country_subdivision": ""}


def result(out, node):
    return out.results[node]


async def test_deterministic_settles_the_totals_chain_in_three_rounds_and_skips_forbidden_paths(harness):
    inv = make_invoice(totals__line_extension_amount="90.00", totals__tax_exclusive_amount="90.00", totals__payable_amount="90.00", invoice_number="")
    h = harness()
    out = await h.run(task_input(inv))
    det: DeterministicResult = result(out, "fix.deterministic")
    assert [(c.path, c.old_value, c.new_value) for c in det.changes] == [
        (LINE_EXT, "90.00", "100.00"), (TAX_EXCL, "90.00", "100.00"), (PAYABLE, "90.00", "100.00")]
    assert all(c.source == "deterministic" for c in det.changes)
    # 1 (the original) + 3 rounds, each re-validated; invoice_number is suggested but AgentForbidden
    assert len(h.validator.calls) >= 4
    assert [i.rule_id for i in error_issues(det.after)] == ["ibr-006"]
    assert paths.get_value(det.invoice, "invoice_number") == ""
    assert paths.get_value(inv, LINE_EXT) == "90.00"  # the input invoice is never mutated


async def test_deterministic_stops_when_no_suggestion_is_new(harness):
    h = harness()
    out = await h.run(task_input(make_invoice()))  # valid: nothing to do
    det = result(out, "fix.deterministic")
    assert det.changes == () and len(h.validator.calls) == 1
    assert result(out, "fix.result").outcome == "no_fix"


async def test_deterministic_never_applies_more_than_three_rounds(harness):
    # an engine whose suggestion always differs from the current value: the loop must stop at 3 rounds
    n = {"i": 0}

    def engine(inv, _v):
        n["i"] += 1
        return validator_pb2.ValidationRun(issues=[issue("loop", "currency", suggested=f"V{n['i']}")])

    h = harness(engine=engine)
    out = await h.run(task_input(make_invoice(), run=engine(make_invoice(), "")))
    assert len(h.validator.calls) == 1 + nodes.MAX_ROUNDS + (0)  # 1 original + 3 rounds (verify adds none)
    assert result(out, "fix.deterministic").changes[0].path == "currency"


@pytest.mark.parametrize(("mode", "cfg", "asks"), [
    ("on_demand", FixConfig(), True),
    ("auto", FixConfig(), False),
    ("auto", FixConfig(auto_llm=True), True),
    ("on_demand", FixConfig(untrusted_llm=True), False),
    ("auto", FixConfig(auto_llm=True, untrusted_llm=True), False),
])
async def test_the_llm_step_is_gated_by_mode_and_config(harness, mode, cfg, asks):
    inv = make_invoice(**NO_EMIRATE)
    h = harness(answer=llm_answer((EMIRATE, "SHJ")), config=cfg)
    out = await h.run(task_input(inv, mode=mode))
    assert bool(h.gateway.calls) is asks
    assert out.node_status["fix.llm"] is (StepStatus.SUCCEEDED if asks else StepStatus.SKIPPED)
    assert result(out, "fix.result").outcome == ("proposed" if asks else "no_fix")


async def test_the_llm_step_is_skipped_when_every_remaining_issue_has_a_computed_value(harness):
    inv = make_invoice(totals__payable_amount="5.00")
    h = harness()
    out = await h.run(task_input(inv))
    assert h.gateway.calls == [] and out.node_status["fix.llm"] is StepStatus.SKIPPED
    assert result(out, "fix.result").outcome == "proposed"  # the deterministic fix alone


async def test_the_model_request_is_static_text_plus_the_invoices_own_content(harness):
    from ai.agents.fix.prompts import PROMPT_ID, PROMPT_VERSION, SYSTEM

    inv = make_invoice(**NO_EMIRATE)
    h = harness(answer=llm_answer((EMIRATE, "SHJ")))
    await h.run(task_input(inv))
    (req,) = h.gateway.calls
    assert (req.prompt_id, req.prompt_version, req.system) == (PROMPT_ID, PROMPT_VERSION, SYSTEM)
    assert (req.model, req.effort, req.max_tokens, req.tools) == ("claude-sonnet-5", "low", 2048, ())
    (msg,) = req.messages
    (part,) = msg.parts
    assert '"invoice_number":"INV-7"' in part.text and EMIRATE in part.text
    assert "run-9" not in part.text and "t-1" not in part.text  # no run, task, firm or tenant data
    assert req.meta is not None and req.meta.firm_id.endswith("f1")  # set by the runtime, not the agent


async def test_a_change_that_adds_an_error_rule_id_is_dropped_and_the_rest_is_proposed(harness):
    inv = make_invoice(**NO_EMIRATE, buyer__postal_address__country_code="ae")
    h = harness(answer=llm_answer((EMIRATE, "XX"), (BUYER_COUNTRY, "AE")))  # "XX" makes AE-EMIRATE-X fire
    out = await h.run(task_input(inv))
    v: VerifiedResult = result(out, "fix.verify")
    assert [(c.path, c.new_value) for c in v.changes] == [(BUYER_COUNTRY, "AE")]
    assert v.candidate.after_rule_ids == ["ibr-143-ae"] and v.verdict == "accept"
    proposal = h.sink.proposals[0][1]
    assert [(c.path, c.new_value) for c in proposal.changes] == [(BUYER_COUNTRY, "AE")]


async def test_culprits_are_removed_one_at_a_time_within_the_evaluation_cap(harness):
    n = 20
    inv = make_invoice()
    for i in range(n):
        paths.set_value(inv, f"lines[{i}].unit_code", "bad")
        paths.set_value(inv, f"lines[{i}].price.base_quantity_unit_code", "BAD")

    def engine(x, _v):
        out = []
        for i in range(n):
            if paths.get_value(x, f"lines[{i}].unit_code") == "BAD":
                out.append(issue("NEW-RULE", f"lines[{i}].unit_code"))  # every LLM fix adds a new error id
            if paths.get_value(x, f"lines[{i}].unit_code") == "bad":
                out.append(issue("ibr-cl-23", f"lines[{i}].unit_code"))
        return validator_pb2.ValidationRun(issues=out)

    answer = llm_answer(*[(f"lines[{i}].unit_code", "BAD") for i in range(n)])
    h = harness(answer=answer, engine=engine)
    out = await h.run(task_input(inv, run=engine(inv, "")))
    v = result(out, "fix.verify")
    assert v.changes == () and v.verdict != "accept"
    # det: 1 validation, verify: one evaluation per LLM change plus the all-changes one: at most 21
    assert len(h.validator.calls) - 1 <= nodes.MAX_EVALUATIONS
    assert result(out, "fix.result").outcome == "no_fix" and h.sink.proposals == []


async def test_the_culprit_is_the_change_nearest_the_new_issue():
    from ai.agents.fix.types import Change

    cs = [Change("seller.name", "", "x", (), "llm", ""), Change("lines[1].unit_code", "", "y", (), "llm", ""),
          Change("lines[1].tax.code", "", "z", (), "llm", "")]
    assert nodes.culprit_index(cs, [issue("n", "lines[1].tax.rate")]) == 2  # shares lines[1].tax: nearest
    assert nodes.culprit_index(cs, [issue("n", "lines[1].price.net")]) == 1  # a tie on lines[1]: the earliest
    assert nodes.culprit_index(cs, [issue("n", "currency")]) == 2  # nothing shared: the last


async def test_an_unsure_model_does_not_sink_the_computed_fix(harness):
    inv = make_invoice(totals__payable_amount="5.00", **NO_EMIRATE)
    h = harness(answer=llm_answer((EMIRATE, "SHJ"), confidence=0.5))  # below the 0.85 threshold
    out = await h.run(task_input(inv))
    v = result(out, "fix.verify")
    assert [c.path for c in v.changes] == [PAYABLE] and v.verdict == "accept" and v.candidate.confidence == 1.0
    assert result(out, "fix.result").outcome == "proposed" and result(out, "fix.result").changes == 1


async def test_a_low_confidence_model_alone_is_not_proposed(harness):
    h = harness(answer=llm_answer((EMIRATE, "SHJ"), confidence=0.5))
    out = await h.run(task_input(make_invoice(**NO_EMIRATE)))
    r = result(out, "fix.result")
    assert (r.outcome, r.proposal_id, r.changes) == ("not_improving", "", 0) or r.outcome == "not_improving"
    assert h.sink.proposals == []


async def test_a_model_failure_leaves_the_computed_fix_standing(harness):
    from ai.gateway.errors import ModelRefusal

    inv = make_invoice(totals__payable_amount="5.00", **NO_EMIRATE)
    h = harness(answer=ModelRefusal("policy"))
    out = await h.run(task_input(inv))
    assert out.status is RunStatus.SUCCEEDED and out.node_status["fix.llm"] is StepStatus.FAILED
    assert result(out, "fix.result").outcome == "proposed"


async def test_a_change_that_leaves_every_target_standing_is_not_proposed(harness):
    # an engine that never reacts to the change: errors do not drop, so nothing is proposed
    h = harness(answer=llm_answer((EMIRATE, "SHJ")), engine=lambda x, v: validator_pb2.ValidationRun(
        issues=[issue("ibr-143-ae", EMIRATE)]))
    inv = make_invoice(**NO_EMIRATE)
    out = await h.run(task_input(inv, run=validator_pb2.ValidationRun(issues=[issue("ibr-143-ae", EMIRATE)])))
    assert result(out, "fix.result").outcome == "not_improving" and h.sink.proposals == []


async def test_a_transient_validator_error_is_retried_by_the_node(harness):
    calls = {"n": 0}

    def engine(x, v):
        calls["n"] += 1
        if calls["n"] == 1:
            raise ToolTransientError("down")
        return mini_engine(x, v)

    h = harness(engine=engine)
    out = await h.run(task_input(make_invoice(totals__payable_amount="5.00")))
    assert out.status is RunStatus.SUCCEEDED and result(out, "fix.result").outcome == "proposed"
    assert "retrying" in h.sink.statuses("fix.deterministic")


async def test_a_rejected_request_fails_the_run_as_bad_request(harness):
    h = harness(engine=lambda x, v: (_ for _ in ()).throw(ValueError("unknown ruleset")))
    out = await h.run(task_input(make_invoice(**NO_EMIRATE)))
    assert out.status is RunStatus.FAILED and out.error_code == "bad_request"


async def test_the_budget_caps_apply(harness):
    h = harness(answer=llm_answer((EMIRATE, "SHJ")))
    out = await h.run(task_input(make_invoice(**NO_EMIRATE)), Budget(max_llm_calls=0))
    assert out.status is RunStatus.BUDGET_EXCEEDED
    assert h.gateway.calls == [] and h.sink.proposals == []


async def test_propose_refuses_a_forbidden_path_even_when_the_verdict_accepted(harness):
    """The AgentForbidden guard runs before ctx.propose, not only in api-go."""
    from ai.agents.fix.convert import error_issues  # noqa: F401
    from ai.agents.fix.types import CandidateChange, Change, FixCandidate

    forbidden = Change("invoice_number", "", "INV-9", ("r",), "deterministic", "x")
    cand = FixCandidate(changes=[CandidateChange(path="invoice_number", old_value="", new_value="INV-9",
                                                 rule_ids=["r"], source="deterministic", rationale="x")],
                        errors_before=2, errors_after=1, before_rule_ids=["r"], after_rule_ids=[],
                        target_rule_ids=["r"], target_paths=[], target_keys=[], after_keys=[], confidence=1.0)
    h = harness()
    inp = task_input(make_invoice())

    class Ctx:
        def __init__(self) -> None:
            self.results = {"fix.verify": VerifiedResult(cand, "accept", 1.0, (forbidden,))}
            self.proposed = False

        def result(self, node_id, typ):
            return self.results[node_id]

        def emit(self, *a, **k):
            pass

        async def propose(self, draft):
            self.proposed = True
            return "p"

    ctx = Ctx()
    out = await nodes.propose(ctx, inp)  # type: ignore[arg-type]
    assert out.outcome == "not_improving" and ctx.proposed is False and h.sink.proposals == []


async def test_nothing_is_written_only_proposals_and_feed_lines_reach_the_sink(harness):
    h = harness(answer=llm_answer((EMIRATE, "SHJ")))
    out = await h.run(task_input(make_invoice(**NO_EMIRATE, totals__payable_amount="5.00")))
    assert out.status is RunStatus.SUCCEEDED
    assert len(h.sink.proposals) == 1  # one proposal per task (api-go allows one open per invoice)
    keys = [r.message_key for r in h.sink.steps if r.message_key]
    assert all(k.startswith("P2Agents.feed.fix.") or k == "proposal.created" for k in keys)
    assert {"P2Agents.feed.fix.deterministic_applied", "P2Agents.feed.fix.llm_requested",
            "P2Agents.feed.fix.verified", "P2Agents.feed.fix.proposed"} <= set(keys)
    for r in (r for r in h.sink.steps if r.message_key.startswith("P2Agents.feed.fix.")):
        assert all(v.isdigit() for v in r.message_args.values()), r.message_args  # counts only


def test_llm_wanted_requires_a_deterministic_result():
    assert not nodes.llm_wanted({}, FixConfig(), "on_demand")
    assert not isinstance(LLMResult((), 0.0, ()), DeterministicResult)
