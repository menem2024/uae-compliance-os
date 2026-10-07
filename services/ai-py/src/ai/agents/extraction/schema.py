"""The Phase 1 extraction field set (spec section 5.5), named after canonical-invoice v0.2 proto fields.

Absent = empty string (canonical-invoice rule 4). No Optional and no free-form dict anywhere: Structured
Outputs needs `additionalProperties: false` on every object. The synthetic ground truth is this model.
"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field


class _Strict(BaseModel):
    model_config = ConfigDict(extra="forbid")


class PostalAddress(_Strict):
    country_subdivision: str = ""  # AUH, DXB, SHJ, UAQ, FUJ, AJM, RAK


class SellerParty(_Strict):
    name: str = ""
    postal_address: PostalAddress = Field(default_factory=PostalAddress)


class BuyerParty(_Strict):
    name: str = ""


class Totals(_Strict):
    line_extension_amount: str = ""
    tax_exclusive_amount: str = ""
    payable_amount: str = ""


class TaxCategory(_Strict):
    code: str = ""  # S, E, O, AE, Z, N
    rate: str = ""  # percent; empty = absent


class TaxSubtotal(_Strict):
    taxable_amount: str = ""
    tax_amount: str = ""
    category: TaxCategory = Field(default_factory=TaxCategory)


class Item(_Strict):
    name: str = ""


class Price(_Strict):
    net_price: str = ""


class InvoiceLine(_Strict):
    item: Item = Field(default_factory=Item)
    quantity: str = ""
    unit_code: str = ""
    price: Price = Field(default_factory=Price)
    net_amount: str = ""
    tax: TaxCategory = Field(default_factory=TaxCategory)


class ExtractedInvoice(_Strict):
    invoice_number: str = ""
    issue_date: str = ""  # YYYY-MM-DD
    invoice_type_code: str = ""  # 380 invoice, 381 credit note
    currency: str = ""  # ISO 4217
    seller: SellerParty = Field(default_factory=SellerParty)
    seller_trn: str = ""
    buyer: BuyerParty = Field(default_factory=BuyerParty)
    buyer_trn: str = ""
    payment_due_date: str = ""
    totals: Totals = Field(default_factory=Totals)
    vat_amount: str = ""
    total_amount: str = ""
    note: str = ""
    tax_breakdown: list[TaxSubtotal] = Field(default_factory=list)
    lines: list[InvoiceLine] = Field(default_factory=list)


class FieldConfidence(_Strict):
    path: str
    confidence: float


class ExtractionOutput(_Strict):
    """The Extraction agent's structured output."""

    invoice: ExtractedInvoice
    field_confidence: list[FieldConfidence]
    language: Literal["ar", "en", "mixed"]


HEADER_PATHS: tuple[str, ...] = (
    "invoice_number", "issue_date", "invoice_type_code", "currency", "seller.name", "seller_trn",
    "seller.postal_address.country_subdivision", "buyer.name", "buyer_trn", "payment_due_date",
    "totals.line_extension_amount", "totals.tax_exclusive_amount", "vat_amount", "total_amount",
    "totals.payable_amount", "note",
)
TAX_FIELDS: tuple[str, ...] = ("taxable_amount", "tax_amount", "category.code", "category.rate")
LINE_FIELDS: tuple[str, ...] = (
    "item.name", "quantity", "unit_code", "price.net_price", "net_amount", "tax.code", "tax.rate",
)
DECIMAL_FIELDS: frozenset[str] = frozenset({
    "totals.line_extension_amount", "totals.tax_exclusive_amount", "vat_amount", "total_amount",
    "totals.payable_amount", "taxable_amount", "tax_amount", "category.rate", "quantity", "price.net_price",
    "net_amount", "tax.rate",
})
DATE_FIELDS: frozenset[str] = frozenset({"issue_date", "payment_due_date"})
CODE_FIELDS: frozenset[str] = frozenset({
    "invoice_type_code", "currency", "seller.postal_address.country_subdivision", "category.code", "unit_code",
    "tax.code",
})
TRN_FIELDS: frozenset[str] = frozenset({"seller_trn", "buyer_trn"})


def _get(obj: BaseModel, dotted: str) -> str:
    cur: object = obj
    for part in dotted.split("."):
        cur = getattr(cur, part)
    return cur if isinstance(cur, str) else ""


def flatten(inv: ExtractedInvoice) -> dict[str, str]:
    """Every field-set path (canonical grammar, e.g. `lines[0].price.net_price`) -> value."""
    out = {p: _get(inv, p) for p in HEADER_PATHS}
    for i, t in enumerate(inv.tax_breakdown):
        out.update({f"tax_breakdown[{i}].{f}": _get(t, f) for f in TAX_FIELDS})
    for i, ln in enumerate(inv.lines):
        out.update({f"lines[{i}].{f}": _get(ln, f) for f in LINE_FIELDS})
    return out


def leaf(path: str) -> str:
    """The field-set name of a path: `lines[3].tax.rate` -> `tax.rate`, `seller.name` -> `seller.name`."""
    head, _, rest = path.partition("].")
    return rest if rest else head
