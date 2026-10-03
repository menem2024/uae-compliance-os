"""Deterministic plausibility checks for an extracted invoice and the invoice VerifierProfile (spec section 5.2).

These are signals about the extraction, not validity (ADR 006, contract rule 7): a finding never makes an invoice
invalid; only validator-rs does. Paths use the canonical field-set grammar (`lines[0].net_amount`).
"""

from __future__ import annotations

import re
from collections.abc import Sequence
from datetime import date
from decimal import ROUND_HALF_UP, Decimal, InvalidOperation
from typing import ClassVar

from ai.agents.extraction.normalize import to_decimal
from ai.agents.extraction.schema import DECIMAL_FIELDS, ExtractedInvoice, ExtractionOutput, flatten, leaf
from ai.agents.verifier.core import Check, Critic, Finding, VerifierProfile

CENT = Decimal("0.01")
LINE_TOLERANCE = CENT  # per-line and per-category rounding; sums of printed amounts must match exactly
# No real invoice amount, quantity or rate reaches this, and it stays far below ~1e26, where the default 28-digit
# Decimal context stops being exact at cent precision (quantize raises InvalidOperation, sums silently round).
MAX_MAGNITUDE = Decimal("1e18")
CRITICAL_PATHS: tuple[str, ...] = (
    "invoice_number", "issue_date", "seller_trn", "buyer_trn", "currency", "vat_amount", "total_amount",
)
LOW_FIELD_CONFIDENCE = 0.8
UNKNOWN_CONFIDENCE = 0.9  # the producer reported no confidence for any critical field
TAX_CATEGORIES = frozenset({"S", "E", "O", "AE", "Z", "N"})
EMIRATE_CODES = frozenset({"AUH", "DXB", "SHJ", "UAQ", "FUJ", "AJM", "RAK"})
_ISO_4217_CODES = """
AED AFN ALL AMD ANG AOA ARS AUD AWG AZN BAM BBD BDT BGN BHD BIF BMD BND BOB BRL BSD BTN BWP BYN BZD CAD CDF CHF
CLP CNY COP CRC CUP CVE CZK DJF DKK DOP DZD EGP ERN ETB EUR FJD FKP GBP GEL GHS GIP GMD GNF GTQ GYD HKD HNL HTG
HUF IDR ILS INR IQD IRR ISK JMD JOD JPY KES KGS KHR KMF KPW KRW KWD KYD KZT LAK LBP LKR LRD LSL LYD MAD MDL MGA
MKD MMK MNT MOP MRU MUR MVR MWK MXN MYR MZN NAD NGN NIO NOK NPR NZD OMR PAB PEN PGK PHP PKR PLN PYG QAR RON RSD
RUB RWF SAR SBD SCR SDG SEK SGD SHP SLE SOS SRD SSP STN SVC SYP SZL THB TJS TMT TND TOP TRY TTD TWD TZS UAH UGX
USD UYU UZS VES VND VUV WST XAF XCD XCG XOF XPF YER ZAR ZMW ZWG
"""
ISO_4217 = frozenset(_ISO_4217_CODES.split())
_TRN = re.compile(r"^[0-9]{15}$")
_TYPE_CODE = re.compile(r"^[0-9]{3}$")


def _iso_date(value: str) -> date | None:
    try:
        return date.fromisoformat(value) if re.fullmatch(r"\d{4}-\d{2}-\d{2}", value) else None
    except ValueError:
        return None


def _cents(d: Decimal) -> Decimal | None:
    """d rounded to cents; None when it has more digits than the decimal context holds (>= ~1e26)."""
    try:
        return d.quantize(CENT, rounding=ROUND_HALF_UP)
    except InvalidOperation:
        return None


