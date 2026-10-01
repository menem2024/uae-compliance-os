"""Seeded, deterministic SyntheticInvoice generator (spec section 5.5).

The ground truth is an ExtractedInvoice holding exactly what the document prints (normalised: Western
digits, ISO dates, codes). Injected defects change what is printed, so a defective invoice still has an
exact extraction target; they exist so the Verifier's critic-confirmation rule is exercised.
"""

import random
from dataclasses import dataclass
from datetime import date, timedelta
from decimal import ROUND_HALF_UP, Decimal
from typing import Literal

from ai.agents.extraction.schema import (
    BuyerParty,
    ExtractedInvoice,
    InvoiceLine,
    Item,
    PostalAddress,
    Price,
    SellerParty,
    TaxCategory,
    TaxSubtotal,
    Totals,
)

type Lang = Literal["ar", "en"]
type Layout = Literal["classic", "modern", "thermal"]

LAYOUTS: tuple[Layout, ...] = ("classic", "modern", "thermal")
DEFECTS: tuple[str, ...] = (
    "total_off_by_cent", "missing_buyer_trn", "seller_trn_14_digits", "missing_due_date", "line_net_off_by_cent",
)
EMIRATES: dict[str, tuple[str, str]] = {
    "AUH": ("Abu Dhabi", "أبوظبي"), "DXB": ("Dubai", "دبي"), "SHJ": ("Sharjah", "الشارقة"),
    "UAQ": ("Umm Al Quwain", "أم القيوين"), "FUJ": ("Fujairah", "الفجيرة"), "AJM": ("Ajman", "عجمان"),
    "RAK": ("Ras Al Khaimah", "رأس الخيمة"),
}
SELLERS: dict[Lang, tuple[str, ...]] = {
    "en": ("Oasis Trading LLC", "Gulf Star Logistics FZE", "Desert Rose Catering LLC", "Blue Dhow Marine Services",
           "Palm Crest Electronics LLC", "Falcon Office Supplies", "Emirates Fresh Produce Co.", "Al Noor IT Solutions"),
    "ar": ("شركة الواحة للتجارة ذ.م.م", "مؤسسة نجمة الخليج للخدمات اللوجستية", "مطعم وردة الصحراء",
           "شركة المرسى للخدمات البحرية", "مؤسسة النخلة للإلكترونيات", "شركة الصقر للقرطاسية",
           "شركة الإمارات للخضار والفواكه", "مؤسسة النور لتقنية المعلومات"),
}
BUYERS: dict[Lang, tuple[str, ...]] = {
    "en": ("Sunrise Hotels LLC", "Creek View Clinic", "Marina Builders LLC", "Al Safa School", "Harbour Cafe"),
    "ar": ("فنادق الشروق ذ.م.م", "عيادة إطلالة الخور", "شركة المارينا للمقاولات", "مدرسة الصفا", "مقهى الميناء"),
}
ITEMS: tuple[tuple[str, str, str], ...] = (  # (en, ar, unit code)
    ("A4 copy paper box", "صندوق ورق طباعة A4", "H87"), ("Laptop stand", "حامل حاسوب محمول", "H87"),
    ("Consulting services", "خدمات استشارية", "HUR"), ("Basmati rice", "أرز بسمتي", "KGM"),
    ("Fresh dates", "تمر طازج", "KGM"), ("Network cable 5m", "كابل شبكة 5 متر", "H87"),
    ("Office cleaning", "تنظيف المكاتب", "HUR"), ("Printer toner", "حبر طابعة", "H87"),
    ("Bottled water carton", "كرتونة مياه معبأة", "C62"), ("Delivery charge", "رسوم التوصيل", "C62"),
)
NOTES: dict[Lang, tuple[str, ...]] = {
    "en": ("", "", "Thank you for your business", "Payment by bank transfer"),
    "ar": ("", "", "شكراً لتعاملكم معنا", "الدفع بالتحويل البنكي"),
}
CENT = Decimal("0.01")


@dataclass(frozen=True, slots=True)
class SyntheticInvoice:
    case_id: str
    seed: int
    lang: Lang
    layout: Layout
    truth: ExtractedInvoice
    defects: tuple[str, ...]
    seller_emirate_name: str  # what the address line prints; the truth holds the code


def _money(d: Decimal) -> str:
    return str(d.quantize(CENT, rounding=ROUND_HALF_UP))


def _trn(rng: random.Random, digits: int = 15) -> str:
    return "100" + "".join(str(rng.randrange(10)) for _ in range(digits - 3))


