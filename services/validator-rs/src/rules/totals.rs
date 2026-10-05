//! `totals` family rules (spec 5.2.4): the document totals (IBG-22, `cac:LegalMonetaryTotal`),
//! the document-level allowances and charges (IBG-20, IBG-21) and, because those schematron
//! contexts match every `cac:AllowanceCharge` outside a price, the line allowances and charges
//! (IBG-27, IBG-28); plus the root rules on IBT-005, IBT-006, IBT-111 and BTAE-20. Coverage:
//! `rulesets/pint-ae-1.0.4/coverage/totals.tsv`; fixtures: `mutations/totals.jsonl`, run by
//! `tests/family_fixtures.rs`.
//!
//! Each rule is "this official assert fails on the XML the exporter writes for this `Doc`"
//! (Rule authoring protocol). What decides which rule sees which `cac:AllowanceCharge`:
//!
//! * in each compiled pattern a node is handled by the first (highest-priority) rule whose
//!   context matches. `cac:Price/cac:AllowanceCharge` precedes the `ChargeIndicator` contexts in
//!   both patterns, so the price discount never reaches these rules, and the bare
//!   `cac:AllowanceCharge` context of `ibr-082` comes after them, so it is never reached at all
//!   (the exporter writes `true` or `false` from the `is_charge` bool);
//! * every allowance or charge of the model is written (its `ChargeIndicator` is content), at
//!   the root or inside its line; only a document-level one carries a `cac:TaxCategory`;
//! * `cac:LegalMonetaryTotal` is written when one of its amounts parses ([`lmt_written`]), and
//!   IBT-200 only as `true` inside the document-currency `TaxTotal`, which needs IBT-110
//!   ([`tax_included`]).
//!
//! **Money.** The totals chain (`ibr-co-10` to `ibr-co-16`) adds, subtracts and rounds amounts
//! of up to 28 significant digits each. `rust_decimal` rounds a sum silently when the exact
//! result needs more digits than its 96-bit mantissa holds (`1e25 + 0.0049…` becomes
//! `…0.005`, which then rounds to `.01` where the exact value rounds to `.00`), while Saxon's
//! `xs:decimal` is exact. The chain is therefore computed in [`Cents`]: whole cents plus an
//! exact sub-cent remainder in checked `i128`, which holds every value the contract admits
//! without rounding; a checked overflow is `None`, so the rule fires and nothing is suggested.
//! `round(x * 10 * 10) div 100` is [`Cents::round_half_up`] (half toward positive infinity,
//! `decimal::xpath_round2`'s semantics, cross-checked by a property test). No binary floating
//! point anywhere; `ibr-131-ae` and `ibr-146-ae`, which the XPath evaluates in `xs:double`, use
//! exact decimal (contract upstream defect 5).
//!
//! **Suggestions** (`fix = value`, protocol step 8) belong to the chain rules only. Each
//! suggests the one value of its field computed from the **current** inputs, as a string with at
//! most two decimals that the contract grammar accepts. Changing that field can break only the
//! next rule of the chain (IBT-106, IBT-107, IBT-108 → IBT-109 → IBT-112 → IBT-115), which then
//! suggests from the new value; no other official rule reads these elements
//! (`only_known_rules_read_the_totals_elements`), and the one that reads IBT-115's sign
//! (`ibr-127-ae`) is protected explicitly ([`breaks_due_date_rule`]). Nothing is suggested when
//! an input is missing.
//!
//! Status of the 41 rows: 37 implemented ([`RULES`]) and 4 structural (`ibr-082`, `ibr-sr-30`,
//! `ibr-sr-31`, `ibr-sr-61`, proved by `structural_rules_hold_on_every_exported_allowance_charge`).

use rust_decimal::Decimal;

use crate::decimal;
use crate::doc::{AllowanceChargeDec, Dec, Doc, DocKind, TotalsDec, tax_scheme, text};
use crate::pb;
use crate::rule::{Finding, Rule, Sink};

/// Every implemented rule of the family; the coverage rows with status `implemented` are exactly
/// these.
pub static RULES: &[Rule] = &[
    Rule {
        id: "aligned-ibrp-032",
        check: aligned_ibrp_032,
    },
    Rule {
        id: "aligned-ibrp-037",
        check: aligned_ibrp_037,
    },
    Rule {
        id: "aligned-ibrp-057",
        check: aligned_ibrp_057,
    },
    Rule {
        id: "aligned-ibrp-058",
        check: aligned_ibrp_058,
    },
    Rule {
        id: "ibr-012",
        check: ibr_012,
    },
    Rule {
        id: "ibr-013",
        check: ibr_013,
    },
    Rule {
        id: "ibr-014",
        check: ibr_014,
    },
    Rule {
        id: "ibr-015",
        check: ibr_015,
    },
    Rule {
        id: "ibr-031",
        check: ibr_031,
    },
    Rule {
        id: "ibr-033",
        check: ibr_033,
    },
    Rule {
        id: "ibr-036",
        check: ibr_036,
    },
    Rule {
        id: "ibr-038",
        check: ibr_038,
    },
    Rule {
        id: "ibr-041",
        check: ibr_041,
    },
    Rule {
        id: "ibr-042",
        check: ibr_042,
    },
    Rule {
        id: "ibr-043",
        check: ibr_043,
    },
    Rule {
        id: "ibr-044",
        check: ibr_044,
    },
    Rule {
        id: "ibr-053",
        check: ibr_053,
    },
    Rule {
        id: "ibr-084",
        check: ibr_084,
    },
    Rule {
        id: "ibr-091",
        check: ibr_091,
    },
    Rule {
        id: "ibr-114-ae",
        check: ibr_114_ae,
    },
    Rule {
        id: "ibr-115-ae",
        check: ibr_115_ae,
    },
    Rule {
        id: "ibr-121",
        check: ibr_121,
    },
    Rule {
        id: "ibr-122",
        check: ibr_122,
    },
    Rule {
        id: "ibr-123",
        check: ibr_123,
    },
    Rule {
        id: "ibr-125",
        check: ibr_125,
    },
    Rule {
        id: "ibr-131-ae",
        check: ibr_131_ae,
    },
    Rule {
        id: "ibr-146-ae",
        check: ibr_146_ae,
    },
    Rule {
        id: "ibr-153-ae",
        check: ibr_153_ae,
    },
    Rule {
        id: "ibr-168-ae",
        check: ibr_168_ae,
    },
    Rule {
        id: "ibr-169-ae",
        check: ibr_169_ae,
    },
    Rule {
        id: "ibr-175-ae",
        check: ibr_175_ae,
    },
    Rule {
        id: "ibr-co-10",
        check: ibr_co_10,
    },
    Rule {
        id: "ibr-co-11",
        check: ibr_co_11,
    },
    Rule {
        id: "ibr-co-12",
        check: ibr_co_12,
    },
    Rule {
        id: "ibr-co-13",
        check: ibr_co_13,
    },
    Rule {
        id: "ibr-co-15",
        check: ibr_co_15,
    },
    Rule {
        id: "ibr-co-16",
        check: ibr_co_16,
    },
];

/// `AED`, the UAE dirham (IBT-006 of `ibr-153-ae` and `ibr-175-ae`, the line BTAE-08 currency).
const AED: &str = "AED";

// ------------------------------------------------------------------------------ exact money

/// One cent in sub-cent units (`10^26`): a decimal of scale at most 28 has at most 26 digits
/// below the cent.
const SUB_PER_CENT: i128 = 10i128.pow(26);

/// An exact amount: `value * 100 = whole + sub / 10^26` with `0 <= sub < 10^26`. Every decimal
/// the contract admits (28 significant digits, scale at most 28) converts without rounding:
/// `|whole| < 10^31`, and sums of millions of them stay far below `i128::MAX` (`1.7 * 10^38`).
/// All arithmetic is checked; `None` means overflow.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct Cents {
    whole: i128,
    sub: i128,
}

impl Cents {
    const ZERO: Cents = Cents { whole: 0, sub: 0 };

    fn of(x: Decimal) -> Option<Cents> {
        let (m, scale) = (x.mantissa(), x.scale());
        if scale <= 2 {
            let whole = m.checked_mul(10i128.checked_pow(2 - scale)?)?;
            return Some(Cents { whole, sub: 0 });
        }
        // value * 100 = m / 10^(scale - 2), with 1 <= scale - 2 <= 26.
        let unit = 10i128.checked_pow(scale - 2)?;
        let sub = m
            .rem_euclid(unit)
            .checked_mul(10i128.checked_pow(28 - scale)?)?;
        Some(Cents {
            whole: m.div_euclid(unit),
            sub,
        })
    }

    fn add(self, other: Cents) -> Option<Cents> {
        let whole = self.whole.checked_add(other.whole)?;
        let sub = self.sub.checked_add(other.sub)?;
        if sub >= SUB_PER_CENT {
            Some(Cents {
                whole: whole.checked_add(1)?,
                sub: sub.checked_sub(SUB_PER_CENT)?,
            })
        } else {
            Some(Cents { whole, sub })
        }
    }

    fn neg(self) -> Option<Cents> {
        if self.sub == 0 {
            return Some(Cents {
                whole: self.whole.checked_neg()?,
                sub: 0,
            });
        }
        Some(Cents {
            whole: self.whole.checked_neg()?.checked_sub(1)?,
            sub: SUB_PER_CENT.checked_sub(self.sub)?,
        })
    }

    fn sub(self, other: Cents) -> Option<Cents> {
        self.add(other.neg()?)
    }

    /// XPath `round(x * 10 * 10) div 100`: whole cents, a half rounded toward positive infinity
    /// (`floor(x * 100 + 0.5)`).
    fn round_half_up(self) -> Option<Cents> {
        self.round(self.sub >= SUB_PER_CENT / 2)
    }

    /// Whole cents, a half rounded toward negative infinity (`ceil(x * 100 - 0.5)`): the one
    /// two-decimal `y` with `x - 0.005 <= y < x + 0.005` (`ibr-co-16`'s rounding window).
    fn round_half_down(self) -> Option<Cents> {
        self.round(self.sub > SUB_PER_CENT / 2)
    }

    fn round(self, up: bool) -> Option<Cents> {
        let whole = if up {
            self.whole.checked_add(1)?
        } else {
            self.whole
        };
        Some(Cents { whole, sub: 0 })
    }

    /// The amount as a decimal with two decimals (fewer when 28 significant digits do not hold
    /// two), or `None` when it has a sub-cent part or no form the contract grammar accepts.
    fn to_decimal(self) -> Option<Decimal> {
        if self.sub != 0 {
            return None;
        }
        let mut whole = self.whole;
        for scale in [2u32, 1, 0] {
            if let Ok(d) = Decimal::try_from_i128_with_scale(whole, scale)
                && decimal::parse(&d.to_string()).is_ok()
            {
                return Some(d);
            }
            if whole % 10 != 0 {
                return None;
            }
            whole /= 10;
        }
        None
    }
}

/// `sum()` of exact amounts; zero for none.
fn sum_cents(xs: impl IntoIterator<Item = Decimal>) -> Option<Cents> {
    xs.into_iter()
        .try_fold(Cents::ZERO, |acc, x| acc.add(Cents::of(x)?))
}

/// `xs:decimal(field) = expected`: false when either side is absent.
fn equals(field: &Dec<'_>, expected: Option<Cents>) -> bool {
    match (field.value.and_then(Cents::of), expected) {
        (Some(v), Some(e)) => v == e,
        _ => false,
    }
}

/// Suggests `value` when it exists and has a two-decimal form.
fn suggest(f: &mut Finding, value: Option<Cents>) {
    if let Some(d) = value.and_then(Cents::to_decimal) {
        f.suggest(d);
    }
}

// ------------------------------------------------------------------------------ helpers

/// `cac:LegalMonetaryTotal` is written when one of its amounts parses
/// (`export::B::legal_monetary_total`).
fn lmt_written(doc: &Doc<'_>) -> bool {
    let t = &doc.totals;
    [
        &t.line_extension_amount,
        &t.tax_exclusive_amount,
        &doc.total_amount,
        &t.allowance_total_amount,
        &t.charge_total_amount,
        &t.paid_amount,
        &t.rounding_amount,
        &t.payable_amount,
    ]
    .iter()
    .any(|d| d.exists())
}

/// `cac:TaxTotal/cbc:TaxIncludedIndicator = true()`: IBT-200 is written, as `true`, only inside
/// the document-currency `TaxTotal`, which needs IBT-110.
fn tax_included(doc: &Doc<'_>) -> bool {
    doc.vat_amount.exists()
        && doc
            .inv
            .totals
            .as_ref()
            .is_some_and(|t| t.tax_inclusive_pricing)
}

/// XML whitespace as `normalize-space` sees it.
const XML_WS: [char; 4] = [' ', '\t', '\n', '\r'];

/// `value = normalize-space(s)`, without allocating.
fn eq_normalized(value: &str, s: &str) -> bool {
    let mut rest = value;
    for (n, token) in s.split(XML_WS).filter(|t| !t.is_empty()).enumerate() {
        if n > 0 {
            match rest.strip_prefix(' ') {
                Some(r) => rest = r,
                None => return false,
            }
        }
        match rest.strip_prefix(token) {
            Some(r) => rest = r,
            None => return false,
        }
    }
    rest.is_empty()
}

/// A field of an allowance or charge, for paths and business terms.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Field {
    Amount,
    BaseAmount,
    Percentage,
    Reason,
}

impl Field {
    fn name(self) -> &'static str {
        match self {
            Field::Amount => "amount",
            Field::BaseAmount => "base_amount",
            Field::Percentage => "percentage",
            Field::Reason => "reason",
        }
    }

    /// The business term of this field of a document-level or line allowance or charge.
    fn term(self, line: bool, charge: bool) -> &'static str {
        match (line, charge, self) {
            (false, false, Field::Amount) => "IBT-092",
            (false, false, Field::BaseAmount) => "IBT-093",
            (false, false, Field::Percentage) => "IBT-094",
            (false, false, Field::Reason) => "IBT-097",
            (false, true, Field::Amount) => "IBT-099",
            (false, true, Field::BaseAmount) => "IBT-100",
            (false, true, Field::Percentage) => "IBT-101",
            (false, true, Field::Reason) => "IBT-104",
            (true, false, Field::Amount) => "IBT-136",
            (true, false, Field::BaseAmount) => "IBT-137",
            (true, false, Field::Percentage) => "IBT-138",
            (true, false, Field::Reason) => "IBT-139",
            (true, true, Field::Amount) => "IBT-141",
            (true, true, Field::BaseAmount) => "IBT-142",
            (true, true, Field::Percentage) => "IBT-143",
            (true, true, Field::Reason) => "IBT-144",
        }
    }
}

