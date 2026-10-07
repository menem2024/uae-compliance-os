import dataclasses
from decimal import Decimal

import pypdfium2 as pdfium
import pytest

from ai.agents.extraction.normalize import normalize_invoice
from ai.agents.extraction.schema import PostalAddress, SellerParty
from ai.canonical import sha256_hex
from ai.synthetic.generator import DEFECTS, LAYOUTS, generate
from ai.synthetic.render_image import pdf_page_count, pdf_to_jpeg
from ai.synthetic.render_pdf import render_letter, render_pdf


def _text(pdf: bytes) -> str:
    doc = pdfium.PdfDocument(pdf)
    try:
        return "\n".join(doc[i].get_textpage().get_text_range() for i in range(len(doc)))
    finally:
        doc.close()


def test_generation_is_deterministic_and_normalised():
    a, b = generate(7, "ar"), generate(7, "ar")
    assert a == b and a != generate(8, "ar")
    for seed in range(40):
        for lang in ("ar", "en"):
            inv = generate(seed, lang)
            assert normalize_invoice(inv.truth) == inv.truth  # truth is already in stored form
            assert len(inv.truth.seller_trn) in (14, 15) and inv.layout in LAYOUTS
            assert set(inv.defects) <= set(DEFECTS)


def test_defect_free_invoices_add_up():
    checked = 0
    for seed in range(60):
        inv = generate(seed, "en")
        if inv.defects:
            continue
        t = inv.truth
        nets = sum(Decimal(ln.net_amount) for ln in t.lines)
        vat = sum(Decimal(tb.tax_amount) for tb in t.tax_breakdown)
        assert Decimal(t.totals.line_extension_amount) == nets == Decimal(t.totals.tax_exclusive_amount)
        assert Decimal(t.vat_amount) == vat and Decimal(t.total_amount) == nets + vat
        checked += 1
    assert checked > 30


def test_defect_rate_and_forced_layout():
    with_defects = sum(bool(generate(s, "ar").defects) for s in range(200))
    assert 25 <= with_defects <= 75
    assert generate(3, "en", "thermal").truth.payment_due_date == ""


@pytest.mark.parametrize("layout", LAYOUTS)
def test_pdf_is_deterministic_and_readable(layout):
    inv = generate(11, "en", layout)
    pdf = render_pdf(inv)
    assert pdf == render_pdf(inv) and pdf_page_count(pdf) == 1
    text = _text(pdf)
    assert inv.truth.invoice_number in text and inv.truth.seller_trn in text


def test_arabic_pdf_shapes_text_and_thermal_uses_arabic_indic_digits():
    inv = generate(5, "ar", "classic")
    seller = SellerParty(name="شركة الواحة للتجارة", postal_address=PostalAddress(country_subdivision="DXB"))
    inv = dataclasses.replace(inv, truth=inv.truth.model_copy(update={"seller": seller}))
    text = _text(render_pdf(inv))
    assert "الواحة" in text and inv.truth.seller_trn in text  # finding F10
    thermal = generate(5, "ar", "thermal")
    ttext = _text(render_pdf(thermal))
    assert thermal.truth.seller_trn not in ttext and any("٠" <= ch <= "٩" for ch in ttext)


def test_image_is_deterministic_jpeg_at_150_dpi():
    pdf = render_pdf(generate(2, "en", "classic"))
    a, b = pdf_to_jpeg(pdf, 2), pdf_to_jpeg(pdf, 2)
    assert a == b and a[:3] == b"\xff\xd8\xff" and sha256_hex(a) != sha256_hex(pdf_to_jpeg(pdf, 3))


def test_letter_is_one_page_and_deterministic():
    assert render_letter(1, "ar") == render_letter(1, "ar") and pdf_page_count(render_letter(1, "en")) == 1
