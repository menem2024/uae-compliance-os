"""Invoice checks: silent on clean generated invoices, loud on defects and on the verifier-suite corruptions."""

import pytest

from ai.agents.extraction.schema import ExtractionOutput, FieldConfidence
from ai.agents.verifier.invoice import (
    CRITICAL_PATHS,
    INVOICE_CHECKS,
    ArithmeticCheck,
    DatesCodesCheck,
    IdentifiersCheck,
    extraction_confidence,
    invoice_critic_when,
    invoice_findings,
    low_confidence_paths,
)
from ai.synthetic.corrupt import CORRUPTIONS, corrupt
from ai.synthetic.generator import generate

EXPECTED_DEFECT_CODES = {
    "total_off_by_cent": "arithmetic.total",
    "seller_trn_14_digits": "identifiers.trn_shape",
    "line_net_off_by_cent": "arithmetic.line_total",
}


def flagged(inv) -> list[str]:
    return [f.code for f in invoice_findings(inv) if f.severity != "info"]


def test_families_and_order():
    assert [c.code_family for c in INVOICE_CHECKS] == ["arithmetic", "identifiers", "dates_codes"]


def test_clean_generated_invoices_raise_nothing():
    for lang in ("ar", "en"):
        for seed in range(150):
            inv = generate(seed, lang, defect_rate=0.0).truth
            assert flagged(inv) == [], (lang, seed)


@pytest.mark.parametrize("defect", sorted(EXPECTED_DEFECT_CODES))
def test_printed_defects_are_flagged(defect):
    hits = 0
    for seed in range(400):
        s = generate(seed, "en")
        if s.defects == (defect,):
            assert EXPECTED_DEFECT_CODES[defect] in flagged(s.truth), seed
            hits += 1
    assert hits >= 3


def test_every_corruption_is_flagged_or_reviewed_by_the_critic():
    seen: set[str] = set()
    for seed in range(300):
        truth = generate(seed, "ar", defect_rate=0.0).truth
        bad, c = corrupt(truth, seed)
        seen.add(c.code)
        findings = invoice_findings(bad)
        touched = {p for f in findings if f.severity != "info" for p in (f.path, *f.related)}
        assert c.path in touched or c.path in CRITICAL_PATHS, (seed, c)
    assert seen == set(CORRUPTIONS)


def test_arithmetic_details():
    inv = generate(3, "en", defect_rate=0.0).truth.model_copy(deep=True)
    inv.lines[0].net_amount = "999999.99"
    codes = {f.code: f for f in ArithmeticCheck.check(inv)}
    assert codes["arithmetic.line_net"].related == ("lines[0].quantity", "lines[0].price.net_price")
    assert "arithmetic.line_total" in codes and codes["arithmetic.line_total"].severity == "block"


def _out_of_range(inv) -> dict[str, str]:
    return {f.path: f.severity for f in ArithmeticCheck.check(inv) if f.code == "arithmetic.out_of_range"}


def test_a_27_digit_quantity_is_flagged_not_a_crash():
    """Review B #3: quantize raised decimal.InvalidOperation (>= ~1e26) and failed the whole document."""
    inv = generate(3, "en", defect_rate=0.0).truth.model_copy(deep=True)
    inv.lines[0].quantity = "1" * 27
    assert _out_of_range(inv) == {"lines[0].quantity": "block", "lines[0].net_amount": "block"}


def test_a_huge_taxable_amount_is_flagged_not_a_crash():
    inv = generate(3, "en", defect_rate=0.0).truth.model_copy(deep=True)
    inv.tax_breakdown[0].taxable_amount = "9" * 30
    assert _out_of_range(inv)["tax_breakdown[0].taxable_amount"] == "block"


def test_a_product_too_large_for_cents_is_flagged():
    inv = generate(3, "en", defect_rate=0.0).truth.model_copy(deep=True)
    inv.lines[0].quantity = inv.lines[0].price.net_price = "9" * 14  # each plausible-looking, product ~1e28
    findings = {f.path: f for f in ArithmeticCheck.check(inv) if f.code == "arithmetic.out_of_range"}
    assert findings["lines[0].net_amount"].related == ("lines[0].quantity", "lines[0].price.net_price")


def test_identifier_and_date_details():
    inv = generate(4, "en", defect_rate=0.0).truth.model_copy(deep=True)
    inv.buyer_trn = inv.seller_trn
    inv.invoice_number = ""
    assert {f.code for f in IdentifiersCheck.check(inv)} == {"identifiers.same_party",
                                                              "identifiers.invoice_number_missing"}
    inv.issue_date, inv.currency, inv.lines[0].tax.code = "2026-02-30", "DIRHAM", "X"
    assert {f.code for f in DatesCodesCheck.check(inv)} >= {"dates_codes.issue_date_format",
                                                             "dates_codes.currency_code", "dates_codes.tax_category"}


def test_confidence_and_critic_trigger():
    inv = generate(5, "en", defect_rate=0.0).truth
    none = ExtractionOutput(invoice=inv, field_confidence=[], language="en")
    assert extraction_confidence(none) == 0.9 and not invoice_critic_when(none, [])
    low = ExtractionOutput(invoice=inv, language="en", field_confidence=[
        FieldConfidence(path="total_amount", confidence=0.5), FieldConfidence(path="currency", confidence=1.5),
        FieldConfidence(path="lines[99].net_amount", confidence=0.1)])
    assert extraction_confidence(low) == 0.75  # mean of 0.5 and the clamped 1.0; unknown paths ignored
    assert low_confidence_paths(low) == ["total_amount"] and invoice_critic_when(low, [])
