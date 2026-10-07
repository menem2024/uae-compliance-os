"""The verdict rule and the critic-confirmation rule (contract section 6, binding)."""

from pydantic import BaseModel

from ai.agents.verifier.core import (
    CriticResult,
    Finding,
    StageVerdict,
    VerdictResult,
    VerifierProfile,
    confirm_by_critic,
    decide,
    final_verdict,
    paths_of,
)


class Out(BaseModel):
    conf: float = 1.0


PROFILE = VerifierProfile(output_type=Out, checks=(), producer_confidence=lambda o: o.conf)
BLOCK = Finding("total_amount", "arithmetic.total", "block", related=("vat_amount",))
WARN = Finding("currency", "dates_codes.currency_code", "warn")
CRITIC_EXPECTED = Finding("total_amount", "critic.mismatch", "block", observed="10", expected="100", source="critic")
CRITIC_NO_VALUE = Finding("total_amount", "critic.not_found", "warn", observed="10", source="critic")


def test_accept_needs_no_block_and_threshold():
    assert decide(PROFILE, Out(conf=0.95), [], revisions=0).verdict == "accept"
    v = decide(PROFILE, Out(conf=0.95), [WARN], revisions=0)
    assert v.verdict == "accept" and v.confidence == 0.855
    assert decide(PROFILE, Out(conf=0.9), [WARN], revisions=0).verdict == "escalate"  # 0.81 < 0.85


def test_block_halves_and_never_accepts():
    v = decide(PROFILE, Out(conf=1.0), [BLOCK], revisions=0)
    assert v.confidence == 0.5 and v.verdict == "escalate"


def test_revise_needs_a_critic_expected_value_and_revision_room():
    assert decide(PROFILE, Out(), [BLOCK, CRITIC_EXPECTED], revisions=0).verdict == "revise"
    assert decide(PROFILE, Out(), [BLOCK, CRITIC_EXPECTED], revisions=1).verdict == "escalate"
    assert decide(PROFILE, Out(), [BLOCK, CRITIC_NO_VALUE], revisions=0).verdict == "escalate"


def test_confidence_is_clamped_and_rounded():
    assert decide(PROFILE, Out(conf=1.7), [], revisions=0).confidence == 1.0
    assert decide(PROFILE, Out(conf=0.9), [WARN, WARN, WARN], revisions=0).confidence == 0.656


def test_critic_confirmation_downgrades_only_fully_reviewed_undisputed_findings():
    reviewed = CriticResult(findings=(), reviewed=frozenset({"total_amount", "vat_amount"}))
    (f,) = confirm_by_critic([BLOCK], reviewed)
    assert f.severity == "info" and f.code == "arithmetic.total.confirmed_by_critic"
    partial = CriticResult(findings=(), reviewed=frozenset({"total_amount"}))  # related path not re-read
    assert confirm_by_critic([BLOCK], partial)[0].severity == "block"
    disputed = CriticResult(findings=(CRITIC_EXPECTED,), reviewed=frozenset({"total_amount", "vat_amount"}))
    assert confirm_by_critic([BLOCK], disputed)[0].severity == "block"
    assert confirm_by_critic([BLOCK], None) == (BLOCK,)
    assert paths_of([BLOCK, WARN]) == {"total_amount", "vat_amount", "currency"}


def test_confirmed_findings_leave_the_verdict_to_the_producer():
    reviewed = CriticResult(findings=(), reviewed=paths_of([BLOCK]))
    v = decide(PROFILE, Out(conf=0.9), confirm_by_critic([BLOCK], reviewed), revisions=0)
    assert v.verdict == "accept" and v.confidence == 0.9


def test_final_verdict_picks_the_highest_final_stage():
    v = VerdictResult("accept", 0.9, ())
    results = {"a": StageVerdict(1, "x", v, final=False), "b": StageVerdict(2, "y", v, final=True), "c": 3}
    assert final_verdict(results) == results["b"]
    assert final_verdict({"a": results["a"]}) is None
