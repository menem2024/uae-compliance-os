"""Writes invoices in the import template as XLSX or CSV with reproducible bytes (spec section 5.5).

XLSX: amounts are float cells formatted `#,##0.00`, quantities and rates are numbers in `General`, dates are
date cells, TRNs and codes are text. openpyxl stamps the current time into docProps/core.xml and every zip
entry, so the archive is re-packed with pinned timestamps and a fixed entry order.
"""

from __future__ import annotations

import csv
import datetime as dt
import io
import re
import zipfile
from collections.abc import Sequence
from decimal import Decimal
from typing import Literal

from openpyxl import Workbook

from ai.agents.extraction.schema import ExtractedInvoice
from ai.agents.extraction.tabular.template import COLUMNS, SYNONYMS

ZIP_TIME = (2026, 1, 1, 0, 0, 0)
_W3C = "2026-01-01T00:00:00Z"
_CORE_TIMES = re.compile(rb"(<dcterms:(created|modified)[^>]*>)[^<]*(</dcterms:\2>)")
_AMOUNT_COLUMNS = frozenset({"line_extension_amount", "tax_exclusive_amount", "vat_amount", "total_amount",
                             "payable_amount", "net_price", "line_net_amount", "taxable_amount", "tax_amount"})
_NUMBER_COLUMNS = frozenset({"quantity", "tax_rate"})
_DATE_COLUMNS = frozenset({"issue_date", "payment_due_date"})
_ROW_TYPE = {"en": {"line": "line", "tax": "tax"}, "ar": {"line": "بند", "tax": "ضريبة"}}


def headers(lang: Literal["ar", "en"]) -> list[str]:
    return [SYNONYMS[c][lang][0] for c in COLUMNS]


def rows(invoices: Sequence[ExtractedInvoice], lang: Literal["ar", "en"]) -> list[dict[str, str]]:
    """Template rows as text: one per line, then one `tax` row per tax subtotal, header values on every row."""
    out: list[dict[str, str]] = []
    for inv in invoices:
        head = {
            "invoice_number": inv.invoice_number, "issue_date": inv.issue_date,
            "invoice_type_code": inv.invoice_type_code, "currency": inv.currency, "seller_name": inv.seller.name,
            "seller_trn": inv.seller_trn, "seller_emirate": inv.seller.postal_address.country_subdivision,
            "buyer_name": inv.buyer.name, "buyer_trn": inv.buyer_trn, "payment_due_date": inv.payment_due_date,
            "line_extension_amount": inv.totals.line_extension_amount,
            "tax_exclusive_amount": inv.totals.tax_exclusive_amount, "vat_amount": inv.vat_amount,
            "total_amount": inv.total_amount, "payable_amount": inv.totals.payable_amount, "note": inv.note,
        }
        for ln in inv.lines:
            out.append({**head, "row_type": _ROW_TYPE[lang]["line"], "item_name": ln.item.name,
                        "quantity": ln.quantity, "unit_code": ln.unit_code, "net_price": ln.price.net_price,
                        "line_net_amount": ln.net_amount, "tax_code": ln.tax.code, "tax_rate": ln.tax.rate})
        for t in inv.tax_breakdown:
            out.append({**head, "row_type": _ROW_TYPE[lang]["tax"], "tax_code": t.category.code,
                        "tax_rate": t.category.rate, "taxable_amount": t.taxable_amount, "tax_amount": t.tax_amount})
    return out


def _cell(col: str, text: str) -> tuple[object, str]:
    if text == "":
        return None, "General"
    if col in _AMOUNT_COLUMNS:
        return float(Decimal(text)), "#,##0.00"
    if col in _NUMBER_COLUMNS:
        d = Decimal(text)
        return (int(d) if d == d.to_integral_value() else float(d)), "General"
    if col in _DATE_COLUMNS:
        return dt.date.fromisoformat(text), "yyyy-mm-dd"
    return text, "@"


def _repack(data: bytes) -> bytes:
    src = zipfile.ZipFile(io.BytesIO(data))
    out = io.BytesIO()
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as dst:
        for name in sorted(src.namelist()):
            body = src.read(name)
            if name == "docProps/core.xml":
                body = _CORE_TIMES.sub(rb"\g<1>" + _W3C.encode() + rb"\g<3>", body)
            info = zipfile.ZipInfo(name, date_time=ZIP_TIME)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o600 << 16
            dst.writestr(info, body, compresslevel=6)
    return out.getvalue()


def write_xlsx(invoices: Sequence[ExtractedInvoice], lang: Literal["ar", "en"]) -> bytes:
    wb = Workbook()
    ws = wb.active
    assert ws is not None
    ws.title = "Invoices" if lang == "en" else "الفواتير"
    ws.sheet_view.rightToLeft = lang == "ar"
    ws.append(headers(lang))
    for r, row in enumerate(rows(invoices, lang), start=2):
        for c, col in enumerate(COLUMNS, start=1):
            value, fmt = _cell(col, row.get(col, ""))
            cell = ws.cell(row=r, column=c, value=value)  # type: ignore[arg-type]
            cell.number_format = fmt
    fixed = dt.datetime(2026, 1, 1, tzinfo=dt.UTC).replace(tzinfo=None)
    wb.properties.creator = "compliance-os synthetic"
    wb.properties.created = fixed
    wb.properties.modified = fixed
    buf = io.BytesIO()
    wb.save(buf)
    return _repack(buf.getvalue())


def write_csv(invoices: Sequence[ExtractedInvoice], lang: Literal["ar", "en"], *,
              encoding: Literal["utf-8-sig", "cp1256"] = "utf-8-sig") -> bytes:
    """Amounts are printed with thousands separators (`1,234.50`), as Excel exports them."""
    buf = io.StringIO()
    w = csv.writer(buf, lineterminator="\r\n")
    w.writerow(headers(lang))
    for row in rows(invoices, lang):
        w.writerow([f"{Decimal(v):,}" if c in _AMOUNT_COLUMNS and v else v
                    for c, v in ((c, row.get(c, "")) for c in COLUMNS)])
    return buf.getvalue().encode(encoding)
