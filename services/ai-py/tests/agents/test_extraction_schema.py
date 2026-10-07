import pytest

from ai.agents.extraction.normalize import (
    fold_text,
    normalize_date,
    normalize_decimal,
    normalize_invoice,
    normalize_trn,
    to_decimal,
)
from ai.agents.extraction.schema import (
    HEADER_PATHS,
    ExtractedInvoice,
    ExtractionOutput,
    InvoiceLine,
    TaxSubtotal,
    flatten,
    leaf,
)
from ai.gateway.schema import strict_schema


def test_field_set_is_the_spec_set():
    assert len(HEADER_PATHS) == 16
    inv = ExtractedInvoice(lines=[InvoiceLine(), InvoiceLine()], tax_breakdown=[TaxSubtotal()])
    flat = flatten(inv)
    assert len(flat) == 16 + 4 + 2 * 7
    assert "lines[1].price.net_price" in flat and "tax_breakdown[0].category.rate" in flat
    assert leaf("lines[1].tax.rate") == "tax.rate" and leaf("seller.name") == "seller.name"


def test_output_schema_is_structured_outputs_safe():
    s = strict_schema(ExtractionOutput)
    assert s["additionalProperties"] is False and s["required"] == ["invoice", "field_confidence", "language"]
    assert s["properties"]["language"]["enum"] == ["ar", "en", "mixed"]


@pytest.mark.parametrize(("raw", "want"), [
    ("1,050.00", "1050.00"), ("١٬٠٥٠٫٠٠", "1050.00"), ("۱۲۳٫۴", "123.4"), ("AED 5,000", "5000"),
    ("5%", "5"), ("(12.00)", "-12.00"), ("  ", ""), ("12.3.4", "12.3.4"), ("د.إ ٥٠٫٠٠", "50.00"),
])
def test_normalize_decimal(raw, want):
    assert normalize_decimal(raw) == want


def test_to_decimal_compares_numerically():
    assert to_decimal("5.00") == to_decimal("٥") and to_decimal("abc") is None and to_decimal("") is None


@pytest.mark.parametrize(("raw", "want"), [
    ("2026-03-15", "2026-03-15"), ("15/03/2026", "2026-03-15"), ("١٥-٠٣-٢٠٢٦", "2026-03-15"),
    ("2026/3/5", "2026-03-05"), ("15 March 2026", "2026-03-15"), ("15 مارس 2026", "2026-03-15"),
    ("5 آذار 2026", "2026-03-05"), ("", ""), ("next week", "next week"), ("31/13/2026", "31/13/2026"),
])
def test_normalize_date(raw, want):
    assert normalize_date(raw) == want


def test_trn_and_fold():
    assert normalize_trn("١٠٠ ٢٣٤-٥٦٧٨ ٠٠٠٠٣") == "100234567800003"
    assert fold_text("  شركة   الأمل ") == fold_text("شركه الامل")
    assert fold_text("ACME Trading") == "acme trading"


def test_normalize_invoice_is_a_copy():
    inv = ExtractedInvoice(issue_date="15/03/2026", currency="aed", total_amount="١٬٠٥٠٫٠٠",
                           lines=[InvoiceLine(quantity="٢", unit_code="h87")])
    out = normalize_invoice(inv)
    assert (out.issue_date, out.currency, out.total_amount) == ("2026-03-15", "AED", "1050.00")
    assert (out.lines[0].quantity, out.lines[0].unit_code) == ("2", "H87")
    assert inv.issue_date == "15/03/2026"