def _out_of_range(inv: ExtractedInvoice) -> list[Finding]:
    """A block finding for every decimal field at or over MAX_MAGNITUDE: implausible, and the arithmetic
    below is only exact under it. A human looks instead of the document failing."""
    out: list[Finding] = []
    for path, value in flatten(inv).items():
        if leaf(path) in DECIMAL_FIELDS and (d := to_decimal(value)) is not None and abs(d) >= MAX_MAGNITUDE:
            out.append(Finding(path, "arithmetic.out_of_range", "block", observed=value))
    return out


def _rate(value: str) -> Decimal:
    d = to_decimal(value)
    return d if d is not None else Decimal(0)


class ArithmeticCheck:
    """Line nets, line total, per-category taxable and tax amounts, VAT total, grand total, payable; values
    too large to compute with are flagged (`arithmetic.out_of_range`), never raised."""

    code_family: ClassVar[str] = "arithmetic"

    def __call__(self, output: ExtractionOutput) -> list[Finding]:
        return self.check(output.invoice)

    @staticmethod
    def check(inv: ExtractedInvoice) -> list[Finding]:
        out: list[Finding] = _out_of_range(inv)
        nets: list[Decimal | None] = []
        for i, ln in enumerate(inv.lines):
            q, p, n = to_decimal(ln.quantity), to_decimal(ln.price.net_price), to_decimal(ln.net_amount)
            nets.append(n)
            if q is not None and p is not None and n is not None:
                exp = _cents(q * p)
                if exp is None:
                    out.append(Finding(f"lines[{i}].net_amount", "arithmetic.out_of_range", "block",
                                       observed=ln.net_amount,
                                       related=(f"lines[{i}].quantity", f"lines[{i}].price.net_price")))
                elif abs(exp - n) > LINE_TOLERANCE:
                    out.append(Finding(f"lines[{i}].net_amount", "arithmetic.line_net", "warn", observed=ln.net_amount,
                                       expected=str(exp),
                                       related=(f"lines[{i}].quantity", f"lines[{i}].price.net_price")))
        known = bool(nets) and all(n is not None for n in nets)
        line_sum = sum((n for n in nets if n is not None), Decimal(0))
        lext = to_decimal(inv.totals.line_extension_amount)
        if known and lext is not None and line_sum != lext:
            out.append(Finding("totals.line_extension_amount", "arithmetic.line_total", "block",
                               observed=inv.totals.line_extension_amount, expected=str(line_sum),
                               related=tuple(f"lines[{i}].net_amount" for i in range(len(nets)))))
        groups: dict[tuple[str, Decimal], list[int]] = {}
        for i, ln in enumerate(inv.lines):
            groups.setdefault((ln.tax.code, _rate(ln.tax.rate)), []).append(i)
        seen: set[tuple[str, Decimal]] = set()
        for j, t in enumerate(inv.tax_breakdown):
            base, tax = to_decimal(t.taxable_amount), to_decimal(t.tax_amount)
            rate = _rate(t.category.rate)
            key = (t.category.code, rate)
            seen.add(key)
            if base is not None and tax is not None:
                exp = _cents(base * rate / 100)
                if exp is None:
                    out.append(Finding(f"tax_breakdown[{j}].tax_amount", "arithmetic.out_of_range", "block",
                                       observed=t.tax_amount,
                                       related=(f"tax_breakdown[{j}].taxable_amount", f"tax_breakdown[{j}].category.rate")))
                elif abs(exp - tax) > LINE_TOLERANCE:
                    out.append(Finding(f"tax_breakdown[{j}].tax_amount", "arithmetic.tax_amount", "warn",
                                       observed=t.tax_amount, expected=str(exp),
                                       related=(f"tax_breakdown[{j}].taxable_amount", f"tax_breakdown[{j}].category.rate")))
            members = groups.get(key, [])
            if known and base is not None and members:
                group_sum = sum((nets[i] or Decimal(0) for i in members), Decimal(0))
                if group_sum != base:
                    out.append(Finding(f"tax_breakdown[{j}].taxable_amount", "arithmetic.taxable_amount", "warn",
                                       observed=t.taxable_amount, expected=str(group_sum),
                                       related=tuple(p for i in members
                                                     for p in (f"lines[{i}].net_amount", f"lines[{i}].tax.rate"))))
        if inv.tax_breakdown:
            for key, members in groups.items():
                if key not in seen and key[0]:
                    i = members[0]
                    out.append(Finding(f"lines[{i}].tax.rate", "arithmetic.line_category_missing", "warn",
                                       observed=inv.lines[i].tax.rate, related=(f"lines[{i}].tax.code",)))
        taxes = [to_decimal(t.tax_amount) for t in inv.tax_breakdown]
        vat = to_decimal(inv.vat_amount)
        if taxes and all(x is not None for x in taxes) and vat is not None:
            tax_sum = sum((x for x in taxes if x is not None), Decimal(0))
            if tax_sum != vat:
                out.append(Finding("vat_amount", "arithmetic.vat_total", "block", observed=inv.vat_amount,
                                   expected=str(tax_sum),
                                   related=tuple(f"tax_breakdown[{j}].tax_amount" for j in range(len(taxes)))))
        excl_path = "totals.tax_exclusive_amount" if inv.totals.tax_exclusive_amount else "totals.line_extension_amount"
        excl = to_decimal(inv.totals.tax_exclusive_amount or inv.totals.line_extension_amount)
        total = to_decimal(inv.total_amount)
        if excl is not None and vat is not None and total is not None and excl + vat != total:
            out.append(Finding("total_amount", "arithmetic.total", "block", observed=inv.total_amount,
                               expected=str(excl + vat), related=(excl_path, "vat_amount")))
        payable = to_decimal(inv.totals.payable_amount)
        if payable is not None and total is not None and payable != total:
            out.append(Finding("totals.payable_amount", "arithmetic.payable", "warn",
                               observed=inv.totals.payable_amount, expected=inv.total_amount,
                               related=("total_amount",)))
        return out