/// One written `cac:AllowanceCharge` outside a price: at the root (`line = None`) or in line
/// `line`, at `index` there.
#[derive(Debug, Clone, Copy)]
struct Ac<'d, 'a> {
    line: Option<usize>,
    index: usize,
    model: &'a pb::AllowanceCharge,
    dec: &'d AllowanceChargeDec<'a>,
}

impl Ac<'_, '_> {
    /// Reports `field` of this allowance or charge at its concrete path and term (F7).
    fn fail<'s>(&self, sink: &'s mut Sink<'_>, field: Field) -> &'s mut Finding {
        let path = match self.line {
            None => format!("allowances_charges[{}].{}", self.index, field.name()),
            Some(i) => format!(
                "lines[{i}].allowances_charges[{}].{}",
                self.index,
                field.name()
            ),
        };
        sink.fail_at(path)
            .term(field.term(self.line.is_some(), self.model.is_charge))
    }

    /// `exists(cbc:AllowanceChargeReason) or exists(cbc:AllowanceChargeReasonCode)`.
    fn has_reason(&self) -> bool {
        text(&self.model.reason).is_some() || text(&self.model.reason_code).is_some()
    }

    /// `cac:TaxCategory/cbc:ID` as written (a document-level one only).
    fn category_code(&self) -> Option<&str> {
        if self.line.is_some() {
            return None;
        }
        self.model.tax_category.as_ref().and_then(|c| text(&c.code))
    }
}

/// The document-level allowances and charges.
fn document_level<'d, 'a>(doc: &'d Doc<'a>) -> impl Iterator<Item = Ac<'d, 'a>> + 'd {
    doc.inv
        .allowances_charges
        .iter()
        .zip(&doc.allowances_charges)
        .enumerate()
        .map(|(index, (model, dec))| Ac {
            line: None,
            index,
            model,
            dec,
        })
}

/// The line allowances and charges, line by line.
fn line_level<'d, 'a>(doc: &'d Doc<'a>) -> impl Iterator<Item = Ac<'d, 'a>> + 'd {
    doc.inv
        .lines
        .iter()
        .zip(&doc.lines)
        .enumerate()
        .flat_map(|(i, (l, d))| {
            l.allowances_charges
                .iter()
                .zip(&d.allowances_charges)
                .enumerate()
                .map(move |(index, (model, dec))| Ac {
                    line: Some(i),
                    index,
                    model,
                    dec,
                })
        })
}

/// Every written allowance (`charge = false`) or charge outside a price: the contexts
/// `cac:AllowanceCharge[cbc:ChargeIndicator = …]`, with or without `[not(ancestor::cac:Price)]`.
fn every<'d, 'a>(doc: &'d Doc<'a>, charge: bool) -> impl Iterator<Item = Ac<'d, 'a>> + 'd {
    document_level(doc)
        .chain(line_level(doc))
        .filter(move |ac| ac.model.is_charge == charge)
}

/// The document-level allowances (`charge = false`) or charges.
fn document<'d, 'a>(doc: &'d Doc<'a>, charge: bool) -> impl Iterator<Item = Ac<'d, 'a>> + 'd {
    document_level(doc).filter(move |ac| ac.model.is_charge == charge)
}

/// The line allowances (`charge = false`) or charges.
fn in_lines<'d, 'a>(doc: &'d Doc<'a>, charge: bool) -> impl Iterator<Item = Ac<'d, 'a>> + 'd {
    line_level(doc).filter(move |ac| ac.model.is_charge == charge)
}

// ------------------------------------------------------------------------------ rules

/// `aligned-ibrp-032`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()][not(ancestor::cac:Price)]`, test
/// `not(parent::ubl:Invoice|parent::cn:CreditNote) or exists(cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID)`.
fn aligned_ibrp_032(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    vat_category_code(doc, sink, false);
}

/// `aligned-ibrp-037`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `not(parent::ubl:Invoice|parent::cn:CreditNote) or exists(cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID)`.
fn aligned_ibrp_037(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    vat_category_code(doc, sink, true);
}

/// `aligned-ibrp-032`/`-037`: a document-level allowance or charge (a line one's parent is the
/// line) needs a category code in the VAT scheme. The category is written whenever one of its
/// fields is set, its code as `cbc:ID`, its scheme trimmed or the default `VAT`;
/// `normalize-space(upper-case(s)) = 'VAT'` holds exactly when that scheme is `vat` in any ASCII
/// case (no other character upper-cases to V, A or T, and inner whitespace would remain).
fn vat_category_code(doc: &Doc<'_>, sink: &mut Sink<'_>, charge: bool) {
    for ac in document(doc, charge) {
        let coded =
            ac.model.tax_category.as_ref().is_some_and(|c| {
                text(&c.code).is_some() && tax_scheme(c).eq_ignore_ascii_case("VAT")
            });
        if !coded {
            sink.fail(&[ac.index]);
        }
    }
}

/// `aligned-ibrp-057`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()][not(ancestor::cac:Price)]`, test
/// `not(cbc:MultiplierFactorNumeric or cbc:BaseAmount) or (cbc:MultiplierFactorNumeric and cbc:BaseAmount)`.
fn aligned_ibrp_057(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    base_and_percentage(doc, sink, false);
}

/// `aligned-ibrp-058`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `not(cbc:MultiplierFactorNumeric or cbc:BaseAmount) or (cbc:MultiplierFactorNumeric and cbc:BaseAmount)`.
fn aligned_ibrp_058(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    base_and_percentage(doc, sink, true);
}

/// Both or neither of base amount and percentage, on every allowance (charge) outside a price,
/// document level and line level; the finding names the missing one (F7).
fn base_and_percentage(doc: &Doc<'_>, sink: &mut Sink<'_>, charge: bool) {
    for ac in every(doc, charge) {
        match (ac.dec.base_amount.exists(), ac.dec.percentage.exists()) {
            (false, true) => {
                ac.fail(sink, Field::BaseAmount);
            }
            (true, false) => {
                ac.fail(sink, Field::Percentage);
            }
            _ => {}
        }
    }
}

/// `ibr-012`, context `cac:LegalMonetaryTotal`, test
/// `exists(cbc:LineExtensionAmount)`.
fn ibr_012(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    present(doc, sink, &doc.totals.line_extension_amount);
}

/// `ibr-013`, context `cac:LegalMonetaryTotal`, test
/// `exists(cbc:TaxExclusiveAmount)`.
fn ibr_013(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    present(doc, sink, &doc.totals.tax_exclusive_amount);
}

/// `ibr-014`, context `cac:LegalMonetaryTotal`, test
/// `exists(cbc:TaxInclusiveAmount)`.
fn ibr_014(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    present(doc, sink, &doc.total_amount);
}

/// `ibr-015`, context `cac:LegalMonetaryTotal`, test
/// `exists(cbc:PayableAmount)`.
fn ibr_015(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    present(doc, sink, &doc.totals.payable_amount);
}

/// `exists(amount)` in the `LegalMonetaryTotal` context; an invalid amount is not written.
fn present(doc: &Doc<'_>, sink: &mut Sink<'_>, amount: &Dec<'_>) {
    if lmt_written(doc) && !amount.exists() {
        sink.fail(&[]);
    }
}

/// `ibr-031`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()]`, test
/// `((exists(cbc:Amount) and  not(ancestor::cac:InvoiceLine | ancestor::cac:CreditNoteLine))) or (ancestor::cac:InvoiceLine | ancestor::cac:CreditNoteLine)`.
/// A line allowance passes by the `ancestor` clause.
fn ibr_031(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    without_amount(document(doc, false), sink);
}

/// `ibr-033`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()]`, test
/// `((exists(cbc:AllowanceChargeReason) or exists(cbc:AllowanceChargeReasonCode)) and not(exists(ancestor::cac:InvoiceLine|ancestor::cac:CreditNoteLine))) or (ancestor::cac:InvoiceLine | ancestor::cac:CreditNoteLine)`.
fn ibr_033(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    without_reason(document(doc, false), sink);
}

/// `ibr-036`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `((exists(cbc:Amount) and  not(ancestor::cac:InvoiceLine | ancestor::cac:CreditNoteLine))) or (ancestor::cac:InvoiceLine | ancestor::cac:CreditNoteLine)`.
fn ibr_036(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    without_amount(document(doc, true), sink);
}

/// `ibr-038`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `((exists(cbc:AllowanceChargeReason) or exists(cbc:AllowanceChargeReasonCode)) and not(exists(ancestor::cac:InvoiceLine|ancestor::cac:CreditNoteLine))) or (ancestor::cac:InvoiceLine | ancestor::cac:CreditNoteLine)`.
fn ibr_038(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    without_reason(document(doc, true), sink);
}

/// `ibr-041`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()]`, test
/// `exists(cbc:Amount) or not(exists(ancestor::cac:InvoiceLine|ancestor::cac:CreditNoteLine))`.
/// The price discount is matched first by `cac:Price/cac:AllowanceCharge`, so only line
/// allowances count.
fn ibr_041(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    without_amount(in_lines(doc, false), sink);
}

/// `ibr-042`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()]`, test
/// `exists(cbc:AllowanceChargeReason) or exists(cbc:AllowanceChargeReasonCode) or not(exists(ancestor::cac:InvoiceLine|ancestor::cac:CreditNoteLine))`.
fn ibr_042(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    without_reason(in_lines(doc, false), sink);
}

/// `ibr-043`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `exists(cbc:Amount) or not(exists(ancestor::cac:InvoiceLine|ancestor::cac:CreditNoteLine))`.
fn ibr_043(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    without_amount(in_lines(doc, true), sink);
}

/// `ibr-044`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `exists(cbc:AllowanceChargeReason) or exists(cbc:AllowanceChargeReasonCode)`.
/// The test has no line condition, so a document-level charge without a reason fails it too.
fn ibr_044(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    without_reason(every(doc, true), sink);
}

fn without_amount<'d, 'a: 'd>(acs: impl Iterator<Item = Ac<'d, 'a>>, sink: &mut Sink<'_>) {
    for ac in acs {
        if !ac.dec.amount.exists() {
            ac.fail(sink, Field::Amount);
        }
    }
}

fn without_reason<'d, 'a: 'd>(acs: impl Iterator<Item = Ac<'d, 'a>>, sink: &mut Sink<'_>) {
    for ac in acs {
        if !ac.has_reason() {
            ac.fail(sink, Field::Reason);
        }
    }
}

/// `ibr-053`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `every $taxcurrency in cbc:TaxCurrencyCode satisfies exists(//cac:TaxTotal/cbc:TaxAmount[@currencyID=$taxcurrency])`.
/// Every `TaxTotal/TaxAmount` counts, with its `currencyID` compared as written: IBT-111 carries
/// IBT-006 itself, IBT-110 carries IBT-005, and each line's BTAE-08 (written with BTAE-10)
/// carries `AED`.
fn ibr_053(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    let Some(code) = text(&inv.tax_currency) else {
        return;
    };
    let in_tax_currency = doc.totals.tax_amount_accounting_currency.exists()
        || (doc.vat_amount.exists() && text(&inv.currency).unwrap_or("") == code)
        || (code == AED
            && doc
                .lines
                .iter()
                .any(|l| l.amount_aed.exists() && l.vat_amount_aed.exists()));
    if !in_tax_currency {
        sink.fail(&[]);
    }
}

/// `ibr-084`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `not(cbc:TaxCurrencyCode) or (cac:TaxTotal/cbc:TaxAmount[@currencyID=normalize-space(../../cbc:TaxCurrencyCode)] <= 0 and cac:TaxTotal/cbc:TaxAmount[@currencyID=normalize-space(../../cbc:DocumentCurrencyCode)] <= 0) or (cac:TaxTotal/cbc:TaxAmount[@currencyID=normalize-space(../../cbc:TaxCurrencyCode)] >= 0 and cac:TaxTotal/cbc:TaxAmount[@currencyID=normalize-space(../../cbc:DocumentCurrencyCode)] >= 0)`.
/// The comparisons are existential (an empty side is false) and numeric; only the sign
/// matters, which exact decimal keeps. The root `TaxTotal` amounts are IBT-110 (`currencyID`
/// IBT-005, empty when IBT-005 is) and IBT-111 (`currencyID` IBT-006); `normalize-space` of an
/// absent `DocumentCurrencyCode` is the empty string.
fn ibr_084(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    let Some(tax_currency) = text(&inv.tax_currency) else {
        return;
    };
    let currency = text(&inv.currency).unwrap_or("");
    let amounts = [
        doc.vat_amount.value.map(|v| (currency, v)),
        doc.totals
            .tax_amount_accounting_currency
            .value
            .map(|v| (tax_currency, v)),
    ];
    let any = |code: &str, sign: fn(&Decimal) -> bool| {
        amounts
            .iter()
            .flatten()
            .any(|(id, v)| eq_normalized(id, code) && sign(v))
    };
    let not_above: fn(&Decimal) -> bool = |v| *v <= Decimal::ZERO;
    let not_below: fn(&Decimal) -> bool = |v| *v >= Decimal::ZERO;
    let same_sign = (any(tax_currency, not_above) && any(currency, not_above))
        || (any(tax_currency, not_below) && any(currency, not_below));
    if !same_sign {
        sink.fail(&[]);
    }
}

/// `ibr-091`, context `cac:LegalMonetaryTotal`, test
/// `string-length(substring-after(cbc:PayableAmount, '.')) <= 2`.
fn ibr_091(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    at_most_two_decimals(sink, &doc.totals.payable_amount);
}

/// `ibr-114-ae`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `not(cac:TaxCategory/cbc:ID = "N")`.
fn ibr_114_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    category_n(doc, sink, true);
}