def generate(seed: int, lang: Lang, layout: Layout | None = None, *, defect_rate: float = 0.25) -> SyntheticInvoice:
    rng = random.Random(f"synthetic-invoice:{seed}:{lang}")
    layout = layout or LAYOUTS[rng.randrange(len(LAYOUTS))]
    issue = date(2026, 1, 1) + timedelta(days=rng.randrange(270))
    due = "" if layout == "thermal" else (issue + timedelta(days=rng.choice((0, 15, 30, 45)))).isoformat()
    code = rng.choice(tuple(EMIRATES))
    lines: list[InvoiceLine] = []
    groups: dict[tuple[str, str], Decimal] = {}
    for _ in range(rng.randint(1, 3 if layout == "thermal" else 8)):
        en, ar, unit = ITEMS[rng.randrange(len(ITEMS))]
        qty = Decimal(rng.randint(1, 20)) if unit in {"H87", "C62"} else Decimal(rng.randint(5, 80)) / 2
        price = Decimal(rng.randint(500, 250_000)) / 100
        net = (qty * price).quantize(CENT, rounding=ROUND_HALF_UP)
        roll = rng.random()
        cat = TaxCategory(code="S", rate="5") if roll < 0.85 else (
            TaxCategory(code="Z", rate="0") if roll < 0.95 else TaxCategory(code="E", rate=""))
        lines.append(InvoiceLine(item=Item(name=en if lang == "en" else ar), quantity=str(qty), unit_code=unit,
                                 price=Price(net_price=_money(price)), net_amount=_money(net), tax=cat))
        groups[(cat.code, cat.rate)] = groups.get((cat.code, cat.rate), Decimal(0)) + net
    breakdown = [TaxSubtotal(taxable_amount=_money(base), tax_amount=_money(base * Decimal(rate or 0) / 100),
                             category=TaxCategory(code=c, rate=rate)) for (c, rate), base in groups.items()]
    subtotal = sum((Decimal(ln.net_amount) for ln in lines), Decimal(0))
    vat = sum((Decimal(t.tax_amount) for t in breakdown), Decimal(0))
    total = subtotal + vat
    prefix = {"classic": "INV-2026-", "modern": "TI/2026/", "thermal": "R"}[layout]
    truth = ExtractedInvoice(
        invoice_number=f"{prefix}{rng.randint(1, 99_999):05d}", issue_date=issue.isoformat(),
        invoice_type_code="381" if rng.random() < 0.05 else "380",
        currency="USD" if lang == "en" and rng.random() < 0.1 else "AED",
        seller=SellerParty(name=rng.choice(SELLERS[lang]), postal_address=PostalAddress(country_subdivision=code)),
        seller_trn=_trn(rng), buyer=BuyerParty(name=rng.choice(BUYERS[lang])), buyer_trn=_trn(rng),
        payment_due_date=due,
        totals=Totals(line_extension_amount=_money(subtotal), tax_exclusive_amount=_money(subtotal),
                      payable_amount=_money(total)),
        vat_amount=_money(vat), total_amount=_money(total), note=rng.choice(NOTES[lang]),
        tax_breakdown=breakdown, lines=lines)
    defects: tuple[str, ...] = ()
    if rng.random() < defect_rate:
        defect = DEFECTS[rng.randrange(len(DEFECTS))]
        if defect == "missing_due_date" and not truth.payment_due_date:
            defect = "missing_buyer_trn"
        defects = (defect,)
        truth = _apply_defect(truth, defect, rng)
    name = EMIRATES[code][0 if lang == "en" else 1]
    return SyntheticInvoice(case_id=f"{lang}-{layout}-{seed:05d}", seed=seed, lang=lang, layout=layout, truth=truth,
                            defects=defects, seller_emirate_name=name)


def _apply_defect(t: ExtractedInvoice, defect: str, rng: random.Random) -> ExtractedInvoice:
    t = t.model_copy(deep=True)
    match defect:
        case "total_off_by_cent":
            t.total_amount = _money(Decimal(t.total_amount) + CENT)
        case "missing_buyer_trn":
            t.buyer_trn = ""
        case "seller_trn_14_digits":
            t.seller_trn = t.seller_trn[:14]
        case "missing_due_date":
            t.payment_due_date = ""
        case "line_net_off_by_cent":
            ln = t.lines[rng.randrange(len(t.lines))]
            ln.net_amount = _money(Decimal(ln.net_amount) + CENT)
    return t
