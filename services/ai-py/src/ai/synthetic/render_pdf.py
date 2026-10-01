"""PDF rendering of SyntheticInvoices with fpdf2 + uharfbuzz text shaping (spec section 5.5, finding F10).

Deterministic: fixed creation date and producer, fonts vendored in ai/synthetic/fonts (IBM Plex Sans
Arabic, OFL; it includes the Latin set). Dev/CI only: fpdf2 is a dev dependency.
"""

import datetime
import random
from decimal import Decimal
from pathlib import Path

from fpdf import FPDF

from ai.synthetic.generator import SyntheticInvoice

FONTS = Path(__file__).parent / "fonts"
_AR_DIGITS = str.maketrans("0123456789.,", "٠١٢٣٤٥٦٧٨٩٫٬")
_EN_MONTHS = ("January", "February", "March", "April", "May", "June", "July", "August", "September", "October",
              "November", "December")
_AR_MONTHS = ("يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر",
              "ديسمبر")
LABELS: dict[str, dict[str, str]] = {
    "en": {"invoice": "Tax Invoice", "credit": "Tax Credit Note", "number": "Invoice No", "date": "Date",
           "due": "Due date", "seller": "Seller", "buyer": "Bill to", "trn": "TRN", "item": "Description",
           "qty": "Qty", "unit": "Unit", "price": "Unit price", "net": "Net", "cat": "VAT", "subtotal": "Subtotal",
           "taxable": "Taxable amount", "vat": "VAT amount", "total": "Total incl. VAT", "payable": "Amount due",
           "currency": "Currency", "summary": "VAT summary", "note": "Note"},
    "ar": {"invoice": "فاتورة ضريبية", "credit": "إشعار دائن ضريبي", "number": "رقم الفاتورة", "date": "التاريخ",
           "due": "تاريخ الاستحقاق", "seller": "المورد", "buyer": "العميل", "trn": "الرقم الضريبي",
           "item": "البيان", "qty": "الكمية", "unit": "الوحدة", "price": "سعر الوحدة", "net": "الصافي",
           "cat": "الضريبة", "subtotal": "المجموع الفرعي", "taxable": "المبلغ الخاضع للضريبة",
           "vat": "مبلغ الضريبة", "total": "الإجمالي شامل الضريبة", "payable": "المبلغ المستحق",
           "currency": "العملة", "summary": "ملخص الضريبة", "note": "ملاحظة"},
}
FIXED_DATE = datetime.datetime(2026, 1, 1, tzinfo=datetime.UTC)


def _new_pdf(fmt: str | tuple[float, float]) -> FPDF:
    pdf = FPDF(format=fmt, unit="mm")
    pdf.set_creation_date(FIXED_DATE)
    pdf.set_producer("compliance-synthetic")
    pdf.add_font("plex", "", str(FONTS / "IBMPlexSansArabic-Regular.ttf"))
    pdf.add_font("plex", "B", str(FONTS / "IBMPlexSansArabic-Bold.ttf"))
    pdf.set_text_shaping(True)
    pdf.set_auto_page_break(auto=True, margin=10)
    pdf.add_page()
    return pdf


def fmt_amount(value: str, *, grouped: bool, arabic_digits: bool) -> str:
    if not value:
        return ""
    text = f"{Decimal(value):,}" if grouped else value
    return text.translate(_AR_DIGITS) if arabic_digits else text


def fmt_date(iso: str, style: str, lang: str, *, arabic_digits: bool) -> str:
    if not iso:
        return ""
    y, m, d = (int(p) for p in iso.split("-"))
    if style == "dmy":
        text = f"{d:02d}/{m:02d}/{y}"
    elif style == "long":
        text = f"{d} {(_AR_MONTHS if lang == 'ar' else _EN_MONTHS)[m - 1]} {y}"
    else:
        text = iso
    return text.translate(_AR_DIGITS) if arabic_digits else text