/// `ibr-115-ae`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()][not(ancestor::cac:Price)]`, test
/// `not(cac:TaxCategory/cbc:ID = "N")`.
fn ibr_115_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    category_n(doc, sink, false);
}

/// The written (trimmed) category code equals `N`; only a document-level allowance or charge
/// has a category.
fn category_n(doc: &Doc<'_>, sink: &mut Sink<'_>, charge: bool) {
    for ac in document(doc, charge) {
        if ac.category_code() == Some("N") {
            sink.fail(&[ac.index]);
        }
    }
}

/// `ibr-121`, context `cac:LegalMonetaryTotal`, test
/// `string-length(substring-after(cbc:AllowanceTotalAmount, '.')) <= 2`.
fn ibr_121(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    at_most_two_decimals(sink, &doc.totals.allowance_total_amount);
}

/// `ibr-122`, context `cac:LegalMonetaryTotal`, test
/// `string-length(substring-after(cbc:ChargeTotalAmount, '.')) <= 2`.
fn ibr_122(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    at_most_two_decimals(sink, &doc.totals.charge_total_amount);
}

/// `ibr-123`, context `cac:LegalMonetaryTotal`, test
/// `string-length(substring-after(cbc:TaxExclusiveAmount, '.')) <= 2`.
fn ibr_123(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    at_most_two_decimals(sink, &doc.totals.tax_exclusive_amount);
}

/// `ibr-125`, context `cac:LegalMonetaryTotal`, test
/// `string-length(substring-after(cbc:TaxInclusiveAmount, '.')) <= 2`.
fn ibr_125(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    at_most_two_decimals(sink, &doc.total_amount);
}

/// The lexical decimals of the written value (`1050.000` has three); an absent or invalid
/// amount is not written, and a written one implies the `LegalMonetaryTotal` context.
fn at_most_two_decimals(sink: &mut Sink<'_>, amount: &Dec<'_>) {
    if amount.exists() && decimal::fraction_digits(amount.raw) > 2 {
        sink.fail(&[]);
    }
}

/// `ibr-131-ae`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()][not(ancestor::cac:Price)]`, test
/// `not(exists(cbc:BaseAmount) and exists(cbc:MultiplierFactorNumeric)) or number(cbc:Amount) = (number(cbc:BaseAmount) * number(cbc:MultiplierFactorNumeric) div 100) or number(cbc:Amount) = round((number(cbc:BaseAmount) * number(cbc:MultiplierFactorNumeric) div 100) * 100) div 100`.
fn ibr_131_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    percentage_amount(doc, sink, false);
}

/// `ibr-146-ae`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `not(exists(cbc:BaseAmount) and exists(cbc:MultiplierFactorNumeric)) or number(cbc:Amount) = (number(cbc:BaseAmount) * number(cbc:MultiplierFactorNumeric) div 100) or number(cbc:Amount) = round((number(cbc:BaseAmount) * number(cbc:MultiplierFactorNumeric) div 100) * 100) div 100`.
fn ibr_146_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    percentage_amount(doc, sink, true);
}

/// With base amount and percentage both written, the amount must equal base x percentage / 100
/// or that value rounded half up to two decimals, in exact decimal (contract upstream defect 5);
/// an absent amount (`number()` is NaN) or an overflow fails. Document and line level (F7).
fn percentage_amount(doc: &Doc<'_>, sink: &mut Sink<'_>, charge: bool) {
    for ac in every(doc, charge) {
        let d = ac.dec;
        let (Some(base), Some(percentage)) = (d.base_amount.value, d.percentage.value) else {
            continue;
        };
        let holds = match (d.amount.value, percent_of(base, percentage)) {
            (Some(amount), Some(p)) => amount == p || amount == decimal::xpath_round2(p),
            _ => false,
        };
        if !holds {
            ac.fail(sink, Field::Amount);
        }
    }
}

/// `base x percentage / 100`: the product first, or `base / 100` first when the product
/// exceeds the 96-bit range; `None` when both overflow.
fn percent_of(base: Decimal, percentage: Decimal) -> Option<Decimal> {
    let hundred = Decimal::ONE_HUNDRED;
    decimal::mul(base, percentage)
        .and_then(|p| p.checked_div(hundred))
        .or_else(|| {
            base.checked_div(hundred)
                .and_then(|b| decimal::mul(b, percentage))
        })
}

/// `ibr-153-ae`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `not(cbc:TaxCurrencyCode = "AED" and cbc:DocumentCurrencyCode != "AED" and (not(cac:TaxExchangeRate/cbc:SourceCurrencyCode = cbc:DocumentCurrencyCode) or not(cac:TaxExchangeRate/cbc:TargetCurrencyCode = cbc:TaxCurrencyCode) or not(cac:TaxExchangeRate/cbc:CalculationRate)))`.
/// `TaxExchangeRate` is written only with BTAE-04, and then from IBT-005 to IBT-006, so the
/// rule fails exactly when IBT-006 is AED, IBT-005 is written and is not (`!=` on an absent
/// node is false), and BTAE-04 is absent or invalid.
fn ibr_153_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    if text(&inv.tax_currency) == Some(AED)
        && text(&inv.currency).is_some_and(|c| c != AED)
        && !doc.exchange_rate.exists()
    {
        sink.fail(&[]);
    }
}

/// `ibr-168-ae`, context `cac:AllowanceCharge[cbc:ChargeIndicator = false()][not(ancestor::cac:Price)]`, test
/// `not(cac:TaxCategory/cbc:ID = "E" and not(exists(cbc:AllowanceChargeReasonCode)))`.
fn ibr_168_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    exempt_without_reason_code(doc, sink, false);
}

/// `ibr-169-ae`, context `cac:AllowanceCharge[cbc:ChargeIndicator = true()]`, test
/// `not(cac:TaxCategory/cbc:ID = "E" and not(exists(cbc:AllowanceChargeReasonCode)))`.
fn ibr_169_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    exempt_without_reason_code(doc, sink, true);
}

/// Category `E` without the allowance's (charge's) own reason code: the XPath tests
/// `cbc:AllowanceChargeReasonCode`, not the exemption reason code the message names (contract
/// rule 13).
fn exempt_without_reason_code(doc: &Doc<'_>, sink: &mut Sink<'_>, charge: bool) {
    for ac in document(doc, charge) {
        if ac.category_code() == Some("E") && text(&ac.model.reason_code).is_none() {
            sink.fail(&[ac.index]);
        }
    }
}

/// `ibr-175-ae`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `cbc:DocumentCurrencyCode = "AED" or (cbc:DocumentCurrencyCode != "AED" and cbc:TaxCurrencyCode = "AED" and exists(cac:TaxTotal/cbc:TaxAmount[@currencyID = "AED"]) and exists(cac:AdditionalDocumentReference[cbc:DocumentTypeCode = "aedtotal-incl-vat"]/cbc:DocumentDescription))`.
/// One finding, at the first missing piece (F7): IBT-005 (absent fails both `=` and `!=`),
/// IBT-006 = AED, IBT-111 (the only root `TaxTotal` amount that can be in AED while IBT-005 is
/// not), BTAE-20 (the only `aedtotal-incl-vat` reference, always with its description).
fn ibr_175_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    let currency = text(&inv.currency);
    if currency == Some(AED) {
        return;
    }
    if currency.is_none() {
        sink.fail_at("currency").term("IBT-005");
    } else if text(&inv.tax_currency) != Some(AED) {
        sink.fail_at("tax_currency").term("IBT-006");
    } else if !doc.totals.tax_amount_accounting_currency.exists() {
        sink.fail_at("totals.tax_amount_accounting_currency")
            .term("IBT-111");
    } else if !doc.totals.total_with_tax_aed.exists() {
        sink.fail(&[]);
    }
}

/// `ibr-co-10`, context `cac:LegalMonetaryTotal`, test
/// `(xs:decimal(cbc:LineExtensionAmount) = (round(sum(//(cac:InvoiceLine|cac:CreditNoteLine)/xs:decimal(cbc:LineExtensionAmount)) * 10 * 10) div 100))`.
/// The sum covers every written IBT-131 (a line without one adds nothing); the suggestion
/// needs every line's.
fn ibr_co_10(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if !lmt_written(doc) {
        return;
    }
    let expected = sum_cents(doc.lines.iter().filter_map(|l| l.net_amount.value))
        .and_then(Cents::round_half_up);
    if equals(&doc.totals.line_extension_amount, expected) {
        return;
    }
    let f = sink.fail(&[]);
    if doc.lines.iter().all(|l| l.net_amount.exists()) {
        suggest(f, expected);
    }
}

/// `ibr-co-11`, context `cac:LegalMonetaryTotal`, test
/// `xs:decimal(cbc:AllowanceTotalAmount) = (round(sum(../cac:AllowanceCharge[cbc:ChargeIndicator=false()]/xs:decimal(cbc:Amount)) * 10 * 10) div 100) or  (not(cbc:AllowanceTotalAmount) and not(../cac:AllowanceCharge[cbc:ChargeIndicator=false()]))`.
fn ibr_co_11(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    document_total(doc, sink, false, &doc.totals.allowance_total_amount);
}

/// `ibr-co-12`, context `cac:LegalMonetaryTotal`, test
/// `xs:decimal(cbc:ChargeTotalAmount) = (round(sum(../cac:AllowanceCharge[cbc:ChargeIndicator=true()]/xs:decimal(cbc:Amount)) * 10 * 10) div 100) or (not(cbc:ChargeTotalAmount) and not(../cac:AllowanceCharge[cbc:ChargeIndicator=true()]))`.
fn ibr_co_12(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    document_total(doc, sink, true, &doc.totals.charge_total_amount);
}

/// IBT-107 (IBT-108) equals the rounded sum of the document-level allowance (charge) amounts, or
/// is absent together with every such allowance (charge). Suggested only when there is one and
/// every amount is written: without any, zero and absence both pass.
fn document_total(doc: &Doc<'_>, sink: &mut Sink<'_>, charge: bool, total: &Dec<'_>) {
    if !lmt_written(doc) {
        return;
    }
    let expected = sum_cents(document(doc, charge).filter_map(|ac| ac.dec.amount.value))
        .and_then(Cents::round_half_up);
    let holds = if total.exists() {
        equals(total, expected)
    } else {
        document(doc, charge).next().is_none()
    };
    if holds {
        return;
    }
    let f = sink.fail(&[]);
    let mut acs = document(doc, charge).peekable();
    if acs.peek().is_some() && acs.all(|ac| ac.dec.amount.exists()) {
        suggest(f, expected);
    }
}

/// `ibr-co-13`, context `cac:LegalMonetaryTotal`, test
/// `../cac:TaxTotal/cbc:TaxIncludedIndicator = true() or (((cbc:ChargeTotalAmount) and (cbc:AllowanceTotalAmount) and (xs:decimal(cbc:TaxExclusiveAmount) = round((xs:decimal(cbc:LineExtensionAmount) + xs:decimal(cbc:ChargeTotalAmount) - xs:decimal(cbc:AllowanceTotalAmount)) * 10 * 10) div 100 ))  or (not(cbc:ChargeTotalAmount) and (cbc:AllowanceTotalAmount) and (xs:decimal(cbc:TaxExclusiveAmount) = round((xs:decimal(cbc:LineExtensionAmount) - xs:decimal(cbc:AllowanceTotalAmount)) * 10 * 10 ) div 100)) or ((cbc:ChargeTotalAmount) and not(cbc:AllowanceTotalAmount) and (xs:decimal(cbc:TaxExclusiveAmount) = round((xs:decimal(cbc:LineExtensionAmount) + xs:decimal(cbc:ChargeTotalAmount)) * 10 * 10 ) div 100)) or (not(cbc:ChargeTotalAmount) and not(cbc:AllowanceTotalAmount) and (xs:decimal(cbc:TaxExclusiveAmount) = xs:decimal(cbc:LineExtensionAmount))))`.
/// The suggestion of the last branch is IBT-106 itself, given only with at most two decimals
/// (`ibr-123`).
fn ibr_co_13(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if !lmt_written(doc) || tax_included(doc) {
        return;
    }
    let expected = expected_tax_exclusive(&doc.totals);
    if equals(&doc.totals.tax_exclusive_amount, expected) {
        return;
    }
    suggest(sink.fail(&[]), expected);
}

/// IBT-109 by `ibr-co-13`'s branches (which of IBT-108 and IBT-107 are written, a bare node
/// being "exists"), from the current IBT-106, IBT-107 and IBT-108.
fn expected_tax_exclusive(t: &TotalsDec<'_>) -> Option<Cents> {
    let lines = Cents::of(t.line_extension_amount.value?)?;
    let charges = t.charge_total_amount.value.map(Cents::of);
    let allowances = t.allowance_total_amount.value.map(Cents::of);
    match (charges, allowances) {
        (Some(c), Some(a)) => lines.add(c?)?.sub(a?)?.round_half_up(),
        (None, Some(a)) => lines.sub(a?)?.round_half_up(),
        (Some(c), None) => lines.add(c?)?.round_half_up(),
        (None, None) => Some(lines),
    }
}

/// `ibr-co-15`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `cac:TaxTotal/cbc:TaxIncludedIndicator = true() or ((cac:LegalMonetaryTotal/xs:decimal(cbc:TaxInclusiveAmount) = round( (cac:LegalMonetaryTotal/xs:decimal(cbc:TaxExclusiveAmount) + cac:TaxTotal[1]/xs:decimal(cbc:TaxAmount[@currencyID=/*/cbc:DocumentCurrencyCode])) * 10 * 10) div 100))`.
fn ibr_co_15(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if tax_included(doc) {
        return;
    }
    let expected = doc
        .totals
        .tax_exclusive_amount
        .value
        .and_then(|tax_exclusive| {
            let vat = first_tax_total_in_document_currency(doc)?;
            Cents::of(tax_exclusive)?
                .add(Cents::of(vat)?)?
                .round_half_up()
        });
    if equals(&doc.total_amount, expected) {
        return;
    }
    suggest(sink.fail(&[]), expected);
}