class IdentifiersCheck:
    """Invoice number present; TRNs are 15 digits; seller and buyer TRN differ."""

    code_family: ClassVar[str] = "identifiers"

    def __call__(self, output: ExtractionOutput) -> list[Finding]:
        return self.check(output.invoice)

    @staticmethod
    def check(inv: ExtractedInvoice) -> list[Finding]:
        out: list[Finding] = []
        if not inv.invoice_number:
            out.append(Finding("invoice_number", "identifiers.invoice_number_missing", "block"))
        if not inv.seller_trn:
            out.append(Finding("seller_trn", "identifiers.seller_trn_missing", "warn"))
        for path, trn in (("seller_trn", inv.seller_trn), ("buyer_trn", inv.buyer_trn)):
            if trn and not _TRN.match(trn):
                out.append(Finding(path, "identifiers.trn_shape", "block", observed=trn))
        if inv.seller_trn and inv.seller_trn == inv.buyer_trn:
            out.append(Finding("buyer_trn", "identifiers.same_party", "block", observed=inv.buyer_trn,
                               related=("seller_trn",)))
        return out


class DatesCodesCheck:
    """ISO dates, due date not before issue date, ISO 4217 currency, tax category and emirate code sets."""

    code_family: ClassVar[str] = "dates_codes"

    def __call__(self, output: ExtractionOutput) -> list[Finding]:
        return self.check(output.invoice)

    @staticmethod
    def check(inv: ExtractedInvoice) -> list[Finding]:
        out: list[Finding] = []
        issue = _iso_date(inv.issue_date)
        if not inv.issue_date:
            out.append(Finding("issue_date", "dates_codes.issue_date_missing", "block"))
        elif issue is None:
            out.append(Finding("issue_date", "dates_codes.issue_date_format", "block", observed=inv.issue_date))
        if inv.payment_due_date:
            due = _iso_date(inv.payment_due_date)
            if due is None:
                out.append(Finding("payment_due_date", "dates_codes.due_date_format", "warn",
                                   observed=inv.payment_due_date))
            elif issue is not None and due < issue:
                out.append(Finding("payment_due_date", "dates_codes.due_before_issue", "warn",
                                   observed=inv.payment_due_date, related=("issue_date",)))
        if not inv.currency:
            out.append(Finding("currency", "dates_codes.currency_missing", "warn"))
        elif inv.currency not in ISO_4217:
            out.append(Finding("currency", "dates_codes.currency_code", "warn", observed=inv.currency))
        if inv.invoice_type_code and not _TYPE_CODE.match(inv.invoice_type_code):
            out.append(Finding("invoice_type_code", "dates_codes.type_code", "warn", observed=inv.invoice_type_code))
        sub = inv.seller.postal_address.country_subdivision
        if sub and sub not in EMIRATE_CODES:
            out.append(Finding("seller.postal_address.country_subdivision", "dates_codes.emirate", "warn", observed=sub))
        cats = [(f"tax_breakdown[{j}].category", t.category.code, t.category.rate)
                for j, t in enumerate(inv.tax_breakdown)]
        cats += [(f"lines[{i}].tax", ln.tax.code, ln.tax.rate) for i, ln in enumerate(inv.lines)]
        for base, code, rate in cats:
            if code and code not in TAX_CATEGORIES:
                out.append(Finding(f"{base}.code", "dates_codes.tax_category", "warn", observed=code))
            if rate and to_decimal(rate) is None:
                out.append(Finding(f"{base}.rate", "dates_codes.tax_rate", "warn", observed=rate))
        return out


