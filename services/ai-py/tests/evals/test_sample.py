"""The sample runner: deterministic stratified selection, per-field outcomes that agree with the binding
scorer, error cases scoring zero, and a summary with no document content."""

import json
from pathlib import Path

from ai.agents.extraction.schema import flatten
from ai.evals.core import CaseFailed
from ai.evals.sample import CaseResult, field_outcomes, select_sample, summarize, to_markdown
from ai.evals.scoring import score_invoices
from ai.evals.suites.extraction import ExtractionSuite
from ai.settings import Settings

ROOT = Path(__file__).resolve().parents[2] / "evals" / "datasets"


def _cases():
    return list(ExtractionSuite(Settings(), ROOT).cases("full"))


def test_selection_is_stratified_deterministic_and_sorted():
    a, b = select_sample(_cases(), 24), select_sample(list(reversed(_cases())), 24)
    assert [c.case_id for c in a] == [c.case_id for c in b] and len(a) == 24
    ids = [c.case_id for c in a]
    assert ids == sorted(ids)
    tags = [c.tags for c in a]
    assert sum(any(t.startswith("defect:") for t in x) for x in tags) == 12
    assert sum("lang:ar" in x for x in tags) == 12 and sum("format:pdf" in x for x in tags) == 12


def test_field_outcomes_sum_to_the_scorer_counts():
    c = _cases()[0]
    inv = c.truth.model_copy(deep=True)
    inv.invoice_number = inv.invoice_number + "x"
    acc, d = score_invoices([c.truth], [inv])
    per = field_outcomes(flatten(c.truth), flatten(inv))
    assert sum(n for n, _ in per.values()) == d["counted"] and sum(k for _, k in per.values()) == d["correct"]
    assert per["invoice_number"] == (1, 0) and acc < 1.0


def test_summary_counts_errors_as_zero_and_holds_no_values():
    ok = CaseResult("c1", ["lang:en", "format:pdf"], counted=4, correct=4, exact=True, verdict="accept",
                    per_field={"total_amount": (1, 1), "invoice_number": (3, 3)})
    bad = CaseResult("c2", ["lang:ar", "format:image", "defect:x"], error="model_permanent", counted=4,
                     wrong_fields=["invoice_number", "total_amount"],
                     per_field={"total_amount": (1, 0), "invoice_number": (3, 0)})
    s = summarize([ok, bad], gateway="openai_compat", models=["m"], planned=2)
    assert s["overall"]["field_accuracy"] == 0.5 and s["overall"]["invoice_exact_match_rate"] == 0.5
    assert s["failure_causes"]["errors"] == {"model_permanent": 1} and s["verifier"]["accept_rate"] == 1.0
    assert s["per_field"]["total_amount"]["accuracy"] == 0.5
    assert "case_id" in json.dumps(s) and "# Extraction accuracy sample" in to_markdown(s)
    assert CaseFailed("x").code == "x"


class _QuotaSuite:
    """A suite stand-in whose model is out of daily quota from the second case on."""

    def __init__(self) -> None:
        self.ran: list[str] = []

    async def run_case(self, case, env, hook):
        self.ran.append(case.case_id)
        raise CaseFailed("quota_exhausted")


async def test_quota_exhaustion_is_not_retried_with_case_backoff_and_stops_the_sample():
    from ai.evals.sample import run_case, run_sample

    cases = select_sample(_cases(), 6)
    slept: list[float] = []

    async def sleep(s: float) -> None:
        slept.append(s)

    suite = _QuotaSuite()
    res = await run_sample(suite, cases, gateway=None, settings=Settings(), concurrency=1, sleep=sleep)  # type: ignore[arg-type]
    assert slept == [] and [r.error for r in res] == ["quota_exhausted"] and len(suite.ran) == 1
    assert res[0].attempts == 1
    # a plain transient error still gets the case-level backoff
    class Flaky(_QuotaSuite):
        async def run_case(self, case, env, hook):
            raise CaseFailed("model_transient")

    r = await run_case(Flaky(), None, cases[0], None, sleep)  # type: ignore[arg-type]
    assert r.error == "model_transient" and r.attempts == 4 and slept == [30.0, 60.0, 120.0]


def test_verifier_is_scored_on_extraction_fidelity_not_on_source_defects():
    def case(cid, tags, *, exact, verdict, codes=()):
        return CaseResult(cid, tags, counted=4, correct=4 if exact else 3, exact=exact, verdict=verdict,
                          finding_codes=list(codes), per_field={"total_amount": (1, 1 if exact else 0)})

    rs = [case("a", ["lang:en", "format:pdf"], exact=True, verdict="accept"),
          case("b", ["lang:en", "format:pdf", "defect:missing_buyer_trn"], exact=True, verdict="accept"),
          case("c", ["lang:en", "format:pdf"], exact=False, verdict="accept",
               codes=["arithmetic.line_net.confirmed_by_critic"]),
          case("d", ["lang:en", "format:pdf"], exact=False, verdict="escalate"),
          case("e", ["lang:en", "format:pdf"], exact=True, verdict="escalate")]
    v = summarize(rs, gateway="g", models=["m"], planned=5)["verifier"]
    assert v["by_extraction"] == {
        "correct": {"verified": 3, "accept": 2, "review": 1},
        "wrong": {"verified": 2, "accept": 1, "review": 1, "false_accept_rate": 0.5}}
    assert v["by_defect"] == {"missing_buyer_trn": {"verified": 1, "accept": 1}}
    assert v["confirmed_by_critic_accepts_on_wrong_extractions"] == 1
    md = to_markdown(summarize(rs, gateway="g", models=["m"], planned=5))
    assert "Verifier by extraction correctness" in md and "false accept" in md.lower()