/// `cac:TaxTotal[1]/xs:decimal(cbc:TaxAmount[@currencyID=/*/cbc:DocumentCurrencyCode])`: the
/// first root `TaxTotal` is the document-currency one when IBT-110 is written, else the
/// accounting one; its amount counts when its `currencyID` equals the written IBT-005 (IBT-110's
/// always does, IBT-111's when IBT-006 equals IBT-005).
fn first_tax_total_in_document_currency(doc: &Doc<'_>) -> Option<Decimal> {
    let currency = text(&doc.inv.currency)?;
    if doc.vat_amount.exists() {
        return doc.vat_amount.value;
    }
    let accounting = doc.totals.tax_amount_accounting_currency.value?;
    (text(&doc.inv.tax_currency) == Some(currency)).then_some(accounting)
}

/// `ibr-co-16`, context `cac:LegalMonetaryTotal`, test
/// `(xs:decimal(cbc:PrepaidAmount) and not(xs:decimal(cbc:PayableRoundingAmount)) and (xs:decimal(cbc:PayableAmount) = (round((xs:decimal(cbc:TaxInclusiveAmount) - xs:decimal(cbc:PrepaidAmount)) * 10 * 10) div 100))) or (not(xs:decimal(cbc:PrepaidAmount)) and not(xs:decimal(cbc:PayableRoundingAmount)) and xs:decimal(cbc:PayableAmount) = xs:decimal(cbc:TaxInclusiveAmount)) or (xs:decimal(cbc:PrepaidAmount) and xs:decimal(cbc:PayableRoundingAmount) and ((round((xs:decimal(cbc:PayableAmount) - xs:decimal(cbc:PayableRoundingAmount)) * 10 * 10) div 100) = (round((xs:decimal(cbc:TaxInclusiveAmount) - xs:decimal(cbc:PrepaidAmount)) * 10 * 10) div 100))) or (not(xs:decimal(cbc:PrepaidAmount)) and xs:decimal(cbc:PayableRoundingAmount) and ((round((xs:decimal(cbc:PayableAmount) - xs:decimal(cbc:PayableRoundingAmount)) * 10 * 10) div 100) = xs:decimal(cbc:TaxInclusiveAmount)))`.
fn ibr_co_16(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if !lmt_written(doc) {
        return;
    }
    let t = &doc.totals;
    let (tax_inclusive, paid, rounding) = (
        doc.total_amount.value,
        t.paid_amount.value,
        t.rounding_amount.value,
    );
    if co16_holds(t.payable_amount.value, tax_inclusive, paid, rounding) {
        return;
    }
    let f = sink.fail(&[]);
    if let Some(c) = co16_candidate(tax_inclusive, paid, rounding).and_then(Cents::to_decimal)
        && !breaks_due_date_rule(doc, c)
    {
        f.suggest(c);
    }
}

/// The `ibr-co-16` test for the amount due `payable`, from IBT-112, IBT-113 and IBT-114. A paid
/// or rounding amount takes part only when non-zero (`xs:decimal(x)` as a boolean,
/// `decimal::ebv`).
fn co16_holds(
    payable: Option<Decimal>,
    tax_inclusive: Option<Decimal>,
    paid: Option<Decimal>,
    rounding: Option<Decimal>,
) -> bool {
    let c = |x: Option<Decimal>| x.and_then(Cents::of);
    let rounded_difference = |a: Option<Decimal>, b: Option<Decimal>| -> Option<Cents> {
        c(a)?.sub(c(b)?)?.round_half_up()
    };
    let same = |x: Option<Cents>, y: Option<Cents>| matches!((x, y), (Some(x), Some(y)) if x == y);
    match (decimal::ebv(paid), decimal::ebv(rounding)) {
        (true, false) => same(c(payable), rounded_difference(tax_inclusive, paid)),
        (false, false) => same(c(payable), c(tax_inclusive)),
        (true, true) => same(
            rounded_difference(payable, rounding),
            rounded_difference(tax_inclusive, paid),
        ),
        (false, true) => same(rounded_difference(payable, rounding), c(tax_inclusive)),
    }
}

/// The one two-decimal amount due that passes `ibr-co-16`. IBT-115, less a non-zero rounding
/// amount, must round to (or, without one, equal) the target `t`: `round(IBT-112 - IBT-113)`
/// with a non-zero paid amount, else IBT-112 itself, which then needs at most two decimals. With
/// a non-zero rounding amount `r` the two-decimal values `y` with `round(y - r) = t` are those in
/// `[t + r - 0.005, t + r + 0.005)`: exactly one, `t + r` rounded half down. The candidate is
/// checked against the test before it is returned.
fn co16_candidate(
    tax_inclusive: Option<Decimal>,
    paid: Option<Decimal>,
    rounding: Option<Decimal>,
) -> Option<Cents> {
    let c = |x: Option<Decimal>| x.and_then(Cents::of);
    let target = if decimal::ebv(paid) {
        c(tax_inclusive)?.sub(c(paid)?)?.round_half_up()?
    } else {
        c(tax_inclusive)?
    };
    if target.sub != 0 {
        return None;
    }
    let candidate = if decimal::ebv(rounding) {
        target.add(c(rounding)?)?.round_half_down()?
    } else {
        target
    };
    co16_holds(candidate.to_decimal(), tax_inclusive, paid, rounding).then_some(candidate)
}

/// `ibr-127-ae` (header, `rules::header::ibr_127_ae`) fails for an invoice outside deemed supply
/// (BTAE-02 position 2) with IBT-115 above zero and no due date (IBT-009). True when amount due
/// `candidate` would newly make it fail, i.e. when the current IBT-115 is not above zero
/// (protocol step 8).
fn breaks_due_date_rule(doc: &Doc<'_>, candidate: Decimal) -> bool {
    candidate > Decimal::ZERO
        && doc.kind == DocKind::Invoice
        && !doc.txn(2)
        && text(&doc.inv.payment_due_date).is_none()
        && !doc
            .totals
            .payable_amount
            .value
            .is_some_and(|v| v > Decimal::ZERO)
}

#[cfg(test)]
mod tests {
    use std::collections::{BTreeMap, BTreeSet};
    use std::str::FromStr;
    use std::sync::OnceLock;

    use serde_json::{Map, Value, json};

    use super::*;
    use crate::catalog::{Family, Fix, Status};
    use crate::conformance::{apply_patch, examples};
    use crate::export::testing::{export_str, maximal};
    use crate::ruleset::default_ruleset;

    const STI: &str = "standard-tax-invoice";
    const MAN: &str = "standard-invoice-mandatory-fields";
    const EXT: &str = "standard-invoice-extensive";
    const CON: &str = "continuous-supplies";
    const ECO: &str = "supply-through-e-commerce";
    const EXP: &str = "exports";
    const CN: &str = "standard-tax-credit-note";

    /// The totals chain: each `fix = value` rule and the only rules its suggestion may newly
    /// break, which then suggest from the new value (protocol step 8).
    const CHAIN: &[(&str, &[&str])] = &[
        ("ibr-co-10", &["ibr-co-13"]),
        ("ibr-co-11", &["ibr-co-13"]),
        ("ibr-co-12", &["ibr-co-13"]),
        ("ibr-co-13", &["ibr-co-15"]),
        ("ibr-co-15", &["ibr-co-16"]),
        ("ibr-co-16", &[]),
    ];

    const STRUCTURAL: [&str; 4] = ["ibr-082", "ibr-sr-30", "ibr-sr-31", "ibr-sr-61"];