INVOICE_CHECKS: tuple[Check[ExtractionOutput], ...] = (ArithmeticCheck(), IdentifiersCheck(), DatesCodesCheck())


def invoice_findings(inv: ExtractedInvoice) -> list[Finding]:
    """Every deterministic finding for one invoice (the tabular branch runs this per imported invoice)."""
    return [f for c in (ArithmeticCheck, IdentifiersCheck, DatesCodesCheck) for f in c.check(inv)]


def field_confidences(out: ExtractionOutput) -> dict[str, float]:
    """path -> confidence in [0, 1] for the paths that exist in the extracted invoice (last report wins)."""
    known = flatten(out.invoice)
    return {fc.path: min(1.0, max(0.0, fc.confidence)) for fc in out.field_confidence if fc.path in known}


def extraction_confidence(out: ExtractionOutput) -> float:
    """Producer confidence: the mean reported confidence of the critical header fields; 0.9 when none is
    reported. A low field is still re-read by the critic (`invoice_critic_when`), so a mean is safe here."""
    conf = field_confidences(out)
    vals = [conf[p] for p in CRITICAL_PATHS if p in conf]
    return round(sum(vals) / len(vals), 3) if vals else UNKNOWN_CONFIDENCE


def low_confidence_paths(out: ExtractionOutput) -> list[str]:
    conf = field_confidences(out)
    return [p for p in CRITICAL_PATHS if conf.get(p, 1.0) < LOW_FIELD_CONFIDENCE]


def invoice_critic_when(out: ExtractionOutput, findings: Sequence[Finding]) -> bool:
    """The critic runs when a check reports warn/block or a critical field's confidence is < 0.8."""
    return any(f.severity != "info" for f in findings) or bool(low_confidence_paths(out))


def invoice_profile(critic: Critic[ExtractionOutput] | None = None,
                    escalation_critic: Critic[ExtractionOutput] | None = None) -> VerifierProfile[ExtractionOutput]:
    return VerifierProfile(output_type=ExtractionOutput, checks=INVOICE_CHECKS, critic=critic,
                           critic_when=invoice_critic_when, escalation_critic=escalation_critic,
                           accept_threshold=0.85, max_revisions=1, producer_confidence=extraction_confidence)
