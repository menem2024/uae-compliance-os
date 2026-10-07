"""The XLSX/CSV import template (spec section 5.5): one row per invoice line, rows grouped by invoice number.

A row whose `row_type` is `tax` (Arabic `ضريبة`) is a tax-breakdown row instead of a line. Headers are matched
after `fold_text` (NFKC, casefold, whitespace collapse, Arabic letter-variant folding) and with `_`, `-`, `#`
and `.` treated as spaces, so `Invoice No.`, `invoice_no` and `INVOICE  NO` all match.
"""

import re

from ai.agents.extraction.normalize import fold_text

# canonical column -> ExtractedInvoice path (header columns) or line/tax field (row columns)
HEADER_COLUMNS: dict[str, str] = {
    "invoice_number": "invoice_number",
    "issue_date": "issue_date",
    "invoice_type_code": "invoice_type_code",
    "currency": "currency",
    "seller_name": "seller.name",
    "seller_trn": "seller_trn",
    "seller_emirate": "seller.postal_address.country_subdivision",
    "buyer_name": "buyer.name",
    "buyer_trn": "buyer_trn",
    "payment_due_date": "payment_due_date",
    "line_extension_amount": "totals.line_extension_amount",
    "tax_exclusive_amount": "totals.tax_exclusive_amount",
    "vat_amount": "vat_amount",
    "total_amount": "total_amount",
    "payable_amount": "totals.payable_amount",
    "note": "note",
}
LINE_COLUMNS: dict[str, str] = {
    "item_name": "item.name",
    "quantity": "quantity",
    "unit_code": "unit_code",
    "net_price": "price.net_price",
    "line_net_amount": "net_amount",
    "tax_code": "tax.code",
    "tax_rate": "tax.rate",
}
TAX_COLUMNS: dict[str, str] = {
    "taxable_amount": "taxable_amount",
    "tax_amount": "tax_amount",
    "tax_code": "category.code",
    "tax_rate": "category.rate",
}
COLUMNS: tuple[str, ...] = (*HEADER_COLUMNS, "row_type", *LINE_COLUMNS, "taxable_amount", "tax_amount")
REQUIRED: tuple[str, ...] = ("invoice_number", "issue_date", "currency", "seller_name", "item_name",
                             "line_net_amount")

# The first synonym of each language is what the writer prints.
SYNONYMS: dict[str, dict[str, tuple[str, ...]]] = {
    "invoice_number": {"en": ("Invoice number", "invoice no", "invoice #", "inv no"),
                       "ar": ("رقم الفاتورة",)},
    "issue_date": {"en": ("Issue date", "invoice date", "date"), "ar": ("تاريخ الفاتورة", "تاريخ الإصدار", "التاريخ")},
    "invoice_type_code": {"en": ("Invoice type code", "type code"), "ar": ("رمز نوع الفاتورة",)},
    "currency": {"en": ("Currency",), "ar": ("العملة",)},
    "seller_name": {"en": ("Seller name", "seller", "supplier"), "ar": ("اسم البائع", "البائع", "المورد")},
    "seller_trn": {"en": ("Seller TRN", "supplier trn"), "ar": ("الرقم الضريبي للبائع",)},
    "seller_emirate": {"en": ("Seller emirate", "emirate"), "ar": ("إمارة البائع", "الإمارة")},
    "buyer_name": {"en": ("Buyer name", "buyer", "customer"), "ar": ("اسم المشتري", "المشتري", "العميل")},
    "buyer_trn": {"en": ("Buyer TRN", "customer trn"), "ar": ("الرقم الضريبي للمشتري",)},
    "payment_due_date": {"en": ("Payment due date", "due date"), "ar": ("تاريخ الاستحقاق",)},
    "line_extension_amount": {"en": ("Line extension amount", "lines total"), "ar": ("مجموع البنود",)},
    "tax_exclusive_amount": {"en": ("Tax exclusive amount", "subtotal", "total excluding vat"),
                             "ar": ("المجموع قبل الضريبة",)},
    "vat_amount": {"en": ("VAT amount", "total vat"), "ar": ("إجمالي الضريبة", "ضريبة القيمة المضافة")},
    "total_amount": {"en": ("Total amount", "total", "total including vat"), "ar": ("الإجمالي", "المبلغ الإجمالي")},
    "payable_amount": {"en": ("Payable amount", "amount due"), "ar": ("المبلغ المستحق",)},
    "note": {"en": ("Note", "notes"), "ar": ("ملاحظات", "ملاحظة")},
    "row_type": {"en": ("Row type",), "ar": ("نوع السطر",)},
    "item_name": {"en": ("Item", "item name", "description"), "ar": ("البند", "اسم البند", "الوصف")},
    "quantity": {"en": ("Quantity", "qty"), "ar": ("الكمية",)},
    "unit_code": {"en": ("Unit code", "unit"), "ar": ("رمز الوحدة", "الوحدة")},
    "net_price": {"en": ("Unit price", "net price", "price"), "ar": ("سعر الوحدة",)},
    "line_net_amount": {"en": ("Line amount", "line net amount", "net amount"), "ar": ("مبلغ البند", "صافي البند")},
    "tax_code": {"en": ("Tax category", "tax code", "vat category"), "ar": ("فئة الضريبة", "رمز الضريبة")},
    "tax_rate": {"en": ("Tax rate", "vat rate"), "ar": ("نسبة الضريبة",)},
    "taxable_amount": {"en": ("Taxable amount",), "ar": ("المبلغ الخاضع للضريبة",)},
    "tax_amount": {"en": ("Tax amount", "category tax amount"), "ar": ("مبلغ الضريبة",)},
}
ROW_TYPES: dict[str, str] = {"line": "line", "tax": "tax", "بند": "line", "ضريبة": "tax"}

_SEP = re.compile(r"[_\-#.]+")


def header_key(cell: str) -> str:
    return fold_text(_SEP.sub(" ", cell))


_LOOKUP: dict[str, str] = {header_key(s): col for col, langs in SYNONYMS.items()
                           for names in langs.values() for s in (col, *names)}


def match_header(cells: list[str]) -> tuple[dict[int, str], tuple[str, ...]]:
    """(column index -> canonical column, missing required columns). The first match of a column wins."""
    found: dict[int, str] = {}
    seen: set[str] = set()
    for i, cell in enumerate(cells):
        col = _LOOKUP.get(header_key(cell))
        if col and col not in seen:
            found[i] = col
            seen.add(col)
    return found, tuple(c for c in REQUIRED if c not in seen)


_ROW_TYPE_KEYS: dict[str, str] = {fold_text(k): v for k, v in ROW_TYPES.items()}


def row_type(value: str) -> str:
    """'' and unknown values are lines; `tax`/`ضريبة` rows are tax-breakdown rows."""
    return _ROW_TYPE_KEYS.get(fold_text(value), "line")