    fn downstream(rule: &str) -> &'static [&'static str] {
        CHAIN
            .iter()
            .find(|(id, _)| *id == rule)
            .map_or(&[], |(_, next)| *next)
    }

    fn d(s: &str) -> Decimal {
        Decimal::from_str(s).unwrap()
    }

    fn bases() -> &'static [(String, pb::Invoice)] {
        static BASES: OnceLock<Vec<(String, pb::Invoice)>> = OnceLock::new();
        BASES.get_or_init(examples)
    }

    fn example(slug: &str) -> pb::Invoice {
        bases()
            .iter()
            .find(|(s, _)| s == slug)
            .unwrap_or_else(|| panic!("no example {slug}"))
            .1
            .clone()
    }

    fn patch(inv: &mut pb::Invoice, set: &Value, remove: &[&str]) {
        let set = set.as_object().expect("an object").clone();
        let remove: Vec<String> = remove.iter().map(|s| s.to_string()).collect();
        apply_patch(inv, &set, &remove).expect("patch applies");
    }

    fn patched(slug: &str, set: Value) -> pb::Invoice {
        let mut inv = example(slug);
        patch(&mut inv, &set, &[]);
        inv
    }

    /// `inv` with the string field `path` set to `value`.
    fn with(inv: &pb::Invoice, path: &str, value: &str) -> pb::Invoice {
        let mut set = Map::new();
        set.insert(path.to_string(), Value::String(value.to_string()));
        let mut out = inv.clone();
        apply_patch(&mut out, &set, &[]).expect("patch applies");
        out
    }

    /// Rule id -> number of issues, over every registered rule.
    fn counts(inv: &pb::Invoice) -> BTreeMap<String, usize> {
        let mut out = BTreeMap::new();
        for issue in default_ruleset().validate(inv).issues {
            *out.entry(issue.rule_id).or_insert(0) += 1;
        }
        out
    }

    fn is_totals(id: &str) -> bool {
        default_ruleset()
            .catalog()
            .get(id)
            .is_some_and(|e| e.family == Family::Totals)
    }

    /// The totals rule ids of `inv`, sorted, with multiplicity.
    fn totals_ids(inv: &pb::Invoice) -> Vec<String> {
        let mut out: Vec<String> = default_ruleset()
            .validate(inv)
            .issues
            .into_iter()
            .map(|i| i.rule_id)
            .filter(|id| is_totals(id))
            .collect();
        out.sort_unstable();
        out
    }

    /// `(path, business term, suggested value)` of every issue of `rule`.
    fn findings(inv: &pb::Invoice, rule: &str) -> Vec<(String, String, String)> {
        default_ruleset()
            .validate(inv)
            .issues
            .into_iter()
            .filter(|i| i.rule_id == rule)
            .map(|i| (i.path, i.business_term, i.suggested_value))
            .collect()
    }

    fn found(path: &str, term: &str, value: &str) -> (String, String, String) {
        (path.to_string(), term.to_string(), value.to_string())
    }

    /// The suggested value of the one issue of `rule` (empty for none).
    fn suggestion(inv: &pb::Invoice, rule: &str) -> String {
        let all = findings(inv, rule);
        assert_eq!(all.len(), 1, "{rule}: {all:?}");
        all[0].2.clone()
    }

    /// `(rule id, path, value)` of every totals issue with a suggestion.
    fn suggestions(inv: &pb::Invoice) -> Vec<(String, String, String)> {
        default_ruleset()
            .validate(inv)
            .issues
            .into_iter()
            .filter(|i| is_totals(&i.rule_id) && !i.suggested_value.is_empty())
            .map(|i| (i.rule_id, i.path, i.suggested_value))
            .collect()
    }

    /// Applies the suggestion of `rule`'s one issue alone and checks protocol step 8: `rule`
    /// then passes, the value is a two-decimal string the contract accepts, and every rule
    /// that fails more often than before is downstream of `rule` in the chain.
    fn apply_suggestion(inv: &pb::Invoice, rule: &str) -> pb::Invoice {
        let before = counts(inv);
        let all = findings(inv, rule);
        assert_eq!(all.len(), 1, "{rule}: {all:?}");
        let (path, _, value) = &all[0];
        assert!(!value.is_empty(), "{rule} suggests nothing");
        assert_two_decimal_string(value);
        let fixed = with(inv, path, value);
        let after = counts(&fixed);
        assert!(
            !after.contains_key(rule),
            "{rule} still fails with its own suggestion {path} = {value}"
        );
        for (id, n) in &after {
            if *n > before.get(id).copied().unwrap_or(0) {
                assert!(
                    downstream(rule).contains(&id.as_str()),
                    "{rule}'s suggestion {path} = {value} newly breaks {id}"
                );
            }
        }
        fixed
    }

    /// At most two decimals, at most 28 significant digits, never a negative zero.
    fn assert_two_decimal_string(value: &str) {
        assert!(decimal::parse(value).is_ok(), "{value}");
        assert!(decimal::fraction_digits(value) <= 2, "{value}");
        assert!(
            !(value.starts_with('-') && decimal::parse(value).unwrap().is_zero()),
            "{value}"
        );
    }

    /// SplitMix64, so every property run sees the same cases.
    struct Rng(u64);

    impl Rng {
        fn next(&mut self) -> u64 {
            self.0 = self.0.wrapping_add(0x9E37_79B9_7F4A_7C15);
            let mut z = self.0;
            z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
            z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
            z ^ (z >> 31)
        }

        fn below(&mut self, n: u64) -> u64 {
            self.next() % n
        }

        fn pick<'x, T>(&mut self, xs: &'x [T]) -> &'x T {
            &xs[usize::try_from(self.below(xs.len() as u64)).unwrap()]
        }

        /// A decimal string below `int_max` with 0 to `max_dec` decimals, negative one time in
        /// eight, and its value in units of `10^-max_dec`.
        fn amount(&mut self, int_max: u64, max_dec: u32) -> (String, i128) {
            let int = self.below(int_max);
            let dec = u32::try_from(self.below(u64::from(max_dec) + 1)).unwrap();
            let frac = self.below(10u64.pow(dec));
            let negative = self.below(8) == 0;
            let mut s = if negative {
                "-".to_string()
            } else {
                String::new()
            };
            s.push_str(&int.to_string());
            if dec > 0 {
                s.push_str(&format!(".{frac:0width$}", width = dec as usize));
            }
            let units = i128::from(int) * 10i128.pow(max_dec)
                + i128::from(frac) * 10i128.pow(max_dec - dec);
            (s, if negative { -units } else { units })
        }

        fn decimal(&mut self, int_max: u64, max_dec: u32) -> Decimal {
            d(&self.amount(int_max, max_dec).0)
        }
    }

    /// Whole cents as a two-decimal string (`-5` cents is `-0.05`).
    fn format_cents(cents: i128) -> String {
        let sign = if cents < 0 { "-" } else { "" };
        let abs = cents.abs();
        format!("{sign}{}.{:02}", abs / 100, abs % 100)
    }

    // ------------------------------------------------------------------ the coverage list

    #[test]
    fn the_registered_rules_are_exactly_the_implemented_rows() {
        let mut registered: Vec<&str> = RULES.iter().map(|r| r.id).collect();
        registered.sort_unstable();
        let mut rows: Vec<&str> = default_ruleset()
            .catalog()
            .entries()
            .iter()
            .filter(|e| e.family == Family::Totals && e.status == Status::Implemented)
            .map(|e| e.rule_id)
            .collect();
        rows.sort_unstable();
        assert_eq!(registered, rows);
        assert_eq!(registered.len(), 37);
    }

    #[test]
    fn every_row_is_accounted_for() {
        let rows: Vec<_> = default_ruleset()
            .catalog()
            .entries()
            .iter()
            .filter(|e| e.family == Family::Totals)
            .collect();
        assert_eq!(rows.len(), 41);
        let structural: Vec<&str> = rows
            .iter()
            .filter(|e| e.status == Status::Structural)
            .map(|e| e.rule_id)
            .collect();
        assert_eq!(structural, STRUCTURAL);
        for e in &rows {
            assert!(
                matches!(e.status, Status::Implemented | Status::Structural),
                "{}",
                e.rule_id
            );
            if e.status == Status::Structural {
                assert_eq!(e.fix, Fix::None, "{}", e.rule_id);
                assert!(
                    e.note.contains(
                        "test: src/rules/totals.rs::tests::structural_rules_hold_on_every_exported_allowance_charge"
                    ),
                    "{}",
                    e.rule_id
                );
            }
        }
    }

    /// `fix = value` exactly on the chain, each with suggestion tests below.
    #[test]
    fn the_value_rows_are_the_totals_chain() {
        let value: Vec<&str> = default_ruleset()
            .catalog()
            .entries()
            .iter()
            .filter(|e| e.family == Family::Totals && e.fix == Fix::Value)
            .map(|e| e.rule_id)
            .collect();
        let chain: Vec<&str> = CHAIN.iter().map(|(id, _)| *id).collect();
        assert_eq!(value, chain);
    }

    /// The four `structural` rows cannot fail on the XML the exporter writes: every
    /// `AllowanceCharge` has one `ChargeIndicator`, `true` or `false`, so the bare context of
    /// `ibr-082` is never reached; at most one `AllowanceChargeReason` (`ibr-sr-30`, `ibr-sr-31`);
    /// at most one `TaxExemptionReason` per category (`ibr-sr-61`), and only document-level ones
    /// carry a category.
    #[test]
    fn structural_rules_hold_on_every_exported_allowance_charge() {
        let mut docs: Vec<pb::Invoice> = bases().iter().map(|(_, inv)| inv.clone()).collect();
        docs.push(maximal("380"));
        docs.push(maximal("381"));
        let mut degenerate = example(MAN);
        patch(
            &mut degenerate,
            &json!({
                "allowances_charges[0].is_charge": false,
                "allowances_charges[1].is_charge": true,
                "allowances_charges[2].reason": "R",
                "allowances_charges[2].reason_code": "95",
                "allowances_charges[2].tax_category.exemption_reason_text": "T",
                "allowances_charges[2].tax_category.exemption_reason_code": "VATEX-AE-X",
                "lines[0].allowances_charges[0].is_charge": true,
                "lines[0].allowances_charges[1].reason": "R",
                "lines[0].allowances_charges[1].tax_category.exemption_reason_text": "T",
            }),
            &[],
        );
        docs.push(degenerate);
        let mut seen = 0;
        for inv in &docs {
            let xml = export_str(inv);
            let tree = roxmltree::Document::parse(&xml).expect("well-formed");
            let root = tree.root_element();
            for ac in tree
                .descendants()
                .filter(|n| n.tag_name().name() == "AllowanceCharge")
            {
                seen += 1;
                let kids = |name: &'static str| {
                    ac.children()
                        .filter(move |c| c.tag_name().name() == name)
                        .collect::<Vec<_>>()
                };
                let indicator = kids("ChargeIndicator");
                assert_eq!(indicator.len(), 1);
                assert!(matches!(indicator[0].text(), Some("true" | "false")));
                assert!(kids("AllowanceChargeReason").len() <= 1);
                let categories = kids("TaxCategory");
                if ac.parent() != Some(root) {
                    assert!(categories.is_empty());
                }
                for c in categories {
                    let reasons = c
                        .children()
                        .filter(|k| k.tag_name().name() == "TaxExemptionReason")
                        .count();
                    assert!(reasons <= 1);
                }
            }
        }
        assert!(seen > 100, "only {seen} AllowanceCharge elements");
    }

    /// No official rule outside this family reads the `LegalMonetaryTotal` elements the chain
    /// suggests, except as listed: line net amounts (IBT-131, never IBG-22), the `currencyID`
    /// attribute (unchanged by any suggestion), IBT-113's existence (never suggested), and
    /// IBT-115's sign (`ibr-127-ae`, guarded by `breaks_due_date_rule`). So a chain suggestion
    /// can only break chain rules, whatever is registered today.
    #[test]
    fn only_known_rules_read_the_totals_elements() {
        let upstream = [
            include_str!("../../rulesets/pint-ae-1.0.4/upstream/rules-base.tsv"),
            include_str!("../../rulesets/pint-ae-1.0.4/upstream/rules-ae.tsv"),
        ];
        let names = [
            "LegalMonetaryTotal",
            "LineExtensionAmount",
            "TaxExclusiveAmount",
            "TaxInclusiveAmount",
            "AllowanceTotalAmount",
            "ChargeTotalAmount",
            "PrepaidAmount",
            "PayableRoundingAmount",
            "PayableAmount",
        ];
        let readers: BTreeSet<&str> = upstream
            .iter()
            .flat_map(|file| file.lines())
            .filter_map(|line| {
                let cols: Vec<&str> = line.split('\t').collect();
                let read = format!("{} {}", cols[2], cols[3]);
                names.iter().any(|n| read.contains(n)).then_some(cols[0])
            })
            .collect();
        let ours = [
            "ibr-012",
            "ibr-013",
            "ibr-014",
            "ibr-015",
            "ibr-091",
            "ibr-121",
            "ibr-122",
            "ibr-123",
            "ibr-125",
            "ibr-co-10",
            "ibr-co-11",
            "ibr-co-12",
            "ibr-co-13",
            "ibr-co-15",
            "ibr-co-16",
        ];
        let line_net_amount_only = [
            "aligned-ibrp-ae-08-ae",
            "aligned-ibrp-e-08",
            "aligned-ibrp-o-08",
            "aligned-ibrp-s-08",
            "aligned-ibrp-z-08",
            "ibr-024",
            "ibr-102-ae",
            "ibr-147-ae",
        ];
        let currency_attribute_only = ["ibr-126", "ibr-cl-03"];
        let prepaid_existence_only = ["ibr-093"];
        let payable_sign = ["ibr-127-ae"];
        let known: BTreeSet<&str> = ours
            .into_iter()
            .chain(line_net_amount_only)
            .chain(currency_attribute_only)
            .chain(prepaid_existence_only)
            .chain(payable_sign)
            .collect();
        assert_eq!(readers, known);
        for id in ours {
            assert!(is_totals(id), "{id}");
        }
    }

    // ------------------------------------------------------------------ official behaviour

    /// `(name, base, set as JSON, remove, totals rule ids)`.
    type Probe = (
        &'static str,
        &'static str,
        &'static str,
        &'static [&'static str],
        &'static [&'static str],
    );

    /// Edge cases run through the official schematron (SaxonC-HE 13.0, both compiled
    /// stylesheets) on the exported XML, 2026-10-04: `(name, base, set, remove, the totals
    /// rule ids Saxon reported)`.
    const PROBES: &[Probe] = &[
        (
            "co10-half-up-pass",
            "standard-invoice-extensive",
            r#"{"lines[4].net_amount": "1975.005", "totals.line_extension_amount": "18307.51"}"#,
            &[],
            &["ibr-co-13"],
        ),
        (
            "co10-neg-half",
            "standard-invoice-mandatory-fields",
            r#"{"lines[0].net_amount": "-1000.005", "totals.line_extension_amount": "-1000.00"}"#,
            &[],
            &["ibr-co-13"],
        ),
        (
            "co10-neg-half-b",
            "standard-invoice-mandatory-fields",
            r#"{"lines[0].net_amount": "-1000.005", "totals.line_extension_amount": "-1000.01"}"#,
            &[],
            &["ibr-co-10", "ibr-co-13"],
        ),
        (
            "co10-lea-3dec",
            "standard-invoice-mandatory-fields",
            r#"{"totals.line_extension_amount": "1000.000"}"#,
            &[],
            &[],
        ),
        (
            "co10-no-lines-net",
            "standard-invoice-mandatory-fields",
            r#"{"lines[0].net_amount": "", "totals.line_extension_amount": "0"}"#,
            &[],
            &["ibr-co-13"],
        ),
        (
            "co10-zero",
            "standard-invoice-mandatory-fields",
            r#"{"lines[0].net_amount": "0", "totals.line_extension_amount": "0.00"}"#,
            &[],
            &["ibr-co-13"],
        ),
        (
            "co11-zero-no-ac",
            "standard-invoice-mandatory-fields",
            r#"{"totals.allowance_total_amount": "0.00"}"#,
            &[],
            &[],
        ),
        (
            "co11-neg-zero",
            "standard-invoice-mandatory-fields",
            r#"{"totals.allowance_total_amount": "-0"}"#,
            &[],
            &[],
        ),
        (
            "co12-zero-no-ac",
            "standard-invoice-mandatory-fields",
            r#"{"totals.charge_total_amount": "0"}"#,
            &[],
            &[],
        ),
        (
            "co13-incl",
            "standard-tax-invoice",
            r#"{"totals.tax_exclusive_amount": "1", "totals.tax_inclusive_pricing": true}"#,
            &[],
            &[],
        ),
        (
            "co13-incl-novat",
            "standard-tax-invoice",
            r#"{"totals.tax_exclusive_amount": "1", "totals.tax_inclusive_pricing": true, "vat_amount": ""}"#,
            &[],
            &["ibr-co-13", "ibr-co-15"],
        ),
        (
            "co13-cht-zero",
            "standard-invoice-mandatory-fields",
            r#"{"totals.charge_total_amount": "0", "totals.tax_exclusive_amount": "1000.00"}"#,
            &[],
            &[],
        ),
        (
            "co13-lea-3dec",
            "standard-invoice-mandatory-fields",
            r#"{"totals.line_extension_amount": "1000.004", "totals.tax_exclusive_amount": "1000.004"}"#,
            &[],
            &["ibr-123", "ibr-co-10"],
        ),
        (
            "co13-lea-3dec-b",
            "standard-invoice-mandatory-fields",
            r#"{"totals.line_extension_amount": "1000.004", "totals.tax_exclusive_amount": "1000.00"}"#,
            &[],
            &["ibr-co-10", "ibr-co-13"],
        ),
        (
            "co13-round-half",
            "standard-invoice-mandatory-fields",
            r#"{"totals.charge_total_amount": "0.005", "totals.tax_exclusive_amount": "1000.01"}"#,
            &[],
            &["ibr-122", "ibr-co-12", "ibr-co-15"],
        ),
        (
            "co13-round-half-neg",
            "standard-invoice-mandatory-fields",
            r#"{"totals.allowance_total_amount": "0.005", "totals.tax_exclusive_amount": "1000.00"}"#,
            &[],
            &["ibr-121", "ibr-co-11"],
        ),
        (
            "co15-incl",
            "standard-tax-invoice",
            r#"{"total_amount": "1", "totals.tax_inclusive_pricing": true}"#,
            &[],
            &["ibr-co-16"],
        ),
        (
            "co15-no-vat",
            "standard-invoice-mandatory-fields",
            r#"{"vat_amount": ""}"#,
            &[],
            &["ibr-co-15"],
        ),
        (
            "co15-no-vat-acc-same",
            "standard-invoice-mandatory-fields",
            r#"{"vat_amount": "", "tax_currency": "AED", "totals.tax_amount_accounting_currency": "50"}"#,
            &[],
            &[],
        ),
        (
            "co15-no-vat-acc-diff",
            "standard-invoice-mandatory-fields",
            r#"{"vat_amount": "", "tax_currency": "USD", "totals.tax_amount_accounting_currency": "50"}"#,
            &[],
            &["ibr-084", "ibr-co-15"],
        ),
        (
            "co15-no-currency",
            "standard-invoice-mandatory-fields",
            r#"{"currency": ""}"#,
            &[],
            &["ibr-175-ae", "ibr-co-15"],
        ),
        (
            "co15-half",
            "standard-invoice-mandatory-fields",
            r#"{"vat_amount": "50.005", "total_amount": "1050.01", "totals.payable_amount": "1050.01"}"#,
            &[],
            &[],
        ),
        (
            "co15-tea-absent",
            "standard-invoice-mandatory-fields",
            r#"{"totals.tax_exclusive_amount": ""}"#,
            &[],
            &["ibr-013", "ibr-co-13", "ibr-co-15"],
        ),
        (
            "co16-prepaid-zero-rounding",
            "supply-through-e-commerce",
            r#"{"totals.rounding_amount": "0.01", "totals.payable_amount": "105.01"}"#,
            &[],
            &[],
        ),
        (
            "co16-b3-3dec-rounding",
            "standard-invoice-extensive",
            r#"{"totals.rounding_amount": "0.305", "totals.payable_amount": "5777.50"}"#,
            &[],
            &[],
        ),
        (
            "co16-b3-3dec-rounding-b",
            "standard-invoice-extensive",
            r#"{"totals.rounding_amount": "0.305", "totals.payable_amount": "5777.51"}"#,
            &[],
            &["ibr-co-16"],
        ),
        (
            "co16-b3-3dec-rounding-c",
            "standard-invoice-extensive",
            r#"{"totals.rounding_amount": "0.305", "totals.payable_amount": "5777.505"}"#,
            &[],
            &["ibr-091"],
        ),
        (
            "co16-b4-tia-3dec",
            "continuous-supplies",
            r#"{"total_amount": "6056.255", "totals.payable_amount": "6057.005"}"#,
            &[],
            &["ibr-091", "ibr-125", "ibr-co-15", "ibr-co-16"],
        ),
        (
            "co16-pa-3dec-pass",
            "continuous-supplies",
            r#"{"totals.payable_amount": "6057.004"}"#,
            &[],
            &["ibr-091"],
        ),
        (
            "co16-neg-paid",
            "standard-invoice-mandatory-fields",
            r#"{"totals.paid_amount": "-50", "totals.payable_amount": "1100"}"#,
            &[],
            &[],
        ),
        (
            "co16-pa-absent-lmt",
            "standard-invoice-mandatory-fields",
            r#"{"totals.payable_amount": ""}"#,
            &[],
            &["ibr-015", "ibr-co-16"],
        ),
        (
            "091-plus",
            "standard-invoice-mandatory-fields",
            r#"{"totals.payable_amount": "+1050.00"}"#,
            &[],
            &[],
        ),
        (
            "125-trailing",
            "standard-invoice-mandatory-fields",
            r#"{"total_amount": "1050.0000", "totals.payable_amount": "1050.0000"}"#,
            &[],
            &["ibr-091", "ibr-125"],
        ),
        (
            "053-same-currency",
            "standard-invoice-mandatory-fields",
            r#"{"tax_currency": "AED"}"#,
            &[],
            &[],
        ),
        (
            "053-same-currency-novat",
            "standard-invoice-mandatory-fields",
            r#"{"tax_currency": "AED", "vat_amount": ""}"#,
            &[],
            &["ibr-084", "ibr-co-15"],
        ),
        (
            "053-line-aed",
            "standard-invoice-mandatory-fields",
            r#"{"tax_currency": "AED", "vat_amount": "", "lines[0].amount_aed": "1050", "lines[0].vat_amount_aed": "50"}"#,
            &[],
            &["ibr-084", "ibr-co-15"],
        ),
        (
            "084-same-currency",
            "standard-invoice-mandatory-fields",
            r#"{"tax_currency": "AED", "totals.tax_amount_accounting_currency": "-50"}"#,
            &[],
            &[],
        ),
        (
            "084-zero",
            "standard-tax-invoice",
            r#"{"tax_currency": "USD", "totals.tax_amount_accounting_currency": "0"}"#,
            &[],
            &[],
        ),
        (
            "084-neg-both",
            "standard-invoice-mandatory-fields",
            r#"{"tax_currency": "USD", "totals.tax_amount_accounting_currency": "-13.61", "vat_amount": "-50"}"#,
            &[],
            &["ibr-co-15"],
        ),
        (
            "084-no-doc-currency",
            "standard-invoice-mandatory-fields",
            r#"{"tax_currency": "USD", "totals.tax_amount_accounting_currency": "13.61", "currency": ""}"#,
            &[],
            &["ibr-175-ae", "ibr-co-15"],
        ),
        (
            "084-spaces",
            "standard-invoice-mandatory-fields",
            r#"{"tax_currency": "U  SD", "totals.tax_amount_accounting_currency": "13.61"}"#,
            &[],
            &["ibr-084"],
        ),
        (
            "153-aed-aed",
            "standard-invoice-mandatory-fields",
            r#"{"tax_currency": "AED", "totals.tax_amount_accounting_currency": "50"}"#,
            &[],
            &[],
        ),
        (
            "153-no-currency",
            "exports",
            r#"{"exchange_rate": "", "currency": ""}"#,
            &[],
            &["ibr-175-ae", "ibr-co-15"],
        ),
        (
            "153-tax-usd",
            "exports",
            r#"{"exchange_rate": "", "tax_currency": "USD"}"#,
            &[],
            &["ibr-175-ae"],
        ),
        (
            "175-no-currency",
            "standard-invoice-mandatory-fields",
            r#"{"currency": ""}"#,
            &[],
            &["ibr-175-ae", "ibr-co-15"],
        ),
        (
            "175-no-acc-but-aed-doc",
            "exports",
            r#"{"totals.tax_amount_accounting_currency": "", "currency": "AED"}"#,
            &[],
            &[],
        ),
        (
            "175-lower",
            "exports",
            r#"{"tax_currency": "aed"}"#,
            &[],
            &["ibr-175-ae"],
        ),
        (
            "131-exact3",
            "standard-tax-invoice",
            r#"{"allowances_charges[0].base_amount": "10486.2", "allowances_charges[0].amount": "262.155"}"#,
            &[],
            &["ibr-co-11"],
        ),
        (
            "131-no-amount-no-base",
            "standard-tax-invoice",
            r#"{"allowances_charges[0].amount": "", "allowances_charges[0].base_amount": ""}"#,
            &[],
            &["aligned-ibrp-057", "ibr-031", "ibr-co-11"],
        ),
        (
            "131-pct-zero",
            "standard-tax-invoice",
            r#"{"lines[0].allowances_charges[0].percentage": "0", "lines[0].allowances_charges[0].amount": "0"}"#,
            &[],
            &[],
        ),
        (
            "146-neg",
            "standard-tax-invoice",
            r#"{"lines[0].allowances_charges[1].base_amount": "-9800", "lines[0].allowances_charges[1].amount": "-980"}"#,
            &[],
            &[],
        ),
        (
            "032-scheme-lower",
            "standard-tax-invoice",
            r#"{"allowances_charges[0].tax_category.tax_scheme": " vat "}"#,
            &[],
            &[],
        ),
        (
            "032-no-category",
            "standard-tax-invoice",
            r#"{"allowances_charges[0].tax_category.code": "", "allowances_charges[0].tax_category.rate": "", "allowances_charges[0].tax_category.tax_scheme": ""}"#,
            &[],
            &["aligned-ibrp-032"],
        ),
        (
            "032-line-noop",
            "standard-tax-invoice",
            r#"{"lines[0].allowances_charges[0].tax_category.code": "S"}"#,
            &[],
            &[],
        ),
        (
            "044-doc-charge-code-only",
            "standard-tax-invoice",
            r#"{"allowances_charges[1].reason": ""}"#,
            &[],
            &[],
        ),
        (
            "114-line",
            "standard-tax-invoice",
            r#"{"lines[0].tax.code": "N"}"#,
            &[],
            &[],
        ),
        (
            "114-spaces",
            "standard-tax-invoice",
            r#"{"allowances_charges[1].tax_category.code": " N "}"#,
            &[],
            &["ibr-114-ae"],
        ),
        (
            "168-code-present",
            "doc-level-allowance-e-category",
            r#"{"allowances_charges[0].reason_code": "VATEX-AE-X"}"#,
            &[],
            &[],
        ),
        (
            "cn-chain",
            "standard-tax-credit-note",
            r#"{"totals.line_extension_amount": "10486.01"}"#,
            &[],
            &["ibr-co-10", "ibr-co-13"],
        ),
        (
            "cn-ac",
            "standard-tax-credit-note",
            r#"{"allowances_charges[0].amount": "", "lines[0].allowances_charges[1].reason": "", "lines[0].allowances_charges[1].reason_code": ""}"#,
            &[],
            &["ibr-031", "ibr-044", "ibr-131-ae", "ibr-co-11"],
        ),
        (
            "ac-added",
            "standard-invoice-mandatory-fields",
            r#"{"allowances_charges[0].amount": "10", "allowances_charges[0].reason": "Discount"}"#,
            &[],
            &["aligned-ibrp-032", "ibr-co-11"],
        ),
        (
            "ac-added-charge",
            "standard-invoice-mandatory-fields",
            r#"{"allowances_charges[0].is_charge": true, "allowances_charges[0].amount": "10", "allowances_charges[0].reason": "Fee"}"#,
            &[],
            &["aligned-ibrp-037", "ibr-co-12"],
        ),
        (
            "line-ac-empty",
            "standard-invoice-mandatory-fields",
            r#"{"lines[0].allowances_charges[0].is_charge": true}"#,
            &[],
            &["ibr-043", "ibr-044"],
        ),
        (
            "lmt-absent",
            "standard-invoice-mandatory-fields",
            r#"{"total_amount": "", "totals.line_extension_amount": "", "totals.tax_exclusive_amount": "", "totals.payable_amount": ""}"#,
            &[],
            &["ibr-co-15"],
        ),
        (
            "big",
            "standard-invoice-mandatory-fields",
            r#"{"lines[0].net_amount": "9999999999999999999999999.99", "totals.line_extension_amount": "9999999999999999999999999.99", "totals.tax_exclusive_amount": "9999999999999999999999999.99", "vat_amount": "50", "total_amount": "10000000000000000000000049.99", "totals.payable_amount": "10000000000000000000000049.99"}"#,
            &[],
            &[],
        ),
        (
            "co10-precision",
            "standard-invoice-mandatory-fields",
            r#"{"lines[0].net_amount": "10000000000000000000000000", "lines[1].id": "2", "lines[1].net_amount": "0.0049999999999999999999999", "totals.line_extension_amount": "10000000000000000000000000.00"}"#,
            &[],
            &["ibr-co-13"],
        ),
        (
            "co10-precision-b",
            "standard-invoice-mandatory-fields",
            r#"{"lines[0].net_amount": "10000000000000000000000000", "lines[1].id": "2", "lines[1].net_amount": "0.0049999999999999999999999", "totals.line_extension_amount": "10000000000000000000000000.01"}"#,
            &[],
            &["ibr-co-10", "ibr-co-13"],
        ),
        (
            "co16-cn-neg",
            "standard-tax-credit-note",
            r#"{"total_amount": "-11175.45", "totals.payable_amount": "-11175.40"}"#,
            &[],
            &["ibr-co-15"],
        ),
        (
            "co16-b3-half",
            "standard-invoice-extensive",
            r#"{"totals.rounding_amount": "0.305", "totals.payable_amount": "5777.50"}"#,
            &[],
            &[],
        ),
        (
            "co11-half",
            "standard-tax-invoice",
            r#"{"allowances_charges[0].amount": "262.155", "totals.allowance_total_amount": "262.16"}"#,
            &[],
            &["ibr-131-ae", "ibr-co-13"],
        ),
        (
            "co13-branch-c",
            "standard-invoice-mandatory-fields",
            r#"{"totals.charge_total_amount": "0.006", "totals.allowance_total_amount": "0.001", "totals.tax_exclusive_amount": "1000.01"}"#,
            &[],
            &["ibr-121", "ibr-122", "ibr-co-11", "ibr-co-12", "ibr-co-15"],
        ),
    ];

    #[test]
    fn probes_confirmed_by_the_official_schematron() {
        let mut problems = Vec::new();
        for (name, slug, set, remove, want) in PROBES {
            let set: Value = serde_json::from_str(set).unwrap();
            let mut inv = example(slug);
            patch(&mut inv, &set, remove);
            let got = totals_ids(&inv);
            if got.iter().map(String::as_str).collect::<Vec<_>>() != *want {
                problems.push(format!(
                    "{name}: got {got:?}, the official run has {want:?}"
                ));
            }
        }
        assert!(problems.is_empty(), "{}", problems.join("\n"));
    }

    /// Contract upstream defect 5: `ibr-131-ae` and `ibr-146-ae` compute in `xs:double`; the
    /// platform computes in exact decimal. `10486.2 x 2.5 / 100` is exactly `262.155`, which
    /// rounds half up to `262.16`; in binary it is `262.15499…`, so Saxon rounds to `262.15`
    /// and rejects an amount of `262.16` (probe run 2026-10-04) that the platform accepts.
    #[test]
    fn allowance_percentages_are_exact_decimal() {
        let amount = |base: &str, amount: &str| {
            patched(
                STI,
                json!({"allowances_charges[0].base_amount": base, "allowances_charges[0].amount": amount}),
            )
        };
        assert!(findings(&amount("10486.2", "262.16"), "ibr-131-ae").is_empty());
        assert!(findings(&amount("10486.2", "262.155"), "ibr-131-ae").is_empty());
        assert_eq!(
            findings(&amount("10486.2", "262.15"), "ibr-131-ae"),
            [found("allowances_charges[0].amount", "IBT-092", "")]
        );
        // 999.375 x 5 / 100 = 49.96875, which rounds to 49.97 (disclosed-agent-billing).
        let disclosed = patched(
            STI,
            json!({
                "allowances_charges[0].base_amount": "999.375",
                "allowances_charges[0].percentage": "5",
                "allowances_charges[0].amount": "49.97",
            }),
        );
        assert!(findings(&disclosed, "ibr-131-ae").is_empty());
        // A zero percentage needs a zero amount; signs follow the operands.
        let zero = patched(
            STI,
            json!({"lines[0].allowances_charges[0].percentage": "0", "lines[0].allowances_charges[0].amount": "0.00"}),
        );
        assert!(findings(&zero, "ibr-131-ae").is_empty());
        let negative = patched(
            STI,
            json!({"lines[0].allowances_charges[1].base_amount": "-9800", "lines[0].allowances_charges[1].amount": "-980"}),
        );
        assert!(findings(&negative, "ibr-146-ae").is_empty());
        // A product beyond the 96-bit range fires the rule rather than panicking, and a
        // product needing the fallback order still compares exactly.
        let huge = patched(
            STI,
            json!({
                "allowances_charges[1].base_amount": "9999999999999999999999999999",
                "allowances_charges[1].percentage": "9999999999999999999999999999",
                "allowances_charges[1].amount": "1",
            }),
        );
        assert_eq!(findings(&huge, "ibr-146-ae").len(), 1);
        let large = patched(
            STI,
            json!({
                "allowances_charges[1].base_amount": "1000000000000000",
                "allowances_charges[1].percentage": "100000000000000",
                "allowances_charges[1].amount": "1000000000000000000000000000",
            }),
        );
        assert!(findings(&large, "ibr-146-ae").is_empty());
    }

    #[test]
    fn allowance_and_charge_findings_name_their_own_field() {
        let inv = patched(
            STI,
            json!({
                "allowances_charges[0].base_amount": "",
                "allowances_charges[1].percentage": "",
                "lines[0].allowances_charges[0].percentage": "",
                "lines[0].allowances_charges[1].base_amount": "",
            }),
        );
        assert_eq!(
            findings(&inv, "aligned-ibrp-057"),
            [
                found("allowances_charges[0].base_amount", "IBT-093", ""),
                found("lines[0].allowances_charges[0].percentage", "IBT-138", ""),
            ]
        );
        assert_eq!(
            findings(&inv, "aligned-ibrp-058"),
            [
                found("allowances_charges[1].percentage", "IBT-101", ""),
                found("lines[0].allowances_charges[1].base_amount", "IBT-142", ""),
            ]
        );
        let reasons = patched(
            STI,
            json!({
                "allowances_charges[1].reason": "",
                "allowances_charges[1].reason_code": "",
                "lines[0].allowances_charges[1].reason": "",
                "lines[0].allowances_charges[1].reason_code": "",
            }),
        );
        assert_eq!(
            findings(&reasons, "ibr-044"),
            [
                found("allowances_charges[1].reason", "IBT-104", ""),
                found("lines[0].allowances_charges[1].reason", "IBT-144", ""),
            ]
        );
        assert_eq!(
            findings(&reasons, "ibr-038"),
            [found("allowances_charges[1].reason", "IBT-104", "")]
        );
        let amounts = patched(
            STI,
            json!({"lines[0].allowances_charges[0].amount": "1", "lines[0].allowances_charges[1].amount": "1"}),
        );
        assert_eq!(
            findings(&amounts, "ibr-131-ae"),
            [found(
                "lines[0].allowances_charges[0].amount",
                "IBT-136",
                ""
            )]
        );
        assert_eq!(
            findings(&amounts, "ibr-146-ae"),
            [found(
                "lines[0].allowances_charges[1].amount",
                "IBT-141",
                ""
            )]
        );
    }

    #[test]
    fn ibr_175_ae_points_at_the_first_missing_piece() {
        for (set, want) in [
            (json!({"currency": ""}), Some(("currency", "IBT-005"))),
            (
                json!({"tax_currency": "EUR"}),
                Some(("tax_currency", "IBT-006")),
            ),
            (
                json!({"totals.tax_amount_accounting_currency": ""}),
                Some(("totals.tax_amount_accounting_currency", "IBT-111")),
            ),
            (
                json!({"totals.total_with_tax_aed": ""}),
                Some(("totals.total_with_tax_aed", "BTAE-20")),
            ),
            (json!({"currency": "AED"}), None),
            (json!({}), None),
        ] {
            let got = findings(&patched(EXP, set.clone()), "ibr-175-ae");
            let want: Vec<_> = want.map(|(p, t)| found(p, t, "")).into_iter().collect();
            assert_eq!(got, want, "{set}");
        }
    }

    #[test]
    fn eq_normalized_is_normalize_space() {
        assert!(eq_normalized("USD", "USD"));
        assert!(eq_normalized("USD", " USD\t"));
        assert!(eq_normalized("U SD", "U \n SD"));
        assert!(!eq_normalized("U  SD", "U  SD"));
        assert!(eq_normalized("", " \t "));
        assert!(eq_normalized("", ""));
        assert!(!eq_normalized("US", "USD"));
        assert!(!eq_normalized("USD", "US"));
        assert!(!eq_normalized(" USD", "USD"));
    }

    // ------------------------------------------------------------------ suggestions

    #[test]
    fn ibr_co_10_suggests_the_rounded_line_sum() {
        // Spec 5.2.8's example fixture.
        let inv = patched(STI, json!({"totals.line_extension_amount": "1.00"}));
        assert_eq!(
            findings(&inv, "ibr-co-10"),
            [found("totals.line_extension_amount", "IBT-106", "10486.00")]
        );
        apply_suggestion(&inv, "ibr-co-10");
        // The line sum 18307.505 rounds half up.
        let half = patched(EXT, json!({"lines[4].net_amount": "1975.005"}));
        assert_eq!(suggestion(&half, "ibr-co-10"), "18307.51");
        apply_suggestion(&half, "ibr-co-10");
        // A negative half rounds toward positive infinity.
        let negative = patched(
            MAN,
            json!({"lines[0].net_amount": "-1000.005", "totals.line_extension_amount": "5"}),
        );
        assert_eq!(suggestion(&negative, "ibr-co-10"), "-1000.00");
        // A line without a (valid) net amount: the rule fires and suggests nothing.
        for net in ["", "1,000"] {
            let missing = patched(
                STI,
                json!({"lines[0].net_amount": net, "totals.line_extension_amount": "1"}),
            );
            assert_eq!(suggestion(&missing, "ibr-co-10"), "", "{net:?}");
        }
        // No LegalMonetaryTotal at all: the context does not exist.
        let none = patched(
            MAN,
            json!({"total_amount": "", "totals.line_extension_amount": "", "totals.tax_exclusive_amount": "", "totals.payable_amount": ""}),
        );
        assert!(findings(&none, "ibr-co-10").is_empty());
    }

    #[test]
    fn ibr_co_11_and_ibr_co_12_suggest_the_rounded_document_level_sums() {
        let wrong = patched(STI, json!({"totals.allowance_total_amount": "262.16"}));
        assert_eq!(
            findings(&wrong, "ibr-co-11"),
            [found("totals.allowance_total_amount", "IBT-107", "262.15")]
        );
        apply_suggestion(&wrong, "ibr-co-11");
        let absent = patched(STI, json!({"totals.allowance_total_amount": ""}));
        assert_eq!(suggestion(&absent, "ibr-co-11"), "262.15");
        apply_suggestion(&absent, "ibr-co-11");
        let half = patched(STI, json!({"allowances_charges[0].amount": "262.155"}));
        assert_eq!(suggestion(&half, "ibr-co-11"), "262.16");
        // Without any allowance both zero and absence pass: no one value, no suggestion.
        let spurious = patched(MAN, json!({"totals.allowance_total_amount": "5"}));
        assert_eq!(suggestion(&spurious, "ibr-co-11"), "");
        // An allowance without an amount: the sum is incomplete, no suggestion.
        let incomplete = patched(STI, json!({"allowances_charges[0].amount": ""}));
        assert_eq!(suggestion(&incomplete, "ibr-co-11"), "");

        let wrong = patched(STI, json!({"totals.charge_total_amount": "419.45"}));
        assert_eq!(
            findings(&wrong, "ibr-co-12"),
            [found("totals.charge_total_amount", "IBT-108", "419.44")]
        );
        apply_suggestion(&wrong, "ibr-co-12");
        let absent = patched(STI, json!({"totals.charge_total_amount": ""}));
        assert_eq!(suggestion(&absent, "ibr-co-12"), "419.44");
        apply_suggestion(&absent, "ibr-co-12");
        let spurious = patched(MAN, json!({"totals.charge_total_amount": "-1"}));
        assert_eq!(suggestion(&spurious, "ibr-co-12"), "");
    }

    #[test]
    fn ibr_co_13_suggests_from_the_current_inputs_in_every_branch() {
        let wrong = patched(STI, json!({"totals.tax_exclusive_amount": "10643.30"}));
        assert_eq!(
            findings(&wrong, "ibr-co-13"),
            [found("totals.tax_exclusive_amount", "IBT-109", "10643.29")]
        );
        apply_suggestion(&wrong, "ibr-co-13");
        // From the current IBT-106, even while IBT-106 itself is wrong (the chain).
        let chained = patched(STI, json!({"totals.line_extension_amount": "1.00"}));
        assert_eq!(suggestion(&chained, "ibr-co-13"), "158.29");
        // Each branch, with halves rounded up.
        for (charges, allowances, want) in [
            ("0.005", "", "1000.01"),
            ("", "0.005", "1000.00"),
            ("0.005", "0.004", "1000.00"),
            ("0.006", "0.001", "1000.01"),
            ("", "", "1000.00"),
        ] {
            let inv = patched(
                MAN,
                json!({
                    "totals.charge_total_amount": charges,
                    "totals.allowance_total_amount": allowances,
                    "totals.tax_exclusive_amount": "1",
                }),
            );
            assert_eq!(
                suggestion(&inv, "ibr-co-13"),
                want,
                "{charges:?} {allowances:?}"
            );
        }
        // Neither total: IBT-109 must equal IBT-106 exactly, which no two-decimal value can.
        let exact = patched(
            MAN,
            json!({"totals.line_extension_amount": "1000.004", "totals.tax_exclusive_amount": "1"}),
        );
        assert_eq!(suggestion(&exact, "ibr-co-13"), "");
        // Without IBT-106 nothing can be computed.
        let missing = patched(MAN, json!({"totals.line_extension_amount": ""}));
        assert_eq!(suggestion(&missing, "ibr-co-13"), "");
        // Tax-inclusive pricing (IBT-200, written with IBT-110) switches the rule off.
        let inclusive = patched(
            STI,
            json!({"totals.tax_exclusive_amount": "1", "totals.tax_inclusive_pricing": true}),
        );
        assert!(findings(&inclusive, "ibr-co-13").is_empty());
    }

    #[test]
    fn ibr_co_15_suggests_from_the_current_inputs() {
        let wrong = patched(STI, json!({"total_amount": "11175.46"}));
        assert_eq!(
            findings(&wrong, "ibr-co-15"),
            [found("total_amount", "IBT-112", "11175.45")]
        );
        apply_suggestion(&wrong, "ibr-co-15");
        let chained = patched(STI, json!({"totals.tax_exclusive_amount": "10000"}));
        assert_eq!(suggestion(&chained, "ibr-co-15"), "10532.16");
        let half = patched(MAN, json!({"vat_amount": "50.005", "total_amount": "1"}));
        assert_eq!(suggestion(&half, "ibr-co-15"), "1050.01");
        // Without IBT-110 the first TaxTotal is the accounting one; it counts when IBT-006
        // equals IBT-005.
        let accounting = patched(
            MAN,
            json!({"vat_amount": "", "tax_currency": "AED", "totals.tax_amount_accounting_currency": "50", "total_amount": "1"}),
        );
        assert_eq!(suggestion(&accounting, "ibr-co-15"), "1050.00");
        let other = patched(
            MAN,
            json!({"vat_amount": "", "tax_currency": "USD", "totals.tax_amount_accounting_currency": "50"}),
        );
        assert_eq!(suggestion(&other, "ibr-co-15"), "");
        let no_currency = patched(MAN, json!({"currency": ""}));
        assert_eq!(suggestion(&no_currency, "ibr-co-15"), "");
        let inclusive = patched(
            STI,
            json!({"total_amount": "1", "totals.tax_inclusive_pricing": true}),
        );
        assert!(findings(&inclusive, "ibr-co-15").is_empty());
    }

    #[test]
    fn ibr_co_16_suggests_the_one_two_decimal_amount_in_every_branch() {
        for (slug, set, want) in [
            // Neither paid nor rounding amount: IBT-112 itself.
            (MAN, json!({"totals.payable_amount": "1"}), "1050.00"),
            // Zero paid and rounding amounts count as absent.
            (ECO, json!({"totals.payable_amount": "104.99"}), "105.00"),
            // Paid amount only.
            (EXT, json!({"totals.rounding_amount": ""}), "5777.20"),
            // Paid and rounding amount.
            (EXT, json!({"totals.payable_amount": "5777.51"}), "5777.50"),
            // A rounding amount with three decimals: 5777.20 + 0.305 = 5777.505, and only
            // 5777.50 passes (probes co16-b3-3dec-rounding and -b).
            (
                EXT,
                json!({"totals.rounding_amount": "0.305", "totals.payable_amount": "1"}),
                "5777.50",
            ),
            // Rounding amount only.
            (
                STI,
                json!({"totals.payable_amount": "11175.40"}),
                "11175.50",
            ),
            (CON, json!({"totals.payable_amount": "6056.25"}), "6057.00"),
            // A negative paid amount adds.
            (
                MAN,
                json!({"totals.paid_amount": "-50", "totals.payable_amount": "1"}),
                "1100.00",
            ),
            // A credit note with negative totals.
            (
                CN,
                json!({"total_amount": "-11175.45", "totals.payable_amount": "1"}),
                "-11175.40",
            ),
        ] {
            let inv = patched(slug, set.clone());
            assert_eq!(suggestion(&inv, "ibr-co-16"), want, "{slug} {set}");
            let fixed = apply_suggestion(&inv, "ibr-co-16");
            // The one two-decimal amount: a cent either way fails.
            for delta in ["0.01", "-0.01"] {
                let other = decimal::add(d(want), d(delta)).unwrap().to_string();
                let neighbour = with(&fixed, "totals.payable_amount", &other);
                assert_eq!(findings(&neighbour, "ibr-co-16").len(), 1, "{other}");
            }
        }
        // IBT-112 with three decimals: no two-decimal amount can pass.
        for (slug, set) in [
            (
                MAN,
                json!({"total_amount": "1050.001", "totals.payable_amount": "1"}),
            ),
            (CON, json!({"total_amount": "6056.255"})),
        ] {
            let inv = patched(slug, set.clone());
            assert_eq!(suggestion(&inv, "ibr-co-16"), "", "{set}");
        }
    }

    /// `ibr-127-ae` (header) fails for an invoice outside deemed supply with IBT-115 > 0 and
    /// no due date. Moving IBT-115 from zero or below to above zero would newly break it, so
    /// that suggestion is withheld; every other case is suggested.
    #[test]
    fn ibr_co_16_never_suggests_an_amount_that_makes_ibr_127_ae_fail() {
        let undated = patched(
            MAN,
            json!({"payment_due_date": "", "totals.payable_amount": "-1"}),
        );
        assert!(!counts(&undated).contains_key("ibr-127-ae"));
        assert_eq!(suggestion(&undated, "ibr-co-16"), "");
        let fixed = with(&undated, "totals.payable_amount", "1050.00");
        assert!(counts(&fixed).contains_key("ibr-127-ae"));
        for (slug, set) in [
            // A due date.
            (MAN, json!({"totals.payable_amount": "-1"})),
            // Already failing: nothing newly breaks.
            (
                MAN,
                json!({"payment_due_date": "", "totals.payable_amount": "2"}),
            ),
            // A credit note.
            (CN, json!({"totals.payable_amount": "-1"})),
            // Deemed supply (BTAE-02 position 2).
            (
                MAN,
                json!({"payment_due_date": "", "transaction_type_code": "01000000", "totals.payable_amount": "-1"}),
            ),
        ] {
            let inv = patched(slug, set.clone());
            assert_ne!(suggestion(&inv, "ibr-co-16"), "", "{set}");
            apply_suggestion(&inv, "ibr-co-16");
        }
        // A negative candidate never touches ibr-127-ae.
        let credit = patched(
            MAN,
            json!({"payment_due_date": "", "total_amount": "-1050", "totals.payable_amount": "1"}),
        );
        assert_eq!(suggestion(&credit, "ibr-co-16"), "-1050.00");
    }

    /// Protocol step 8 as a property, over 24 seeded perturbations (one to three chain
    /// fields cleared, zeroed, nudged by a cent or half a cent, or replaced) of each of the 30
    /// examples: every suggestion fixes its own rule and newly breaks at most its downstream
    /// chain rule; applying every suggestion round by round reaches a fixpoint within six
    /// rounds, and the chain issues left have no one correct value (a total without any
    /// allowance or charge of its kind).
    #[test]
    fn suggestions_break_nothing_outside_the_chain_and_settle() {
        const FIELDS: [&str; 8] = [
            "totals.line_extension_amount",
            "totals.allowance_total_amount",
            "totals.charge_total_amount",
            "totals.tax_exclusive_amount",
            "total_amount",
            "totals.payable_amount",
            "totals.paid_amount",
            "totals.rounding_amount",
        ];
        const DELTAS: [&str; 8] = [
            "0.01", "-0.01", "0.005", "-0.005", "1", "-1", "0.001", "100.5",
        ];
        let current = |inv: &pb::Invoice, path: &str| -> String {
            let t = inv.totals.clone().unwrap_or_default();
            match path {
                "total_amount" => inv.total_amount.clone(),
                "totals.line_extension_amount" => t.line_extension_amount,
                "totals.allowance_total_amount" => t.allowance_total_amount,
                "totals.charge_total_amount" => t.charge_total_amount,
                "totals.tax_exclusive_amount" => t.tax_exclusive_amount,
                "totals.payable_amount" => t.payable_amount,
                "totals.paid_amount" => t.paid_amount,
                _ => t.rounding_amount,
            }
        };
        let mut rng = Rng(0x746f_7461_6c73);
        let (mut single, mut settled) = (0, 0);
        for (slug, base) in bases() {
            for _ in 0..24 {
                let mut inv = base.clone();
                for _ in 0..=rng.below(3) {
                    let path = *rng.pick(&FIELDS);
                    let value = match rng.below(6) {
                        0 => String::new(),
                        1 => "0".to_string(),
                        2 | 3 => match decimal::parse(&current(&inv, path)) {
                            Ok(v) => decimal::add(v, d(rng.pick(&DELTAS))).unwrap().to_string(),
                            Err(_) => rng.amount(100_000, 3).0,
                        },
                        _ => rng.amount(100_000, 3).0,
                    };
                    inv = with(&inv, path, &value);
                }
                for (rule, _, _) in suggestions(&inv) {
                    apply_suggestion(&inv, &rule);
                    single += 1;
                }
                let mut cur = inv.clone();
                for round in 0.. {
                    let s = suggestions(&cur);
                    if s.is_empty() {
                        break;
                    }
                    assert!(round < 6, "{slug}: no fixpoint after six rounds: {s:?}");
                    for (_, path, value) in &s {
                        cur = with(&cur, path, value);
                    }
                    settled += 1;
                }
                let doc = Doc::new(&cur);
                for (id, _) in counts(&cur) {
                    if !CHAIN.iter().any(|(c, _)| *c == id) {
                        continue;
                    }
                    let explained = match id.as_str() {
                        "ibr-co-11" => document(&doc, false).next().is_none(),
                        "ibr-co-12" => document(&doc, true).next().is_none(),
                        _ => false,
                    };
                    assert!(explained, "{slug}: {id} left after settling: {cur:?}");
                }
            }
        }
        assert!(single > 300 && settled > 300, "{single} {settled}");
    }

    // ------------------------------------------------------------------ decimal properties

    /// The exact cents agree with `decimal::{add, sub, xpath_round2}` wherever `rust_decimal`
    /// is exact (operands of 12 integer digits and 6 decimals), and value equality is exact.
    #[test]
    fn cents_agree_with_the_decimal_helpers_wherever_those_are_exact() {
        let mut rng = Rng(7);
        let rounded =
            |c: Option<Cents>| c.and_then(Cents::round_half_up).and_then(Cents::to_decimal);
        for _ in 0..20_000 {
            let (a, b) = (
                rng.decimal(1_000_000_000_000, 6),
                rng.decimal(1_000_000_000_000, 6),
            );
            let (ca, cb) = (Cents::of(a).unwrap(), Cents::of(b).unwrap());
            assert_eq!(
                rounded(ca.add(cb)),
                Some(decimal::xpath_round2(decimal::add(a, b).unwrap())),
                "{a} + {b}"
            );
            assert_eq!(
                rounded(ca.sub(cb)),
                Some(decimal::xpath_round2(decimal::sub(a, b).unwrap())),
                "{a} - {b}"
            );
            assert_eq!(rounded(Some(ca)), Some(decimal::xpath_round2(a)), "{a}");
            assert_eq!(ca == cb, a == b, "{a} {b}");
            assert_eq!(ca.neg().and_then(Cents::neg), Some(ca));
            // Half down is half up mirrored.
            assert_eq!(
                ca.round_half_down(),
                ca.neg().and_then(Cents::round_half_up).and_then(Cents::neg),
                "{a}"
            );
        }
    }

    /// XPath `round` is half toward positive infinity, not half-even and not half away from
    /// zero; a negative zero is never suggested.
    #[test]
    fn rounding_is_half_toward_positive_infinity() {
        for (net, want) in [
            ("0.125", "0.13"),
            ("0.135", "0.14"),
            ("2.345", "2.35"),
            ("-2.345", "-2.34"),
            ("-2.355", "-2.35"),
            ("-0.005", "0.00"),
            ("-0.015", "-0.01"),
            ("0.005", "0.01"),
            ("1050.0049999999999999999", "1050.00"),
            ("0", "0.00"),
            ("-0", "0.00"),
            ("-0.0000001", "0.00"),
        ] {
            let inv = patched(
                MAN,
                json!({"lines[0].net_amount": net, "totals.line_extension_amount": "0.001"}),
            );
            assert_eq!(suggestion(&inv, "ibr-co-10"), want, "{net}");
        }
    }

    /// `ibr-co-10` over invoices of up to 1,000 lines with signed amounts of up to four
    /// decimals, against an integer oracle: the suggestion is the half-up rounded sum, and
    /// that value passes.
    #[test]
    fn line_sums_match_an_integer_oracle_over_many_lines() {
        let mut rng = Rng(42);
        let base = example(MAN);
        for round in 0..12 {
            let n = if round == 0 {
                1000
            } else {
                1 + usize::try_from(rng.below(400)).unwrap()
            };
            let mut inv = base.clone();
            let mut oracle: i128 = 0;
            inv.lines = (0..n)
                .map(|_| {
                    let (s, units) = rng.amount(1_000_000_000, 4);
                    oracle += units;
                    let mut l = base.lines[0].clone();
                    l.net_amount = s;
                    l
                })
                .collect();
            inv.totals.as_mut().unwrap().line_extension_amount = "0.001".to_string();
            // floor(sum * 100 + 0.5) with sum = oracle / 10^4.
            let want = format_cents((oracle + 50).div_euclid(100));
            let doc = Doc::new(&inv);
            let mut sink = Sink::new("totals.line_extension_amount");
            ibr_co_10(&doc, &mut sink);
            let got = sink.into_findings();
            assert_eq!(got.len(), 1);
            assert_eq!(
                got[0].suggested_value.as_deref(),
                Some(want.as_str()),
                "{n} lines"
            );
            inv.totals.as_mut().unwrap().line_extension_amount = want;
            let doc = Doc::new(&inv);
            let mut sink = Sink::new("totals.line_extension_amount");
            ibr_co_10(&doc, &mut sink);
            assert!(sink.findings().is_empty());
        }
    }

    /// Amounts up to the contract's 28 significant digits: sums `rust_decimal` would round are
    /// exact here, sums beyond 28 digits fail without a suggestion (none would parse), and
    /// nothing panics.
    #[test]
    fn large_magnitudes_up_to_the_contract_limit() {
        // decimal::add rounds 1e25 + 0.0049… to …0.005, which then rounds up; the exact sum
        // rounds down (probe co10-precision).
        let (a, b) = (
            d("10000000000000000000000000"),
            d("0.0049999999999999999999999"),
        );
        assert_eq!(
            decimal::xpath_round2(decimal::add(a, b).unwrap()),
            d("10000000000000000000000000.01")
        );
        let two_lines = |lea: &str| {
            patched(
                MAN,
                json!({
                    "lines[0].net_amount": "10000000000000000000000000",
                    "lines[1].id": "2",
                    "lines[1].net_amount": "0.0049999999999999999999999",
                    "totals.line_extension_amount": lea,
                }),
            )
        };
        assert!(findings(&two_lines("10000000000000000000000000.00"), "ibr-co-10").is_empty());
        assert_eq!(
            suggestion(&two_lines("10000000000000000000000000.01"), "ibr-co-10"),
            "10000000000000000000000000.00"
        );
        // 28 digits everywhere, consistent: no issue (probe big).
        let big = patched(
            MAN,
            json!({
                "lines[0].net_amount": "9999999999999999999999999.99",
                "totals.line_extension_amount": "9999999999999999999999999.99",
                "totals.tax_exclusive_amount": "9999999999999999999999999.99",
                "total_amount": "10000000000000000000000049.99",
                "totals.payable_amount": "10000000000000000000000049.99",
            }),
        );
        assert!(totals_ids(&big).is_empty(), "{:?}", totals_ids(&big));
        // A suggestion of 27 integer digits keeps only the decimal that 28 digits allow.
        let wide = patched(
            MAN,
            json!({"lines[0].net_amount": "999999999999999999999999999", "totals.line_extension_amount": "1"}),
        );
        assert_eq!(
            suggestion(&wide, "ibr-co-10"),
            "999999999999999999999999999.0"
        );
        // Ten lines of the largest 28-digit amount: the sum has 29 digits, no amount the
        // contract admits can equal it.
        let mut many = example(MAN);
        many.lines = (0..10)
            .map(|_| {
                let mut l = many.lines[0].clone();
                l.net_amount = "9999999999999999999999999999".to_string();
                l
            })
            .collect();
        assert_eq!(suggestion(&many, "ibr-co-10"), "");
        // The extremes of rust_decimal itself never panic.
        for x in [Decimal::MAX, Decimal::MIN, Decimal::ZERO, d("-0.00")] {
            let c = Cents::of(x).unwrap();
            assert!(c.add(c).is_some() && c.sub(c).is_some());
            let _ = c.round_half_up().and_then(Cents::to_decimal);
        }
        assert_eq!(Cents::of(d("-0.00")), Some(Cents::ZERO));
    }

    /// For random IBT-112, IBT-113 and IBT-114 (absent, zero, signed, up to four decimals)
    /// the `ibr-co-16` candidate is exactly the set of two-decimal amounts that pass: one when
    /// it exists, none otherwise.
    #[test]
    fn the_co16_candidate_is_the_only_two_decimal_amount_that_passes() {
        let mut rng = Rng(16);
        let mut found_some = 0;
        for _ in 0..5_000 {
            let maybe = |rng: &mut Rng, dec: u32| match rng.below(4) {
                0 => None,
                1 => Some(Decimal::ZERO),
                _ => Some(rng.decimal(10_000, dec)),
            };
            let tia_decimals = if rng.below(4) == 0 { 3 } else { 2 };
            let tia = Some(rng.decimal(100_000, tia_decimals));
            let paid = maybe(&mut rng, 2);
            let rounding = maybe(&mut rng, 4);
            let z = [tia, rounding]
                .into_iter()
                .flatten()
                .try_fold(Decimal::ZERO, decimal::add)
                .and_then(|s| decimal::sub(s, paid.unwrap_or_default()))
                .unwrap();
            let centre = Cents::of(z).unwrap().whole;
            let passing: Vec<Decimal> = (centre - 3..=centre + 3)
                .filter_map(|k| Decimal::try_from_i128_with_scale(k, 2).ok())
                .filter(|y| co16_holds(Some(*y), tia, paid, rounding))
                .collect();
            let candidate = co16_candidate(tia, paid, rounding).and_then(Cents::to_decimal);
            assert_eq!(
                passing,
                candidate.into_iter().collect::<Vec<_>>(),
                "{tia:?} {paid:?} {rounding:?}"
            );
            found_some += usize::from(candidate.is_some());
        }
        assert!(found_some > 3_000, "{found_some}");
    }

    #[test]
    fn cents_render_within_the_contract_grammar() {
        let c = |s: &str| Cents::of(d(s)).unwrap();
        assert_eq!(c("1050").to_decimal().unwrap().to_string(), "1050.00");
        assert_eq!(c("-0.05").to_decimal().unwrap().to_string(), "-0.05");
        assert_eq!(c("0.001").to_decimal(), None);
        assert_eq!(
            c("99999999999999999999999999.99")
                .to_decimal()
                .unwrap()
                .to_string(),
            "99999999999999999999999999.99"
        );
        assert_eq!(
            c("999999999999999999999999999.9")
                .to_decimal()
                .unwrap()
                .to_string(),
            "999999999999999999999999999.9"
        );
        assert_eq!(
            c("9999999999999999999999999999")
                .to_decimal()
                .unwrap()
                .to_string(),
            "9999999999999999999999999999"
        );
        let too_wide = Cents {
            whole: 99_999_999_999_999_999_999_999_999_999,
            sub: 0,
        };
        assert_eq!(too_wide.to_decimal(), None);
        assert_eq!(
            Cents::of(d("0.0000000000000000000000000001")),
            Some(Cents { whole: 0, sub: 1 })
        );
        assert_eq!(
            Cents::of(d("-0.0000000000000000000000000001")),
            Some(Cents {
                whole: -1,
                sub: SUB_PER_CENT - 1
            })
        );
    }
}
