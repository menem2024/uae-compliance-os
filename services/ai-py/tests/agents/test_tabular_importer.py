"""XLSX/CSV importer (spec section 5.5, AC-6): template matching, values as printed, guards."""

import datetime as dt
import io
import zipfile

import pytest

from ai.agents.extraction.tabular import importer
from ai.agents.extraction.tabular.importer import TabularRejected, cell_text, import_tabular
from ai.agents.extraction.tabular.template import COLUMNS, SYNONYMS, header_key, match_header, row_type

CSV_EN = (
    "Invoice No.,Date,Currency,Seller,Seller TRN,Buyer,Item,Qty,Unit price,Line amount,Tax category,Tax rate,"
    "Row type,Taxable amount,Tax amount,Total\r\n"
    'INV-1,15/03/2026,aed,Oasis Trading LLC,100 123 456 789 003,Harbour Cafe,Laptop stand,2,"1,250.00",'
    '"2,500.00",s,5,,,,"2,625.00"\r\n'
    "INV-1,,,,,,,,,,S,5,tax,2500.00,125.00,\r\n"
    ",,,,,,,,,,,,,,,\r\n"
    "INV-2,2026-03-16,AED,Oasis Trading LLC,,,Delivery charge,١,٢٥٫٠٠,٢٥٫٠٠,E,,,,,25\r\n"
)


def test_every_column_has_both_languages_and_no_synonym_is_ambiguous():
    assert set(SYNONYMS) == set(COLUMNS) | {"tax_code", "tax_rate"}
    seen: dict[str, str] = {}
    for col, langs in SYNONYMS.items():
        assert langs["en"] and langs["ar"]
        for s in (col, *langs["en"], *langs["ar"]):
            k = header_key(s)
            assert seen.setdefault(k, col) == col, f"{s!r} maps to {seen[k]} and {col}"


def test_header_matching_and_row_types():
    found, missing = match_header(["Invoice No.", "التاريخ", "x", "العملة", "المورد", "البند", "صافي البند"])
    assert found == {0: "invoice_number", 1: "issue_date", 3: "currency", 4: "seller_name", 5: "item_name",
                     6: "line_net_amount"}
    assert missing == ()
    assert match_header(["Invoice number", "Item"])[1] == ("issue_date", "currency", "seller_name",
                                                           "line_net_amount")
    assert (row_type(""), row_type("TAX"), row_type("ضريبة"), row_type("بند")) == ("line", "tax", "tax", "line")


def test_csv_import_groups_rows_and_normalises_values():
    res = import_tabular(CSV_EN.encode("utf-8"), "csv")
    assert res.method == "csv" and res.missing_columns == ()
    a, b = res.invoices
    assert (a.source_ordinal, a.source_ref, b.source_ref) == (0, "rows 2-3", "rows 5-5")
    inv = a.invoice
    assert (inv.invoice_number, inv.issue_date, inv.currency, inv.seller_trn) == (
        "INV-1", "2026-03-15", "AED", "100123456789003")
    assert inv.total_amount == "2625.00" and inv.vat_amount == ""  # absent stays empty: nothing is computed
    assert [(ln.quantity, ln.price.net_price, ln.net_amount, ln.tax.code) for ln in inv.lines] == [
        ("2", "1250.00", "2500.00", "S")]
    assert [(t.taxable_amount, t.tax_amount, t.category.code, t.category.rate) for t in inv.tax_breakdown] == [
        ("2500.00", "125.00", "S", "5")]
    assert [(ln.quantity, ln.net_amount, ln.tax.code) for ln in b.invoice.lines] == [("1", "25.00", "E")]


def test_missing_required_columns_import_nothing():
    res = import_tabular(b"Invoice number,Item\r\nINV-1,Rice\r\n", "csv")
    assert res.invoices == () and "issue_date" in res.missing_columns


def test_cp1256_csv_is_decoded():
    text = "رقم الفاتورة,التاريخ,العملة,البائع,البند,مبلغ البند\r\nف-1,2026-01-02,AED,شركة الواحة,أرز,10.00\r\n"
    res = import_tabular(text.encode("cp1256"), "csv")
    assert res.invoices[0].invoice.seller.name == "شركة الواحة"
    assert res.invoices[0].invoice.lines[0].item.name == "أرز"


@pytest.mark.parametrize(("value", "fmt", "want"), [
    (1234.5, "#,##0.00", "1234.50"),
    (0.1 + 0.2, "#,##0.00", "0.30"),
    (7.0, "General", "7"),
    (7.5, "General", "7.5"),
    (0, "#,##0.00", "0.00"),
    (12, "General", "12"),
    (dt.datetime(2026, 3, 15, 10, 0, tzinfo=dt.UTC), "yyyy-mm-dd", "2026-03-15"),
    (dt.date(2026, 3, 15), "General", "2026-03-15"),
    (None, "General", ""),
    ("100123456789003", "@", "100123456789003"),
])
def test_cell_text_prints_values_as_excel_shows_them(value, fmt, want):
    assert cell_text(value, fmt) == want


@pytest.mark.parametrize("fmt", ["#,##0.00", "General", "0"])
def test_cell_text_of_a_value_too_large_to_quantize_is_exact_not_a_crash(fmt):
    """Review B #3: 1e30 with '#,##0.00' raised InvalidOperation and failed the whole sheet."""
    assert cell_text(1e30, fmt) == "1" + "0" * 30
    assert cell_text(10**30, fmt) == "1" + "0" * 30


def _zip(entries: int, size: int) -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        for i in range(entries):
            zf.writestr(f"x{i}.xml", b"\0" * size)
    return buf.getvalue()


def test_zip_bomb_and_garbage_are_rejected(monkeypatch):
    with pytest.raises(TabularRejected) as e:
        import_tabular(_zip(1001, 1), "xlsx")
    assert e.value.code == "zip_bomb"
    monkeypatch.setattr(importer, "MAX_UNCOMPRESSED_BYTES", 1000)
    with pytest.raises(TabularRejected) as e:
        import_tabular(_zip(1, 2000), "xlsx")
    assert e.value.code == "zip_bomb"
    with pytest.raises(TabularRejected) as e:
        import_tabular(b"not a zip at all", "xlsx")
    assert e.value.code == "not_a_workbook"
    with pytest.raises(TabularRejected) as e:
        import_tabular(b"", "csv")
    assert e.value.code == "empty"


def test_row_and_invoice_limits(monkeypatch):
    monkeypatch.setattr(importer, "MAX_ROWS", 1)
    with pytest.raises(TabularRejected) as e:
        import_tabular(CSV_EN.encode(), "csv")
    assert e.value.code == "too_many_rows"
    monkeypatch.setattr(importer, "MAX_ROWS", 50_000)
    monkeypatch.setattr(importer, "MAX_INVOICES", 1)
    with pytest.raises(TabularRejected) as e:
        import_tabular(CSV_EN.encode(), "csv")
    assert e.value.code == "too_many_invoices"