def render_pdf(inv: SyntheticInvoice) -> bytes:
    t, lang, layout = inv.truth, inv.lang, inv.layout
    lab = LABELS[lang]
    rtl = lang == "ar"
    align = "R" if rtl else "L"
    thermal = layout == "thermal"
    ar_digits = rtl and thermal
    grouped = layout == "classic"
    date_style = {"classic": "dmy", "modern": "long", "thermal": "iso"}[layout]

    def amt(v: str) -> str:
        return fmt_amount(v, grouped=grouped, arabic_digits=ar_digits)

    def dt(v: str) -> str:
        return fmt_date(v, date_style, lang, arabic_digits=ar_digits)

    def trn(v: str) -> str:
        return v.translate(_AR_DIGITS) if ar_digits else v

    pdf = _new_pdf((80, 230) if thermal else "A4")
    width = pdf.epw
    big, body, small = (11, 8, 7) if thermal else (18, 10, 9)

    def row(text: str, *, bold: bool = False, size: int = body, h: float = 6 if not thermal else 4.5) -> None:
        pdf.set_font("plex", "B" if bold else "", size)
        pdf.multi_cell(width, h, text, align="C" if thermal and bold else align, new_x="LMARGIN", new_y="NEXT")

    row(lab["credit"] if t.invoice_type_code == "381" else lab["invoice"], bold=True, size=big, h=10 if not thermal else 6)
    row(t.seller.name, bold=True)
    row(f"{inv.seller_emirate_name}, {'الإمارات العربية المتحدة' if rtl else 'United Arab Emirates'}")
    row(f"{lab['trn']}: {trn(t.seller_trn)}")
    row(f"{lab['number']}: {t.invoice_number}")
    row(f"{lab['date']}: {dt(t.issue_date)}")
    if t.payment_due_date:
        row(f"{lab['due']}: {dt(t.payment_due_date)}")
    row(f"{lab['currency']}: {t.currency}")
    pdf.ln(2)
    row(f"{lab['buyer']}: {t.buyer.name}", bold=layout == "modern")
    if t.buyer_trn:
        row(f"{lab['trn']}: {trn(t.buyer_trn)}")
    pdf.ln(3)

    cols = ("item", "qty", "unit", "price", "net", "cat")
    widths = (0.34, 0.1, 0.1, 0.16, 0.16, 0.14) if not thermal else (0.34, 0.12, 0.12, 0.14, 0.16, 0.12)
    order = list(range(len(cols)))[::-1] if rtl else list(range(len(cols)))
    pdf.set_font("plex", "B", small)
    for i in order:
        pdf.cell(width * widths[i], 6 if not thermal else 4.5, lab[cols[i]], border="B" if layout != "modern" else 0,
                 align="C")
    pdf.ln()
    pdf.set_font("plex", "", small)
    for ln in t.lines:
        cells = (ln.item.name, fmt_amount(ln.quantity, grouped=False, arabic_digits=ar_digits), ln.unit_code,
                 amt(ln.price.net_price), amt(ln.net_amount),
                 f"{ln.tax.code} {fmt_amount(ln.tax.rate, grouped=False, arabic_digits=ar_digits)}%".strip()
                 if ln.tax.rate else ln.tax.code)
        for i in order:
            pdf.cell(width * widths[i], 6 if not thermal else 4.5, cells[i], align="C" if i else align)
        pdf.ln()
    pdf.ln(3)

    row(lab["summary"], bold=True, size=small)
    for tb in t.tax_breakdown:
        rate = f" {fmt_amount(tb.category.rate, grouped=False, arabic_digits=ar_digits)}%" if tb.category.rate else ""
        row(f"{tb.category.code}{rate}: {lab['taxable']} {amt(tb.taxable_amount)} | {lab['vat']} {amt(tb.tax_amount)}",
            size=small)
    pdf.ln(2)
    row(f"{lab['subtotal']}: {amt(t.totals.line_extension_amount)}")
    row(f"{lab['vat']}: {amt(t.vat_amount)}")
    row(f"{lab['total']}: {amt(t.total_amount)} {t.currency}", bold=True)
    row(f"{lab['payable']}: {amt(t.totals.payable_amount)} {t.currency}")
    if t.note:
        pdf.ln(2)
        row(f"{lab['note']}: {t.note}", size=small)
    return bytes(pdf.output())


LETTER: dict[str, tuple[str, ...]] = {
    "en": ("Service Agreement",
           ("This agreement is made between the parties named below for the provision of office cleaning "
            "services for a period of twelve months."),
           "Signed on behalf of both parties."),
    "ar": ("اتفاقية خدمات",
           "أبرمت هذه الاتفاقية بين الطرفين المذكورين أدناه لتقديم خدمات تنظيف المكاتب لمدة اثني عشر شهراً.",
           "وقع نيابة عن الطرفين."),
}


def render_letter(seed: int, lang: str) -> bytes:
    """A one-page non-invoice document (the intake suite's `contract`/`other` cases)."""
    rng = random.Random(f"synthetic-letter:{seed}:{lang}")
    title, body, sign = LETTER[lang]
    pdf = _new_pdf("A4")
    align = "R" if lang == "ar" else "L"
    pdf.set_font("plex", "B", 16)
    pdf.multi_cell(pdf.epw, 10, title, align=align, new_x="LMARGIN", new_y="NEXT")
    pdf.set_font("plex", "", 11)
    for _ in range(rng.randint(2, 4)):
        pdf.multi_cell(pdf.epw, 7, body, align=align, new_x="LMARGIN", new_y="NEXT")
        pdf.ln(2)
    pdf.multi_cell(pdf.epw, 7, sign, align=align, new_x="LMARGIN", new_y="NEXT")
    return bytes(pdf.output())
