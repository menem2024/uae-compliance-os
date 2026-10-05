//! `vat` family rules (spec 5.2.4): the VAT breakdown (IBG-23, `cac:TaxSubtotal` of the
//! document-currency `cac:TaxTotal`), the tax categories of document allowances and charges
//! (IBG-20, IBG-21) and of lines (IBG-30), and the VAT line amount (BTAE-08), plus the root
//! terms about categories, and `ibr-124`, `ibr-126`, `ibr-co-14`. Coverage:
//! `rulesets/pint-ae-1.0.4/coverage/vat.tsv`; fixtures: `mutations/vat.jsonl`, run by
//! `tests/family_fixtures.rs` and, against the official schematron, by `conformance mutations`.
//!
//! Each rule is "this official assert fails on the XML the exporter writes for this `Doc`"
//! (Rule authoring protocol). What the exporter writes is the whole story:
//!
//! * the document-currency `cac:TaxTotal`, and with it every `cac:TaxSubtotal`, is written only
//!   when IBT-110 is a valid decimal ([`subs`]); a breakdown entry is written when one of its
//!   amounts or its category is;
//! * a tax category (`cac:TaxCategory`, `cac:ClassifiedTaxCategory`) is written when one of its
//!   fields is present ([`category`]); its `cac:TaxScheme/cbc:ID` is the written scheme or `VAT`;
//! * a decimal that does not parse is not written, so for the XPath it does not exist.
//!
//! **Scheme tests.** The aligned rules select categories with
//! `[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']` ([`Cat::vat`]); several official
//! tests omit it and compare the code alone ([`Cat::code_is`]): `ibr-102-ae` and the sums of the
//! `-08` rules over the lines, `aligned-ibrp-s-01`, `ibr-105-ae` (breakdown count),
//! `ibr-116-ae`, `ibr-119-ae`, `ibr-122-ae`, `ibr-151-ae`, `ibr-190-ae`. Each rule's comment says
//! which one it is.
//!
//! **Upstream defect 1** (CI "Upstream defects"): the `.gc` file spells the last tax category
//! code with U+039D (Greek capital Nu); the schematron tests ASCII `N`. Every comparison here is
//! on the exact written code, so `N` is the additional-VAT category and U+039D is an unknown
//! code (`ibr-139-ae`); `tests::the_greek_nu_is_not_the_additional_vat_category` pins both.
//!
//! **Upstream defect 2**: the official contexts of `ibr-co-14` and `ibr-124` for credit notes are
//! `/cn:CreditNote/cac:Taxtotal` (lower-case `t`), which matches nothing, so the official
//! schematron never fires them for credit notes. Both rules here apply to credit notes too;
//! `conformance/allowlist.tsv` lists (rule id, `credit_note`), and the `ibr-124#3` and
//! `ibr-co-14#2` fixtures keep the rule in their `expect` list (the RuleSet's output) while the
//! official run has none.
//!
//! **Type errors in the official stylesheet.** `u:slack(exp, val, s)` declares `exp` as
//! `xs:decimal`, so a breakdown entry without IBT-116 raises a dynamic type error where the
//! test would call it (`aligned-ibrp-s-08`, `ibr-102-ae`). The platform reports such an entry
//! as failing; `aligned-ibrp-045` reports the missing amount as well. No fixture can exercise
//! the case (Saxon stops), `tests::a_breakdown_entry_without_a_taxable_amount_fails_the_slack_rules`
//! does.
//!
//! Status of the 57 rows: 56 implemented ([`RULES`]) and one structural, `ibr-sr-32` (every
//! breakdown entry holds one category and the exporter writes at most one `TaxExemptionReason`
//! per category; proved by `tests::structural_rules_hold_on_every_exported_breakdown`). No row
//! is `upstream_noop`: `ibr-187-ae` (the constant `true()`, defect 4) has the context `cac:Item`
//! and the `lines` family (`rule-families.draft.tsv` row 253, `lines.tsv`).

use rust_decimal::Decimal;

use crate::codelists::sets;
use crate::decimal;
use crate::doc::{self, Dec, Doc, text};
use crate::pb;
use crate::rule::{Rule, Sink, fill};

/// Every implemented rule of the family; the coverage rows with status `implemented` are exactly
/// these.
pub static RULES: &[Rule] = &[
    Rule {
        id: "aligned-ibrp-045",
        check: aligned_ibrp_045,
    },
    Rule {
        id: "aligned-ibrp-046",
        check: aligned_ibrp_046,
    },
    Rule {
        id: "aligned-ibrp-047",
        check: aligned_ibrp_047,
    },
    Rule {
        id: "aligned-ibrp-048",
        check: aligned_ibrp_048,
    },
    Rule {
        id: "aligned-ibrp-ae-01-ae",
        check: aligned_ibrp_ae_01_ae,
    },
    Rule {
        id: "aligned-ibrp-ae-05-ae",
        check: aligned_ibrp_ae_05_ae,
    },
    Rule {
        id: "aligned-ibrp-ae-06",
        check: aligned_ibrp_ae_06,
    },
    Rule {
        id: "aligned-ibrp-ae-07",
        check: aligned_ibrp_ae_07,
    },
    Rule {
        id: "aligned-ibrp-ae-08-ae",
        check: aligned_ibrp_ae_08_ae,
    },
    Rule {
        id: "aligned-ibrp-ae-09-ae",
        check: aligned_ibrp_ae_09_ae,
    },
    Rule {
        id: "aligned-ibrp-e-01",
        check: aligned_ibrp_e_01,
    },
    Rule {
        id: "aligned-ibrp-e-05",
        check: aligned_ibrp_e_05,
    },
    Rule {
        id: "aligned-ibrp-e-06",
        check: aligned_ibrp_e_06,
    },
    Rule {
        id: "aligned-ibrp-e-07",
        check: aligned_ibrp_e_07,
    },
    Rule {
        id: "aligned-ibrp-e-08",
        check: aligned_ibrp_e_08,
    },
    Rule {
        id: "aligned-ibrp-e-09",
        check: aligned_ibrp_e_09,
    },
    Rule {
        id: "aligned-ibrp-o-01",
        check: aligned_ibrp_o_01,
    },
    Rule {
        id: "aligned-ibrp-o-05",
        check: aligned_ibrp_o_05,
    },
    Rule {
        id: "aligned-ibrp-o-06",
        check: aligned_ibrp_o_06,
    },
    Rule {
        id: "aligned-ibrp-o-07",
        check: aligned_ibrp_o_07,
    },
    Rule {
        id: "aligned-ibrp-o-08",
        check: aligned_ibrp_o_08,
    },
    Rule {
        id: "aligned-ibrp-o-09",
        check: aligned_ibrp_o_09,
    },
    Rule {
        id: "aligned-ibrp-o-11-ae",
        check: aligned_ibrp_o_11_ae,
    },
    Rule {
        id: "aligned-ibrp-s-01",
        check: aligned_ibrp_s_01,
    },
    Rule {
        id: "aligned-ibrp-s-05",
        check: aligned_ibrp_s_05,
    },
    Rule {
        id: "aligned-ibrp-s-06",
        check: aligned_ibrp_s_06,
    },
    Rule {
        id: "aligned-ibrp-s-07",
        check: aligned_ibrp_s_07,
    },
    Rule {
        id: "aligned-ibrp-s-08",
        check: aligned_ibrp_s_08,
    },
    Rule {
        id: "aligned-ibrp-s-09",
        check: aligned_ibrp_s_09,
    },
    Rule {
        id: "aligned-ibrp-s-10",
        check: aligned_ibrp_s_10,
    },
    Rule {
        id: "aligned-ibrp-z-01",
        check: aligned_ibrp_z_01,
    },
    Rule {
        id: "aligned-ibrp-z-05",
        check: aligned_ibrp_z_05,
    },
    Rule {
        id: "aligned-ibrp-z-06",
        check: aligned_ibrp_z_06,
    },
    Rule {
        id: "aligned-ibrp-z-07",
        check: aligned_ibrp_z_07,
    },
    Rule {
        id: "aligned-ibrp-z-08",
        check: aligned_ibrp_z_08,
    },
    Rule {
        id: "aligned-ibrp-z-09",
        check: aligned_ibrp_z_09,
    },
    Rule {
        id: "ibr-102-ae",
        check: ibr_102_ae,
    },
    Rule {
        id: "ibr-103-ae",
        check: ibr_103_ae,
    },
    Rule {
        id: "ibr-105-ae",
        check: ibr_105_ae,
    },
    Rule {
        id: "ibr-108-ae",
        check: ibr_108_ae,
    },
    Rule {
        id: "ibr-116-ae",
        check: ibr_116_ae,
    },
    Rule {
        id: "ibr-119-ae",
        check: ibr_119_ae,
    },
    Rule {
        id: "ibr-120-ae",
        check: ibr_120_ae,
    },
    Rule {
        id: "ibr-121-ae",
        check: ibr_121_ae,
    },
    Rule {
        id: "ibr-122-ae",
        check: ibr_122_ae,
    },
    Rule {
        id: "ibr-124",
        check: ibr_124,
    },
    Rule {
        id: "ibr-126",
        check: ibr_126,
    },
    Rule {
        id: "ibr-133-ae",
        check: ibr_133_ae,
    },
    Rule {
        id: "ibr-139-ae",
        check: ibr_139_ae,
    },
    Rule {
        id: "ibr-151-ae",
        check: ibr_151_ae,
    },
    Rule {
        id: "ibr-162-ae",
        check: ibr_162_ae,
    },
    Rule {
        id: "ibr-163-ae",
        check: ibr_163_ae,
    },
    Rule {
        id: "ibr-165-ae",
        check: ibr_165_ae,
    },
    Rule {
        id: "ibr-174-ae",
        check: ibr_174_ae,
    },
    Rule {
        id: "ibr-190-ae",
        check: ibr_190_ae,
    },
    Rule {
        id: "ibr-co-14",
        check: ibr_co_14,
    },
];

/// `u:slack` of the `-08`, `-09` and `ibr-102-ae` tests.
const SLACK: Decimal = Decimal::from_parts(2, 0, 0, false, 2);

// ------------------------------------------------------------------------------ what is written

/// A written tax category element: its code, rate, scheme and exemption reason as the XPath
/// sees them.
#[derive(Debug, Clone, Copy)]
struct Cat<'a> {
    /// `cbc:ID` (IBT-118, IBT-095, IBT-102, IBT-151); `None` when not written.
    code: Option<&'a str>,
    /// `cbc:Percent`; `exists()` when written.
    rate: Dec<'a>,
    /// `cac:TaxScheme/cbc:ID`: the written scheme, `VAT` by default.
    scheme: &'a str,
    /// `cbc:TaxExemptionReasonCode`.
    reason_code: Option<&'a str>,
    /// `cbc:TaxExemptionReason`.
    reason: Option<&'a str>,
}

impl Cat<'_> {
    /// `cac:TaxScheme/normalize-space(upper-case(cbc:ID)) = 'VAT'`.
    fn vat(&self) -> bool {
        normalize_space(self.scheme).to_uppercase() == "VAT"
    }

    /// `normalize-space(cbc:ID) = code`. The exporter writes the trimmed code, so only the exact
    /// text can equal a code (which has no white space).
    fn code_is(&self, code: &str) -> bool {
        self.code == Some(code)
    }

    /// `[normalize-space(cbc:ID) = code][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`.
    fn is(&self, code: &str) -> bool {
        self.vat() && self.code_is(code)
    }
}

/// XPath `normalize-space`: strips leading and trailing XML white space and collapses inner runs.
fn normalize_space(s: &str) -> String {
    s.split([' ', '\t', '\n', '\r'])
        .filter(|part| !part.is_empty())
        .collect::<Vec<_>>()
        .join(" ")
}

/// The category element written for `c` (`export::tax_category`): any of the code, a valid rate,
/// the exemption reason code or text, or the scheme is present.
fn category<'a>(c: Option<&'a pb::TaxCategory>, rate: Dec<'a>) -> Option<Cat<'a>> {
    let c = c?;
    let code = text(&c.code);
    let reason_code = text(&c.exemption_reason_code);
    let reason = text(&c.exemption_reason_text);
    doc::tax_category_written(Some(c), rate).then(|| Cat {
        code,
        rate,
        scheme: doc::tax_scheme(c),
        reason_code,
        reason,
    })
}

/// A written `cac:TaxSubtotal` (IBG-23).
#[derive(Debug, Clone, Copy)]
struct Sub<'a> {
    /// Index in `tax_breakdown`.
    i: usize,
    taxable: Dec<'a>,
    tax: Dec<'a>,
    cat: Option<Cat<'a>>,
}

impl Sub<'_> {
    /// The category is `code` under the VAT scheme (the aligned context
    /// `cac:TaxCategory[normalize-space(cbc:ID) = code][VAT scheme]`).
    fn is(&self, code: &str) -> bool {
        self.cat.is_some_and(|c| c.is(code))
    }
}

/// Every written `cac:TaxSubtotal`: none unless IBT-110 is written (it owns the `cac:TaxTotal`).
fn subs<'d, 'a>(doc: &'d Doc<'a>) -> impl Iterator<Item = Sub<'a>> + 'd {
    let written = doc.vat_amount.exists();
    doc.inv
        .tax_breakdown
        .iter()
        .zip(&doc.tax_breakdown)
        .enumerate()
        .filter_map(move |(i, (t, d))| {
            if !written {
                return None;
            }
            let cat = category(t.category.as_ref(), d.rate);
            (d.taxable_amount.exists() || d.tax_amount.exists() || cat.is_some()).then_some(Sub {
                i,
                taxable: d.taxable_amount,
                tax: d.tax_amount,
                cat,
            })
        })
}

/// A document-level `cac:AllowanceCharge` with a written `cac:TaxCategory`.
#[derive(Debug, Clone, Copy)]
struct Ac<'a> {
    /// Index in `allowances_charges`.
    i: usize,
    /// `cbc:ChargeIndicator`.
    charge: bool,
    amount: Dec<'a>,
    cat: Cat<'a>,
}

/// The document allowances and charges that have a written tax category. A line-level
/// allowance or charge never has one.
fn acs<'d, 'a>(doc: &'d Doc<'a>) -> impl Iterator<Item = Ac<'a>> + 'd {
    doc.inv
        .allowances_charges
        .iter()
        .zip(&doc.allowances_charges)
        .enumerate()
        .filter_map(|(i, (a, d))| {
            category(a.tax_category.as_ref(), d.rate).map(|cat| Ac {
                i,
                charge: a.is_charge,
                amount: d.amount,
                cat,
            })
        })
}

/// A line with a written `cac:ClassifiedTaxCategory`.
#[derive(Debug, Clone, Copy)]
struct Ln<'a> {
    /// Index in `lines`.
    i: usize,
    line: &'a pb::InvoiceLine,
    /// IBT-131.
    net: Dec<'a>,
    /// BTAE-10 (written with `cac:ItemPriceExtension`).
    aed: Dec<'a>,
    /// BTAE-08 (written inside `cac:ItemPriceExtension`, so only with BTAE-10).
    vat_aed: Dec<'a>,
    cat: Cat<'a>,
}

/// The lines that have a written tax category (such a line is written).
fn line_cats<'d, 'a>(doc: &'d Doc<'a>) -> impl Iterator<Item = Ln<'a>> + 'd {
    doc.inv
        .lines
        .iter()
        .zip(&doc.lines)
        .enumerate()
        .filter_map(|(i, (l, d))| {
            category(l.tax.as_ref(), d.rate).map(|cat| Ln {
                i,
                line: l,
                net: d.net_amount,
                aed: d.amount_aed,
                vat_aed: d.vat_amount_aed,
                cat,
            })
        })
}

impl Ln<'_> {
    /// `cac:ItemPriceExtension/cac:TaxTotal/cbc:TaxAmount` exists.
    fn vat_aed_written(&self) -> bool {
        self.aed.exists() && self.vat_aed.exists()
    }
}

/// The written type code (IBT-003).
fn type_code<'a>(doc: &Doc<'a>) -> Option<&'a str> {
    text(&doc.inv.invoice_type_code)
}

/// Every written category code (`cbc:ID` of a `cac:TaxCategory` or `cac:ClassifiedTaxCategory`
/// anywhere in the document, any scheme).
fn all_codes<'d, 'a>(doc: &'d Doc<'a>) -> impl Iterator<Item = &'a str> + 'd {
    subs(doc)
        .filter_map(|s| s.cat.and_then(|c| c.code))
        .chain(acs(doc).filter_map(|a| a.cat.code))
        .chain(line_cats(doc).filter_map(|n| n.cat.code))
}

/// `exists(//cac:TaxCategory[VAT]/cbc:ID[. = code]) or exists(//cac:ClassifiedTaxCategory[VAT]/cbc:ID[. = code])`.
fn used(doc: &Doc<'_>, code: &str) -> bool {
    subs(doc).any(|s| s.is(code))
        || acs(doc).any(|a| a.cat.is(code))
        || line_cats(doc).any(|n| n.cat.is(code))
}

/// `count(cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[VAT]/cbc:ID[. = code])`.
fn breakdown_count(doc: &Doc<'_>, code: &str) -> usize {
    subs(doc).filter(|s| s.is(code)).count()
}

// ------------------------------------------------------------------------------ sums

/// `$rate` filter of the sums: absent means every rate.
fn rate_is(rate: Dec<'_>, want: Option<Decimal>) -> bool {
    want.is_none_or(|w| rate.value == Some(w))
}

/// `sum(../../../cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)=code]
/// [... xs:decimal(cbc:Percent) = $rate]/xs:decimal(cbc:LineExtensionAmount))`; no scheme test.
fn sum_lines(doc: &Doc<'_>, code: &str, rate: Option<Decimal>) -> Option<Decimal> {
    decimal::sum(
        line_cats(doc)
            .filter(|n| n.cat.code_is(code) && rate_is(n.cat.rate, rate))
            .filter_map(|n| n.net.value),
    )
}

/// `sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator = charge][cac:TaxCategory/normalize-space(cbc:ID)=code]
/// [... xs:decimal(cbc:Percent) = $rate]/xs:decimal(cbc:Amount))`; no scheme test.
fn sum_acs(doc: &Doc<'_>, code: &str, charge: bool, rate: Option<Decimal>) -> Option<Decimal> {
    decimal::sum(
        acs(doc)
            .filter(|a| a.charge == charge && a.cat.code_is(code) && rate_is(a.cat.rate, rate))
            .filter_map(|a| a.amount.value),
    )
}

/// `lines + charges - allowances` of `code` (and `rate`), with `lines` the sum over the lines or
/// 0 for the other line kind. `None` on overflow.
fn net_of(doc: &Doc<'_>, code: &str, rate: Option<Decimal>, with_lines: bool) -> Option<Decimal> {
    let lines = if with_lines {
        sum_lines(doc, code, rate)?
    } else {
        Decimal::ZERO
    };
    let plus = decimal::add(lines, sum_acs(doc, code, true, rate)?)?;
    decimal::sub(plus, sum_acs(doc, code, false, rate)?)
}

// ------------------------------------------------------------------------------ shared tests

/// Breakdown entries of category `code` (VAT scheme) whose tax amount is not 0
/// (`xs:decimal(../cbc:TaxAmount) = 0`; an absent amount is not 0).
fn tax_amount_is_zero(doc: &Doc<'_>, sink: &mut Sink<'_>, code: &str) {
    for s in subs(doc).filter(|s| s.is(code)) {
        if s.tax.value != Some(Decimal::ZERO) {
            sink.fail(&[s.i]).suggest("0.00");
        }
    }
}

/// Breakdown entries of category `code` (VAT scheme) whose taxable amount is not exactly the
/// lines plus charges minus allowances of that code (`-08` tests). Without a written line the
/// official test is false whatever the amounts are. The suggestion is the sum, when there is a
/// line and the sum fits.
fn taxable_is_exact(doc: &Doc<'_>, sink: &mut Sink<'_>, code: &str) {
    let lines = doc.lines_exist();
    let expected = if lines {
        net_of(doc, code, None, true)
    } else {
        None
    };
    for s in subs(doc).filter(|s| s.is(code)) {
        let holds = lines && matches!((s.taxable.value, expected), (Some(t), Some(e)) if t == e);
        if !holds {
            let f = sink.fail(&[s.i]);
            if let Some(e) = expected {
                f.suggest(e);
            }
        }
    }
}

/// Root test of `ae-01`, `e-01`, `o-01`, `z-01`: when `code` (VAT scheme) is used anywhere there
/// must be `exactly_one` (or at least one) breakdown entry with it. Fails once.
fn breakdown_for_used(doc: &Doc<'_>, sink: &mut Sink<'_>, code: &str, exactly_one: bool) {
    if used(doc, code) {
        let n = breakdown_count(doc, code);
        let holds = if exactly_one { n == 1 } else { n >= 1 };
        if !holds {
            sink.fail(&[]);
        }
    }
}

/// Document allowances (`charge` false) or charges of category `code` (VAT scheme) whose rate
/// does not satisfy `holds`; `zero` suggests 0.
fn ac_rate(
    doc: &Doc<'_>,
    sink: &mut Sink<'_>,
    code: &str,
    charge: bool,
    holds: fn(Dec<'_>) -> bool,
    zero: bool,
) {
    for a in acs(doc).filter(|a| a.charge == charge && a.cat.is(code)) {
        if !holds(a.cat.rate) {
            let f = sink.fail(&[a.i]);
            if zero {
                f.suggest("0");
            }
        }
    }
}

/// Lines of category `code` (VAT scheme) whose rate does not satisfy `holds`; `zero` suggests 0.
fn line_rate(
    doc: &Doc<'_>,
    sink: &mut Sink<'_>,
    code: &str,
    holds: fn(Dec<'_>) -> bool,
    zero: bool,
) {
    for n in line_cats(doc).filter(|n| n.cat.is(code)) {
        if !holds(n.cat.rate) {
            let f = sink.fail(&[n.i]);
            if zero {
                f.suggest("0");
            }
        }
    }
}

/// `cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) = 0`.
fn is_zero(rate: Dec<'_>) -> bool {
    rate.value == Some(Decimal::ZERO)
}

/// `cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) > 0`.
fn is_positive(rate: Dec<'_>) -> bool {
    rate.value.is_some_and(|r| r > Decimal::ZERO)
}

/// `not(cbc:Percent)`.
fn is_absent(rate: Dec<'_>) -> bool {
    !rate.exists()
}

/// `exists(cbc:Percent)`.
fn is_present(rate: Dec<'_>) -> bool {
    rate.exists()
}

// ------------------------------------------------------------------------------------- rules

/// `aligned-ibrp-045`, context `cac:TaxSubtotal`, test
/// `exists(cbc:TaxableAmount)`.
/// One finding per written breakdown entry without a valid IBT-116.
fn aligned_ibrp_045(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| !s.taxable.exists()) {
        sink.fail(&[s.i]);
    }
}

/// `aligned-ibrp-046`, context `cac:TaxSubtotal`, test
/// `exists(cbc:TaxAmount)`.
/// One finding per written breakdown entry without a valid IBT-117.
fn aligned_ibrp_046(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| !s.tax.exists()) {
        sink.fail(&[s.i]);
    }
}

/// `aligned-ibrp-047`, context `cac:TaxSubtotal`, test
/// `exists(cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID)`.
/// The category must be written under the VAT scheme with a code.
fn aligned_ibrp_047(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc) {
        if !s.cat.is_some_and(|c| c.vat() && c.code.is_some()) {
            sink.fail(&[s.i]);
        }
    }
}

/// `aligned-ibrp-048`, context `cac:TaxSubtotal`, test
/// `exists(cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:Percent) or (cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/normalize-space(cbc:ID)=('O','E'))`.
/// A VAT-scheme category with a rate, or with the code `O` or `E`.
fn aligned_ibrp_048(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc) {
        let holds = s
            .cat
            .is_some_and(|c| c.vat() && (c.rate.exists() || c.code_is("O") || c.code_is("E")));
        if !holds {
            sink.fail(&[s.i]);
        }
    }
}

/// `aligned-ibrp-ae-01-ae`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `((exists(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'AE']) or exists(//cac:ClassifiedTaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'AE'])) and (count(cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'AE']) >= 1)) or (not(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'AE']) and not(//cac:ClassifiedTaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'AE']))`.
/// At least one reverse-charge breakdown entry when AE is used anywhere.
fn aligned_ibrp_ae_01_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    breakdown_for_used(doc, sink, "AE", false);
}

/// `aligned-ibrp-ae-05-ae`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `exists(cbc:Percent)`.
fn aligned_ibrp_ae_05_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    line_rate(doc, sink, "AE", is_present, false);
}

/// `aligned-ibrp-ae-06`, context `cac:AllowanceCharge[cbc:ChargeIndicator=false()]/cac:TaxCategory[normalize-space(cbc:ID)='AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `exists(cbc:Percent) and normalize-space(cbc:Percent) != '' and cbc:Percent castable as xs:decimal and xs:decimal(normalize-space(cbc:Percent)) = 0`.
fn aligned_ibrp_ae_06(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "AE", false, is_zero, true);
}

/// `aligned-ibrp-ae-07`, context `cac:AllowanceCharge[cbc:ChargeIndicator=true()]/cac:TaxCategory[normalize-space(cbc:ID)='AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `exists(cbc:Percent) and normalize-space(cbc:Percent) != '' and cbc:Percent castable as xs:decimal and xs:decimal(normalize-space(cbc:Percent)) = 0`.
fn aligned_ibrp_ae_07(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "AE", true, is_zero, true);
}

/// `aligned-ibrp-ae-08-ae`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(exists(//cac:InvoiceLine) and (xs:decimal(../cbc:TaxableAmount) = (sum(../../../cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='AE']/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='AE']/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='AE']/xs:decimal(cbc:Amount))))) or (exists(//cac:CreditNoteLine) and (xs:decimal(../cbc:TaxableAmount) = (sum(../../../cac:CreditNoteLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='AE']/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='AE']/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='AE']/xs:decimal(cbc:Amount)))))`.
fn aligned_ibrp_ae_08_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    taxable_is_exact(doc, sink, "AE");
}

/// `aligned-ibrp-ae-09-ae`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `xs:decimal(../cbc:TaxAmount) = 0`.
fn aligned_ibrp_ae_09_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    tax_amount_is_zero(doc, sink, "AE");
}

/// `aligned-ibrp-e-01`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `((exists(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'E']) or exists(//cac:ClassifiedTaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'E'])) and (count(cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'E']) = 1)) or (not(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'E']) and not(//cac:ClassifiedTaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'E']))`.
/// Exactly one exempt breakdown entry when E is used anywhere.
fn aligned_ibrp_e_01(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    breakdown_for_used(doc, sink, "E", true);
}

/// `aligned-ibrp-e-05`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(cbc:Percent)`.
fn aligned_ibrp_e_05(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    line_rate(doc, sink, "E", is_absent, false);
}

/// `aligned-ibrp-e-06`, context `cac:AllowanceCharge[cbc:ChargeIndicator=false()]/cac:TaxCategory[normalize-space(cbc:ID)='E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) = 0)`.
fn aligned_ibrp_e_06(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "E", false, is_zero, true);
}

/// `aligned-ibrp-e-07`, context `cac:AllowanceCharge[cbc:ChargeIndicator=true()]/cac:TaxCategory[normalize-space(cbc:ID)='E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) = 0)`.
fn aligned_ibrp_e_07(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "E", true, is_zero, true);
}

/// `aligned-ibrp-e-08`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(exists(//cac:InvoiceLine) and (xs:decimal(../cbc:TaxableAmount) = (sum(../../../cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='E']/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='E']/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='E']/xs:decimal(cbc:Amount))))) or (exists(//cac:CreditNoteLine) and (xs:decimal(../cbc:TaxableAmount) = (sum(../../../cac:CreditNoteLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='E']/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='E']/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='E']/xs:decimal(cbc:Amount)))))`.
fn aligned_ibrp_e_08(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    taxable_is_exact(doc, sink, "E");
}

/// `aligned-ibrp-e-09`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `xs:decimal(../cbc:TaxAmount) = 0`.
fn aligned_ibrp_e_09(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    tax_amount_is_zero(doc, sink, "E");
}

/// `aligned-ibrp-o-01`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `((exists(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'O']) or exists(//cac:ClassifiedTaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'O'])) and (count(cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'O']) = 1)) or (not(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'O']) and not(//cac:ClassifiedTaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'O']))`.
/// Exactly one not-subject breakdown entry when O is used anywhere.
fn aligned_ibrp_o_01(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    breakdown_for_used(doc, sink, "O", true);
}

/// `aligned-ibrp-o-05`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'O'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'O'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(cbc:Percent)`.
fn aligned_ibrp_o_05(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    line_rate(doc, sink, "O", is_absent, false);
}

/// `aligned-ibrp-o-06`, context `cac:AllowanceCharge[cbc:ChargeIndicator=false()]/cac:TaxCategory[normalize-space(cbc:ID)='O'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(cbc:Percent)`.
fn aligned_ibrp_o_06(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "O", false, is_absent, false);
}

/// `aligned-ibrp-o-07`, context `cac:AllowanceCharge[cbc:ChargeIndicator=true()]/cac:TaxCategory[normalize-space(cbc:ID)='O'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(cbc:Percent)`.
fn aligned_ibrp_o_07(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "O", true, is_absent, false);
}

/// `aligned-ibrp-o-08`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'O'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(exists(//cac:InvoiceLine) and (xs:decimal(../cbc:TaxableAmount) = (sum(../../../cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='O']/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='O']/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='O']/xs:decimal(cbc:Amount))))) or (exists(//cac:CreditNoteLine) and (xs:decimal(../cbc:TaxableAmount) = (sum(../../../cac:CreditNoteLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='O']/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='O']/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='O']/xs:decimal(cbc:Amount)))))`.
fn aligned_ibrp_o_08(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    taxable_is_exact(doc, sink, "O");
}

/// `aligned-ibrp-o-09`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'O'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `xs:decimal(../cbc:TaxAmount) = 0`.
fn aligned_ibrp_o_09(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    tax_amount_is_zero(doc, sink, "O");
}

/// `aligned-ibrp-o-11-ae`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'O'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(cbc:Percent)`.
fn aligned_ibrp_o_11_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| s.is("O")) {
        if s.cat.is_some_and(|c| c.rate.exists()) {
            sink.fail(&[s.i]);
        }
    }
}

/// `aligned-ibrp-s-01`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `((count(//cac:AllowanceCharge/cac:TaxCategory[normalize-space(cbc:ID) = 'S']) + count(//cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'S'])) > 0 and count(cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'S']) > 0) or ((count(//cac:AllowanceCharge/cac:TaxCategory[normalize-space(cbc:ID) = 'S']) + count(//cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'S'])) = 0 and count(cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'S']) = 0)`.
/// No scheme test and no count of the breakdown entries' own use: fires when S is used by a
/// document allowance, a document charge or a line but no breakdown entry is S, and when a
/// breakdown entry is S but nothing else is.
fn aligned_ibrp_s_01(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let used = acs(doc).filter(|a| a.cat.code_is("S")).count()
        + line_cats(doc).filter(|n| n.cat.code_is("S")).count();
    let in_breakdown = subs(doc)
        .filter(|s| s.cat.is_some_and(|c| c.code_is("S")))
        .count();
    if (used > 0) != (in_breakdown > 0) {
        sink.fail(&[]);
    }
}

/// `aligned-ibrp-s-05`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'S'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'S'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) > 0)`.
fn aligned_ibrp_s_05(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    line_rate(doc, sink, "S", is_positive, false);
}

/// `aligned-ibrp-s-06`, context `cac:AllowanceCharge[cbc:ChargeIndicator=false()]/cac:TaxCategory[normalize-space(cbc:ID)='S'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) > 0)`.
fn aligned_ibrp_s_06(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "S", false, is_positive, false);
}

/// `aligned-ibrp-s-07`, context `cac:AllowanceCharge[cbc:ChargeIndicator=true()]/cac:TaxCategory[normalize-space(cbc:ID)='S'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) > 0)`.
fn aligned_ibrp_s_07(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "S", true, is_positive, false);
}

/// `aligned-ibrp-s-08`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'S'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal) and (every $rate in xs:decimal(cbc:Percent) satisfies (((exists(//cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID) = 'S'][cac:Item/cac:ClassifiedTaxCategory/xs:decimal(cbc:Percent) =$rate]) or exists(//cac:AllowanceCharge[cac:TaxCategory/normalize-space(cbc:ID)='S'][cac:TaxCategory/xs:decimal(cbc:Percent) = $rate])) and (u:slack(../xs:decimal(cbc:TaxableAmount), sum(../../../cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='S'][cac:Item/cac:ClassifiedTaxCategory/xs:decimal(cbc:Percent) =$rate]/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='S'][cac:TaxCategory/xs:decimal(cbc:Percent) = $rate]/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='S'][cac:TaxCategory/xs:decimal(cbc:Percent) = $rate]/xs:decimal(cbc:Amount)),0.02))) or ((exists(//cac:CreditNoteLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID) = 'S'][cac:Item/cac:ClassifiedTaxCategory/xs:decimal(cbc:Percent) =$rate]) or exists(//cac:AllowanceCharge[cac:TaxCategory/normalize-space(cbc:ID)='S'][cac:TaxCategory/xs:decimal(cbc:Percent) = $rate])) and (u:slack(../xs:decimal(cbc:TaxableAmount), sum(../../../cac:CreditNoteLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='S'][cac:Item/cac:ClassifiedTaxCategory/xs:decimal(cbc:Percent) =$rate]/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='S'][cac:TaxCategory/xs:decimal(cbc:Percent) = $rate]/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='S'][cac:TaxCategory/xs:decimal(cbc:Percent) = $rate]/xs:decimal(cbc:Amount)),0.02)))))`.
/// Both branches of the official test are evaluated, as in the XPath: the `InvoiceLine` branch
/// and the `CreditNoteLine` branch. The branch of the kind the document does not have sees no
/// line (its line sum is 0 and `exists(lines)` is false), but its document allowances and
/// charges are still there, so a document whose taxable amount equals `charges - allowances`
/// passes through it whatever its lines add up to. The branch of the document's own kind needs
/// a matching line or a matching allowance or charge.
fn aligned_ibrp_s_08(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| s.is("S")) {
        let rate = s.cat.and_then(|c| c.rate.value);
        let holds = match (rate, s.taxable.value) {
            (Some(rate), Some(taxable)) => {
                let want = Some(rate);
                let any_line =
                    line_cats(doc).any(|n| n.cat.code_is("S") && rate_is(n.cat.rate, want));
                let any_ac = acs(doc).any(|a| a.cat.code_is("S") && rate_is(a.cat.rate, want));
                let within = |sum: Option<Decimal>| {
                    sum.is_some_and(|value| decimal::slack(taxable, value, SLACK))
                };
                ((any_line || any_ac) && within(net_of(doc, "S", want, true)))
                    || (any_ac && within(net_of(doc, "S", want, false)))
            }
            _ => false,
        };
        if !holds {
            sink.fail(&[s.i]);
        }
    }
}

/// `aligned-ibrp-s-09`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'S'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and ../cbc:TaxableAmount castable as xs:decimal and ../cbc:TaxAmount castable as xs:decimal) and u:slack(abs(xs:decimal(../cbc:TaxAmount)) , round((abs(xs:decimal(../cbc:TaxableAmount)) * (xs:decimal(cbc:Percent) div 100)) * 10 * 10) div 100 ,0.02 )`.
/// All three of rate, taxable amount and tax amount must exist; the tax amount's absolute value
/// is within 0.02 of `round2(abs(taxable) * rate / 100)`.
fn aligned_ibrp_s_09(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| s.is("S")) {
        let holds = match (
            s.cat.and_then(|c| c.rate.value),
            s.taxable.value,
            s.tax.value,
        ) {
            (Some(rate), Some(taxable), Some(tax)) => {
                let expected = decimal::mul(taxable.abs(), rate)
                    .and_then(|product| product.checked_div(Decimal::ONE_HUNDRED))
                    .map(decimal::xpath_round2);
                expected.is_some_and(|e| decimal::slack(tax.abs(), e, SLACK))
            }
            _ => false,
        };
        if !holds {
            sink.fail(&[s.i]);
        }
    }
}

/// `aligned-ibrp-s-10`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'S'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(cbc:TaxExemptionReason) and not(cbc:TaxExemptionReasonCode)`.
/// One finding per breakdown entry, at the reason code when it is written (IBT-121), else at the
/// reason text (IBT-120).
fn aligned_ibrp_s_10(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| s.is("S")) {
        let Some(c) = s.cat else { continue };
        if c.reason_code.is_some() {
            sink.fail(&[s.i]);
        } else if c.reason.is_some() {
            sink.fail_at(format!(
                "tax_breakdown[{}].category.exemption_reason_text",
                s.i
            ))
            .term("IBT-120");
        }
    }
}

/// `aligned-ibrp-z-01`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `((exists(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'Z']) or exists(//cac:ClassifiedTaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'Z'])) and (count(cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'Z']) = 1)) or (not(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'Z']) and not(//cac:ClassifiedTaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']/cbc:ID[normalize-space(.) = 'Z']))`.
/// Exactly one zero-rated breakdown entry when Z is used anywhere.
fn aligned_ibrp_z_01(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    breakdown_for_used(doc, sink, "Z", true);
}

/// `aligned-ibrp-z-05`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) = 0)`.
fn aligned_ibrp_z_05(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    line_rate(doc, sink, "Z", is_zero, true);
}

/// `aligned-ibrp-z-06`, context `cac:AllowanceCharge[cbc:ChargeIndicator=false()]/cac:TaxCategory[normalize-space(cbc:ID)='Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) = 0)`.
fn aligned_ibrp_z_06(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "Z", false, is_zero, true);
}

/// `aligned-ibrp-z-07`, context `cac:AllowanceCharge[cbc:ChargeIndicator=true()]/cac:TaxCategory[normalize-space(cbc:ID)='Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal and xs:decimal(cbc:Percent) = 0)`.
fn aligned_ibrp_z_07(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    ac_rate(doc, sink, "Z", true, is_zero, true);
}

/// `aligned-ibrp-z-08`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(exists(//cac:InvoiceLine) and (xs:decimal(../cbc:TaxableAmount) = (sum(../../../cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='Z']/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='Z']/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='Z']/xs:decimal(cbc:Amount))))) or (exists(//cac:CreditNoteLine) and (xs:decimal(../cbc:TaxableAmount) = (sum(../../../cac:CreditNoteLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)='Z']/xs:decimal(cbc:LineExtensionAmount)) + sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=true()][cac:TaxCategory/normalize-space(cbc:ID)='Z']/xs:decimal(cbc:Amount)) - sum(../../../cac:AllowanceCharge[cbc:ChargeIndicator=false()][cac:TaxCategory/normalize-space(cbc:ID)='Z']/xs:decimal(cbc:Amount)))))`.
fn aligned_ibrp_z_08(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    taxable_is_exact(doc, sink, "Z");
}

/// `aligned-ibrp-z-09`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `xs:decimal(../cbc:TaxAmount) = 0`.
fn aligned_ibrp_z_09(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    tax_amount_is_zero(doc, sink, "Z");
}

/// `ibr-102-ae`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'N'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `(cbc:Percent castable as xs:decimal) and (every $rate in xs:decimal(cbc:Percent) satisfies ((exists(//cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID) = "N"][cac:Item/cac:ClassifiedTaxCategory/xs:decimal(cbc:Percent) =$rate]) and  (u:slack(../xs:decimal(cbc:TaxableAmount), sum(../../../cac:InvoiceLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)="N"][cac:Item/cac:ClassifiedTaxCategory/xs:decimal(cbc:Percent) =$rate]/xs:decimal(cbc:LineExtensionAmount)),0.02))) or (exists(//cac:CreditNoteLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID) = "N"][cac:Item/cac:ClassifiedTaxCategory/xs:decimal(cbc:Percent) =$rate])  and (u:slack(../xs:decimal(cbc:TaxableAmount), sum(../../../cac:CreditNoteLine[cac:Item/cac:ClassifiedTaxCategory/normalize-space(cbc:ID)="N"][cac:Item/cac:ClassifiedTaxCategory/xs:decimal(cbc:Percent) =$rate]/xs:decimal(cbc:LineExtensionAmount)) ,0.02)))))`.
/// ASCII `N` only (defect 1). A rate must exist, some line of code `N` (no scheme test) must
/// have it, and the taxable amount is within 0.02 of the net amounts of those lines. The branch
/// of the other line kind finds no line, so only the document's own kind counts.
fn ibr_102_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| s.is("N")) {
        let rate = s.cat.and_then(|c| c.rate.value);
        let holds = match (rate, s.taxable.value) {
            (Some(rate), Some(taxable)) => {
                let want = Some(rate);
                line_cats(doc).any(|n| n.cat.code_is("N") && rate_is(n.cat.rate, want))
                    && sum_lines(doc, "N", want)
                        .is_some_and(|sum| decimal::slack(taxable, sum, SLACK))
            }
            _ => false,
        };
        if !holds {
            sink.fail(&[s.i]);
        }
    }
}

/// `ibr-103-ae`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `exists(../../../cac:AccountingCustomerParty/cac:Party/cac:PartyTaxScheme/cbc:CompanyID)`.
/// The buyer's `PartyTaxScheme/CompanyID` is written for IBT-048 (`buyer_trn`) and for the
/// buyer's TIN.
fn ibr_103_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let buyer_has_company_id = text(&doc.inv.buyer_trn).is_some()
        || doc
            .inv
            .buyer
            .as_ref()
            .is_some_and(|b| text(&b.tax_registration_identifier).is_some());
    if buyer_has_company_id {
        return;
    }
    for n in line_cats(doc).filter(|n| n.cat.is("AE")) {
        sink.fail(&[n.i]);
    }
}

/// `ibr-105-ae`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `(exists(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))="VAT"]/cbc:ID[normalize-space(.) = "N"]) and (count(//cac:TaxTotal/cac:TaxSubtotal[cac:TaxCategory/cbc:ID='N']) = 1)) or not(exists(//cac:TaxCategory[cac:TaxScheme/normalize-space(upper-case(cbc:ID))="VAT"]/cbc:ID[normalize-space(.) = "N"]))`.
/// Only `cac:TaxCategory` elements count (breakdown entries and document allowances and
/// charges), never a line's category; the count of breakdown entries compares the code without
/// a scheme test.
fn ibr_105_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let used = subs(doc).any(|s| s.is("N")) || acs(doc).any(|a| a.cat.is("N"));
    if used {
        let n = subs(doc)
            .filter(|s| s.cat.is_some_and(|c| c.code_is("N")))
            .count();
        if n != 1 {
            sink.fail(&[]);
        }
    }
}

/// `ibr-108-ae`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'N'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `../cbc:TaxAmount = 0`.
fn ibr_108_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    tax_amount_is_zero(doc, sink, "N");
}

/// `ibr-116-ae`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `not(matches(cbc:ProfileExecutionID, "^[01]{2}1[01]{5}$")) or not((//cac:TaxCategory/cbc:ID | //cac:ClassifiedTaxCategory/cbc:ID)[normalize-space(.) != "N"])`.
/// BTAE-02 flag 3 is the third character of the 8-character code.
fn ibr_116_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.txn(3) && all_codes(doc).any(|c| c != "N") {
        sink.fail(&[]);
    }
}

/// `ibr-119-ae`, context `cac:TaxSubtotal`, test
/// `(cac:TaxCategory/cbc:ID = ("O","E") and not(cac:TaxCategory/cbc:Percent)) or (not(cac:TaxCategory/cbc:ID = ("O","E")) and cac:TaxCategory/cbc:Percent)`.
/// No scheme test. A code `O` or `E` must have no rate, any other entry (a missing category or
/// code included) must have one.
fn ibr_119_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc) {
        let exempt = s.cat.is_some_and(|c| c.code_is("O") || c.code_is("E"));
        let rate = s.cat.is_some_and(|c| c.rate.exists());
        if exempt == rate {
            sink.fail(&[s.i]);
        }
    }
}

/// `ibr-120-ae`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `cbc:Percent = 0`.
fn ibr_120_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| s.is("Z")) {
        if !s.cat.is_some_and(|c| is_zero(c.rate)) {
            sink.fail(&[s.i]).suggest("0");
        }
    }
}

/// `ibr-121-ae`, context `/*/cac:TaxTotal/cac:TaxSubtotal/cac:TaxCategory[normalize-space(cbc:ID) = 'E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(cbc:Percent)`.
fn ibr_121_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for s in subs(doc).filter(|s| s.is("E")) {
        if s.cat.is_some_and(|c| c.rate.exists()) {
            sink.fail(&[s.i]);
        }
    }
}

/// `ibr-122-ae`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `not((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81" or (cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "480") or (every $cat in (//cac:TaxCategory/cbc:ID | //cac:ClassifiedTaxCategory/cbc:ID) satisfies normalize-space($cat) = ("E","O","Z"))`.
/// `normalize-space($cat) = ("E","O","Z")` over every written category code, any scheme.
fn ibr_122_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if matches!(type_code(doc), Some("81" | "480"))
        && all_codes(doc).any(|c| !matches!(c, "E" | "O" | "Z"))
    {
        sink.fail(&[]);
    }
}

/// `ibr-124`, context `/ubl:Invoice/cac:TaxTotal | /cn:CreditNote/cac:Taxtotal`, test
/// `string-length(substring-after(cbc:TaxAmount, '.')) <= 2`.
/// Defect 2: applied to credit notes too. The context `cac:TaxTotal` covers both totals, so the
/// accounting-currency one (IBT-111) is a second context at its own field. The test counts the
/// characters after the first `.` of the written text (`decimal::fraction_digits`).
fn ibr_124(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.vat_amount.exists() && decimal::fraction_digits(doc.vat_amount.raw) > 2 {
        sink.fail(&[]);
    }
    let accounting = &doc.totals.tax_amount_accounting_currency;
    if accounting.exists() && decimal::fraction_digits(accounting.raw) > 2 {
        sink.fail_at("totals.tax_amount_accounting_currency")
            .term("IBT-111");
    }
}

/// The `Dec` fields of [`ibr_126`]: every amount the exporter writes with `currencyID` IBT-005
/// as one of the elements the context lists (`cbc:Amount`, `cbc:BaseAmount`, `cbc:PriceAmount`,
/// `cbc:LineExtensionAmount`, the `LegalMonetaryTotal` amounts). The tax totals, the
/// `ItemPriceExtension` amounts, the accounting-currency total, BTAE-20 and the non-amounts are
/// not.
const CURRENCY_AMOUNTS: &[&str] = &[
    "total_amount",
    "payment_terms[#].amount",
    "allowances_charges[#].amount",
    "allowances_charges[#].base_amount",
    "totals.line_extension_amount",
    "totals.allowance_total_amount",
    "totals.charge_total_amount",
    "totals.tax_exclusive_amount",
    "totals.paid_amount",
    "totals.rounding_amount",
    "totals.payable_amount",
    "lines[#].net_amount",
    "lines[#].allowances_charges[#].amount",
    "lines[#].allowances_charges[#].base_amount",
    "lines[#].price.net_price",
    "lines[#].price.discount",
    "lines[#].price.gross_price",
];

/// `ibr-126`, context `cbc:Amount | cbc:BaseAmount | cbc:PriceAmount | cbc:LineExtensionAmount | cbc:TaxExclusiveAmount | cbc:TaxInclusiveAmount | cbc:AllowanceTotalAmount | cbc:ChargeTotalAmount | cbc:PrepaidAmount | cbc:PayableRoundingAmount | cbc:PayableAmount | cac:TaxTotal[cbc:TaxAmount/@currencyID=/*/cbc:DocumentCurrencyCode]/cbc:TaxAmount | cac:TaxTotal[cbc:TaxAmount/@currencyID=/*/cbc:DocumentCurrencyCode]/cac:TaxSubtotal/cbc:TaxableAmount | cac:TaxTotal[cbc:TaxAmount/@currencyID=/*/cbc:DocumentCurrencyCode]/cac:TaxSubtotal/cbc:TaxAmount`, test
/// `ancestor::cac:ItemPriceExtension or @currencyID = //cbc:DocumentCurrencyCode`.
/// Every `currencyID` the exporter writes is the document currency (IBT-005), on the
/// `ItemPriceExtension` amounts it is `AED` (exempt in the test) and the tax totals are in the
/// context only when their currency equals `DocumentCurrencyCode`. So the test fails exactly for
/// the amounts of [`CURRENCY_AMOUNTS`] when the document has no currency: the attribute is then
/// empty and there is no `DocumentCurrencyCode` to equal. One finding per such amount.
fn ibr_126(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.currency).is_some() {
        return;
    }
    doc.each_decimal(|field, idx, term, dec| {
        if dec.exists() && CURRENCY_AMOUNTS.contains(&field.path) {
            sink.fail_at(fill(field.path, idx)).term(term);
        }
    });
}

/// `string-length(normalize-space(.)) = 10 and starts-with(., "1") and translate(., "0123456789", "") = ""`.
fn is_seller_tin(id: &str) -> bool {
    id.len() == 10 && id.starts_with('1') && id.bytes().all(|b| b.is_ascii_digit())
}

/// `ibr-133-ae`, context `cac:TaxScheme/cbc:ID`, test
/// `. = "VAT" or exists(//cac:AccountingSupplierParty/cac:Party/cac:PartyTaxScheme/cbc:CompanyID[ string-length(normalize-space(.)) = 10 and starts-with(normalize-space(.), "1") and translate(normalize-space(.), "0123456789", "") = ""])`.
/// The seller's `PartyTaxScheme/CompanyID` elements are `seller_trn` and the seller's TIN; when
/// one is a ten-digit identifier starting with 1 the rule holds. Otherwise every `TaxScheme/ID`
/// that is not exactly `VAT` (case-sensitive) fails: the TIN schemes of the seller and the buyer
/// (constant `TIN`), and the written schemes of the breakdown entries, document allowances and
/// charges and lines. The seller's, the buyer's and the representative's VAT schemes are the
/// constant `VAT`.
fn ibr_133_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    let seller_tin = inv
        .seller
        .as_ref()
        .and_then(|s| text(&s.tax_registration_identifier));
    if [text(&inv.seller_trn), seller_tin]
        .into_iter()
        .flatten()
        .any(is_seller_tin)
    {
        return;
    }
    if seller_tin.is_some() {
        sink.fail_at("seller.tax_registration_identifier")
            .term("IBT-032");
    }
    if inv
        .buyer
        .as_ref()
        .is_some_and(|b| text(&b.tax_registration_identifier).is_some())
    {
        sink.fail_at("buyer.tax_registration_identifier")
            .term("IBT-032");
    }
    for s in subs(doc) {
        if s.cat.is_some_and(|c| c.scheme != "VAT") {
            sink.fail(&[s.i]);
        }
    }
    for a in acs(doc).filter(|a| a.cat.scheme != "VAT") {
        sink.fail_at(format!(
            "allowances_charges[{}].tax_category.tax_scheme",
            a.i
        ))
        .term(if a.charge { "IBT-102-1" } else { "IBT-095-1" });
    }
    for n in line_cats(doc).filter(|n| n.cat.scheme != "VAT") {
        sink.fail_at(format!("lines[{}].tax.tax_scheme", n.i))
            .term("IBT-167");
    }
}

/// `ibr-139-ae`, context `cac:TaxCategory/cbc:ID | cac:ClassifiedTaxCategory/cbc:ID`, test
/// `((not(contains(normalize-space(.), ' ')) and contains(' S E O AE Z N ', concat(' ', normalize-space(.), ' '))))`.
/// One finding per written category code that is not in the list (`codelists.tsv`, list
/// `ibr-139-ae`): breakdown entries, document allowances and charges, lines. Defect 1: the list
/// holds ASCII `N`, so U+039D fails.
fn ibr_139_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let known = |code: &str| sets().contains("ibr-139-ae", code);
    for s in subs(doc) {
        if s.cat.and_then(|c| c.code).is_some_and(|c| !known(c)) {
            sink.fail(&[s.i]);
        }
    }
    for a in acs(doc) {
        if a.cat.code.is_some_and(|c| !known(c)) {
            sink.fail_at(format!("allowances_charges[{}].tax_category.code", a.i))
                .term(if a.charge { "IBT-102" } else { "IBT-095" });
        }
    }
    for n in line_cats(doc) {
        if n.cat.code.is_some_and(|c| !known(c)) {
            sink.fail_at(format!("lines[{}].tax.code", n.i))
                .term("IBT-151");
        }
    }
}

/// `ibr-151-ae`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `not(((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "380") or ((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "381")) or exists((cac:InvoiceLine | cac:CreditNoteLine)/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) != "E" and normalize-space(cbc:ID) != "O"])`.
/// A line category without a code counts as "neither E nor O"; no scheme test.
fn ibr_151_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if matches!(type_code(doc), Some("380" | "381"))
        && !line_cats(doc).any(|n| !matches!(n.cat.code, Some("E" | "O")))
    {
        sink.fail(&[]);
    }
}

/// `ibr-162-ae`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `../../cac:ItemPriceExtension/cac:TaxTotal/cbc:TaxAmount = 0`.
/// `cac:ItemPriceExtension` exists with BTAE-10 only and holds BTAE-08, so the comparison with 0
/// needs both amounts.
fn ibr_162_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for n in line_cats(doc).filter(|n| n.cat.is("AE")) {
        if !(n.vat_aed_written() && n.vat_aed.value == Some(Decimal::ZERO)) {
            sink.fail(&[n.i]);
        }
    }
}

/// `ibr-163-ae`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'E'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(../../cac:ItemPriceExtension/cac:TaxTotal/cbc:TaxAmount)`.
fn ibr_163_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for n in line_cats(doc).filter(|n| n.cat.is("E")) {
        if n.vat_aed_written() {
            sink.fail(&[n.i]);
        }
    }
}

/// `ibr-165-ae`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'Z'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `../../cac:ItemPriceExtension/cac:TaxTotal/cbc:TaxAmount = 0`.
fn ibr_165_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for n in line_cats(doc).filter(|n| n.cat.is("Z")) {
        if !(n.vat_aed_written() && n.vat_aed.value == Some(Decimal::ZERO)) {
            sink.fail(&[n.i]);
        }
    }
}

/// `ibr-174-ae`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT'] | cac:CreditNoteLine/cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) = 'AE'][cac:TaxScheme/normalize-space(upper-case(cbc:ID))='VAT']`, test
/// `not(not(exists(../cac:StandardItemIdentification/cbc:ID)) or ../cac:StandardItemIdentification/cbc:ID/@schemeID != "0160")`.
/// The standard identifier is written with its `id`; its scheme attribute is written when
/// present. An absent scheme is accepted (`@schemeID != "0160"` is false on the empty sequence).
fn ibr_174_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for n in line_cats(doc).filter(|n| n.cat.is("AE")) {
        let standard = n.line.item.as_ref().and_then(|it| it.standard_id.as_ref());
        match standard {
            Some(s) if text(&s.id).is_some() => {
                if text(&s.scheme_id).is_some_and(|scheme| scheme != "0160") {
                    sink.fail_at(format!("lines[{}].item.standard_id.scheme_id", n.i))
                        .term("IBT-157-1");
                }
            }
            _ => {
                sink.fail(&[n.i]);
            }
        }
    }
}

/// `ibr-190-ae`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `not(//cac:TaxCategory/cbc:ID = "S") or (every $p in //cac:TaxCategory[cbc:ID = "S"]/cbc:Percent satisfies ($p castable as xs:decimal and xs:decimal($p) = 5.00))`.
/// The test reads `cac:TaxCategory` elements only (breakdown entries, then document allowances
/// and charges) with the code `S` and no scheme test, never a line's category. One finding for
/// the document, at the first rate that is not 5.
fn ibr_190_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let five = Decimal::from(5);
    let wrong = |rate: Dec<'_>| rate.value.is_some_and(|r| r != five);
    if let Some(s) = subs(doc).find(|s| s.cat.is_some_and(|c| c.code_is("S") && wrong(c.rate))) {
        sink.fail(&[s.i]);
    } else if let Some(a) = acs(doc).find(|a| a.cat.code_is("S") && wrong(a.cat.rate)) {
        sink.fail_at(format!("allowances_charges[{}].tax_category.rate", a.i))
            .term(if a.charge { "IBT-103" } else { "IBT-096" });
    }
}

/// `ibr-co-14`, context `/ubl:Invoice/cac:TaxTotal | /cn:CreditNote/cac:Taxtotal`, test
/// `(xs:decimal(child::cbc:TaxAmount)= round((sum(cac:TaxSubtotal/xs:decimal(cbc:TaxAmount)) * 10 * 10)) div 100) or not(cac:TaxSubtotal)`.
/// Defect 2: applied to credit notes too. Only the document-currency `cac:TaxTotal` has
/// breakdown entries (the accounting-currency one passes through `not(cac:TaxSubtotal)`), so the
/// rule is checked when an entry is written: IBT-110 must equal `round2` of the sum of the
/// written IBT-117. The suggestion is that sum.
fn ibr_co_14(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if subs(doc).next().is_none() {
        return;
    }
    let expected = decimal::sum(subs(doc).filter_map(|s| s.tax.value)).map(decimal::xpath_round2);
    if expected.is_none() || doc.vat_amount.value != expected {
        let f = sink.fail(&[]);
        if let Some(e) = expected {
            f.suggest(e);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::catalog::{Family, Status};
    use crate::conformance::{self, apply_patch, examples};
    use crate::export::testing::{export_str, maximal};
    use crate::rule::Finding;
    use crate::ruleset::default_ruleset;
    use serde_json::{Value, json};

    fn example(slug: &str) -> pb::Invoice {
        examples()
            .into_iter()
            .find(|(s, _)| s == slug)
            .unwrap_or_else(|| panic!("no example {slug}"))
            .1
    }

    fn patched(slug: &str, set: Value, remove: &[&str]) -> pb::Invoice {
        let mut inv = example(slug);
        let set = set.as_object().expect("an object").clone();
        let remove: Vec<String> = remove.iter().map(|s| s.to_string()).collect();
        apply_patch(&mut inv, &set, &remove).expect("patch applies");
        inv
    }

    type Check = fn(&Doc<'_>, &mut Sink<'_>);

    /// The findings of `rule` over `inv`, with the path template of its coverage row.
    fn findings(rule: Check, id: &str, inv: &pb::Invoice) -> Vec<Finding> {
        let template = default_ruleset()
            .catalog()
            .get(id)
            .unwrap_or_else(|| panic!("no row {id}"))
            .path;
        let doc = Doc::new(inv);
        let mut sink = Sink::new(template);
        rule(&doc, &mut sink);
        sink.into_findings()
    }

    fn paths(rule: Check, id: &str, inv: &pb::Invoice) -> Vec<String> {
        findings(rule, id, inv)
            .into_iter()
            .map(|f| f.path)
            .collect()
    }

    fn rule_of(id: &str) -> Check {
        RULES
            .iter()
            .find(|r| r.id == id)
            .unwrap_or_else(|| panic!("{id} is not registered"))
            .check
    }

    #[test]
    fn the_registered_rules_are_exactly_the_implemented_rows() {
        let mut registered: Vec<&str> = RULES.iter().map(|r| r.id).collect();
        registered.sort_unstable();
        let rows = |status: Status| -> Vec<&'static str> {
            let mut rows: Vec<&str> = default_ruleset()
                .catalog()
                .entries()
                .iter()
                .filter(|e| e.family == Family::Vat && e.status == status)
                .map(|e| e.rule_id)
                .collect();
            rows.sort_unstable();
            rows
        };
        assert_eq!(registered, rows(Status::Implemented));
        assert_eq!(registered.len(), 56);
        assert_eq!(rows(Status::Structural), ["ibr-sr-32"]);
        assert!(rows(Status::UpstreamNoop).is_empty());
        assert!(rows(Status::Pending).is_empty());
    }

    /// `ibr-187-ae` (the constant `true()`, defect 4) has the context `cac:Item`: it is the
    /// `lines` family's `upstream_noop`, and nothing of the `vat` family.
    #[test]
    fn ibr_187_ae_belongs_to_the_lines_family() {
        let catalog = default_ruleset().catalog();
        let row = catalog.get("ibr-187-ae").expect("listed");
        assert_eq!(
            (row.family, row.status),
            (Family::Lines, Status::UpstreamNoop)
        );
        assert!(RULES.iter().all(|r| r.id != "ibr-187-ae"));
        let upstream = include_str!("../../rulesets/pint-ae-1.0.4/upstream/rules-ae.tsv");
        let line = upstream
            .lines()
            .find(|l| l.starts_with("ibr-187-ae\t"))
            .expect("upstream row");
        let columns: Vec<&str> = line.split('\t').collect();
        assert_eq!((columns[2], columns[3]), ("cac:Item", "true()"));
    }

    #[test]
    fn normalize_space_follows_xpath() {
        assert_eq!(normalize_space("VAT"), "VAT");
        assert_eq!(normalize_space(" V\t A\nT\r "), "V A T");
        assert_eq!(normalize_space("a  b"), "a b");
        assert_eq!(normalize_space(""), "");
        // Only XML white space is collapsed: a no-break space stays.
        assert_eq!(normalize_space("a\u{a0}b"), "a\u{a0}b");
    }

    #[test]
    fn the_scheme_test_is_case_insensitive_and_defaults_to_vat() {
        fn c(scheme: &str) -> Cat<'_> {
            Cat {
                code: Some("S"),
                rate: Dec::ABSENT,
                scheme,
                reason_code: None,
                reason: None,
            }
        }
        assert!(c("VAT").is("S") && c("vat").is("S") && c("Vat").is("S"));
        assert!(!c("GST").is("S") && !c("TIN").is("S"));
        assert!(c("GST").code_is("S"));
        assert!(!c("VAT").is("E"));
        // A category without a written scheme is a VAT category.
        let inv = patched(
            "standard-tax-invoice",
            json!({"lines[0].tax.tax_scheme": ""}),
            &[],
        );
        let doc = Doc::new(&inv);
        assert!(line_cats(&doc).all(|n| n.cat.scheme == "VAT" && n.cat.is("S")));
    }

    /// Upstream defect 1: ASCII `N` is the additional-VAT category and U+039D is an unknown code.
    /// Both sides are tested: the rules that select `N` ignore the Greek letter, and `ibr-139-ae`
    /// reports it while it accepts ASCII `N`.
    #[test]
    fn the_greek_nu_is_not_the_additional_vat_category() {
        let ascii = example("margin-scheme");
        assert!(paths(rule_of("ibr-139-ae"), "ibr-139-ae", &ascii).is_empty());
        let doc = Doc::new(&ascii);
        assert_eq!(breakdown_count(&doc, "N"), 1);
        assert!(used(&doc, "N"));
        assert!(paths(rule_of("ibr-105-ae"), "ibr-105-ae", &ascii).is_empty());

        let greek = patched(
            "margin-scheme",
            json!({"tax_breakdown[0].category.code": "\u{39d}", "lines[0].tax.code": "\u{39d}"}),
            &[],
        );
        let doc = Doc::new(&greek);
        assert_eq!(breakdown_count(&doc, "N"), 0);
        assert!(!used(&doc, "N"));
        assert!(!subs(&doc).any(|s| s.is("N")));
        assert_eq!(
            paths(rule_of("ibr-139-ae"), "ibr-139-ae", &greek),
            ["tax_breakdown[0].category.code", "lines[0].tax.code"]
        );
        // N-category rules do not select it: ibr-102-ae, ibr-108-ae and ibr-105-ae stay silent.
        for id in ["ibr-102-ae", "ibr-108-ae", "ibr-105-ae"] {
            assert!(paths(rule_of(id), id, &greek).is_empty(), "{id}");
        }
        let codes = sets();
        assert!(codes.contains("ibr-139-ae", "N") && !codes.contains("ibr-139-ae", "\u{39d}"));
    }

    /// Upstream defect 2: the platform applies `ibr-co-14` and `ibr-124` to credit notes; the
    /// official run never reports them there, which `conformance/allowlist.tsv` lists by rule id
    /// and document kind, and the two fixtures keep them in `expect`.
    #[test]
    fn defect_2_ibr_124_and_ibr_co_14_apply_to_credit_notes() {
        let allow = include_str!("../../conformance/allowlist.tsv");
        for id in ["ibr-124", "ibr-co-14"] {
            assert!(
                allow
                    .lines()
                    .any(|l| l.starts_with(&format!("{id}\tcredit_note\t"))),
                "{id}"
            );
        }
        let bases = examples();
        let fixtures = conformance::mutations(Family::Vat).unwrap();
        for (fixture, rule, base) in [
            ("ibr-124#3", "ibr-124", "standard-tax-credit-note"),
            ("ibr-co-14#2", "ibr-co-14", "standard-tax-credit-note"),
        ] {
            let m = fixtures.iter().find(|m| m.id == fixture).expect("fixture");
            assert_eq!(m.base, base);
            assert!(m.expect.iter().any(|e| e == rule), "{fixture}");
            let inv = m.apply(&bases).unwrap();
            assert_eq!(Doc::new(&inv).kind, doc::DocKind::CreditNote);
            assert_eq!(paths(rule_of(rule), rule, &inv).len(), 1, "{fixture}");
        }
        // ... and the same two edits on the invoice are the ordinary case.
        let inv = patched(
            "standard-tax-invoice",
            json!({"vat_amount": "532.161"}),
            &[],
        );
        assert_eq!(paths(rule_of("ibr-124"), "ibr-124", &inv), ["vat_amount"]);
        assert_eq!(
            paths(rule_of("ibr-co-14"), "ibr-co-14", &inv),
            ["vat_amount"]
        );
    }

    #[test]
    fn ibr_124_counts_characters_after_the_dot_in_both_tax_totals() {
        let rule = rule_of("ibr-124");
        for ok in ["532", "532.1", "532.16", "-532.16", "+0.00"] {
            let inv = patched("standard-tax-invoice", json!({"vat_amount": ok}), &[]);
            assert!(paths(rule, "ibr-124", &inv).is_empty(), "{ok}");
        }
        // The test is lexical: a trailing zero counts.
        let inv = patched(
            "standard-tax-invoice",
            json!({"vat_amount": "532.160"}),
            &[],
        );
        assert_eq!(paths(rule, "ibr-124", &inv), ["vat_amount"]);

        let inv = patched(
            "exports",
            json!({"totals.tax_amount_accounting_currency": "100.123"}),
            &[],
        );
        let found = findings(rule, "ibr-124", &inv);
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].path, "totals.tax_amount_accounting_currency");
        assert_eq!(found[0].business_term, Some("IBT-111"));
        // Both totals at once: one finding each.
        let inv = patched(
            "exports",
            json!({"vat_amount": "1.234", "totals.tax_amount_accounting_currency": "100.123"}),
            &[],
        );
        assert_eq!(paths(rule, "ibr-124", &inv).len(), 2);
    }

    /// The breakdown, and with it every `TaxSubtotal`, exists only when IBT-110 is written.
    #[test]
    fn a_breakdown_entry_exists_only_with_ibt_110() {
        let inv = patched(
            "zero-rated-supplies",
            json!({"vat_amount": "", "tax_breakdown[0].taxable_amount": ""}),
            &[],
        );
        let doc = Doc::new(&inv);
        assert_eq!(subs(&doc).count(), 0);
        for id in [
            "aligned-ibrp-045",
            "aligned-ibrp-z-08",
            "ibr-co-14",
            "aligned-ibrp-z-01",
        ] {
            let found = paths(rule_of(id), id, &inv);
            // z-01 sees the Z lines but no Z breakdown entry; the others see no entry at all.
            assert_eq!(found.len(), usize::from(id == "aligned-ibrp-z-01"), "{id}");
        }
        let with = example("zero-rated-supplies");
        assert_eq!(subs(&Doc::new(&with)).count(), 1);
    }

    /// `u:slack` raises a type error on a missing taxable amount in the official stylesheet; the
    /// platform reports the entry as failing, and `aligned-ibrp-045` reports the amount.
    #[test]
    fn a_breakdown_entry_without_a_taxable_amount_fails_the_slack_rules() {
        let inv = patched(
            "standard-tax-invoice",
            json!({"tax_breakdown[0].taxable_amount": ""}),
            &[],
        );
        for (id, path) in [
            ("aligned-ibrp-s-08", "tax_breakdown[0].taxable_amount"),
            ("aligned-ibrp-s-09", "tax_breakdown[0].tax_amount"),
            ("aligned-ibrp-045", "tax_breakdown[0].taxable_amount"),
        ] {
            assert_eq!(paths(rule_of(id), id, &inv), [path], "{id}");
        }
        let inv = patched(
            "margin-scheme",
            json!({"tax_breakdown[0].taxable_amount": ""}),
            &[],
        );
        assert_eq!(
            paths(rule_of("ibr-102-ae"), "ibr-102-ae", &inv),
            ["tax_breakdown[0].taxable_amount"]
        );
    }

    #[test]
    fn the_slack_is_two_hundredths_inclusive() {
        let rule = rule_of("aligned-ibrp-s-08");
        let set = |taxable: &str| {
            patched(
                "standard-tax-invoice",
                json!({"tax_breakdown[0].taxable_amount": taxable}),
                &[],
            )
        };
        for ok in ["10643.29", "10643.30", "10643.31", "10643.27", "10643.2900"] {
            assert!(
                paths(rule, "aligned-ibrp-s-08", &set(ok)).is_empty(),
                "{ok}"
            );
        }
        for bad in ["10643.32", "10643.26", "10643.3201", "10000"] {
            assert_eq!(
                paths(rule, "aligned-ibrp-s-08", &set(bad)).len(),
                1,
                "{bad}"
            );
        }
        // s-09: |tax| within 0.02 of round2(|taxable| * rate / 100) = 532.16 (half toward +inf).
        let rule = rule_of("aligned-ibrp-s-09");
        let tax = |t: &str| {
            patched(
                "standard-tax-invoice",
                json!({"tax_breakdown[0].tax_amount": t}),
                &[],
            )
        };
        for ok in ["532.1645", "532.14", "532.18", "-532.18"] {
            assert!(
                paths(rule, "aligned-ibrp-s-09", &tax(ok)).is_empty(),
                "{ok}"
            );
        }
        for bad in ["532.13", "532.19", "-532.19", "0"] {
            assert_eq!(
                paths(rule, "aligned-ibrp-s-09", &tax(bad)).len(),
                1,
                "{bad}"
            );
        }
    }

    /// The official test of `aligned-ibrp-s-08` has a branch for each line kind, and the branch of
    /// the kind the document does not have still sees the document allowances and charges.
    #[test]
    fn s_08_passes_through_the_other_line_kind_branch() {
        // 419.44 (charge) - 262.15 (allowance) = 157.29, whatever the lines add up to.
        for slug in ["standard-tax-credit-note", "standard-tax-invoice"] {
            let inv = patched(
                slug,
                json!({"tax_breakdown[0].taxable_amount": "157.29"}),
                &[],
            );
            assert!(
                paths(rule_of("aligned-ibrp-s-08"), "aligned-ibrp-s-08", &inv).is_empty(),
                "{slug}"
            );
            let inv = patched(
                slug,
                json!({"tax_breakdown[0].taxable_amount": "157.32"}),
                &[],
            );
            assert_eq!(
                paths(rule_of("aligned-ibrp-s-08"), "aligned-ibrp-s-08", &inv).len(),
                1
            );
        }
        // Without a matching allowance or charge the other branch has nothing to match.
        let inv = patched(
            "standard-tax-invoice",
            json!({"tax_breakdown[0].taxable_amount": "0"}),
            &["allowances_charges[1]", "allowances_charges[0]"],
        );
        assert_eq!(
            paths(rule_of("aligned-ibrp-s-08"), "aligned-ibrp-s-08", &inv).len(),
            1
        );
    }

    #[test]
    fn s_08_matches_lines_and_allowances_on_the_breakdown_rate() {
        // Rate 10 on the breakdown and on both allowance and charge, 5 on the line: only the
        // allowance and the charge match the rate, so the lines are left out of the sum.
        let inv = patched(
            "standard-tax-invoice",
            json!({
                "tax_breakdown[0].category.rate": "10",
                "allowances_charges[0].tax_category.rate": "10",
                "allowances_charges[1].tax_category.rate": "10",
                "tax_breakdown[0].taxable_amount": "157.29",
            }),
            &[],
        );
        assert!(paths(rule_of("aligned-ibrp-s-08"), "aligned-ibrp-s-08", &inv).is_empty());
    }

    #[test]
    fn exact_sums_have_no_slack_and_suggest_the_sum() {
        let rule = rule_of("aligned-ibrp-z-08");
        let inv = patched(
            "doc-level-charge-z-category",
            json!({"tax_breakdown[0].taxable_amount": "1200.01"}),
            &[],
        );
        let found = findings(rule, "aligned-ibrp-z-08", &inv);
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].path, "tax_breakdown[0].taxable_amount");
        // 1000 (line) + 200 (charge).
        assert_eq!(found[0].suggested_value.as_deref(), Some("1200"));
        let exact = patched(
            "doc-level-charge-z-category",
            json!({"tax_breakdown[0].taxable_amount": "1200.00"}),
            &[],
        );
        assert!(paths(rule, "aligned-ibrp-z-08", &exact).is_empty());

        // No written line: the official test is false, and there is nothing to suggest.
        let inv = patched("zero-rated-supplies", json!({}), &["lines[0]"]);
        let found = findings(rule, "aligned-ibrp-z-08", &inv);
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].suggested_value, None);
    }

    /// `lines_exist` is the exporter's: a line with no written child is not an element.
    #[test]
    fn lines_exist_follows_the_exporter() {
        let line_elements = |inv: &pb::Invoice| {
            let xml = export_str(inv);
            let tree = roxmltree::Document::parse(&xml).unwrap();
            tree.root_element()
                .children()
                .filter(|n| matches!(n.tag_name().name(), "InvoiceLine" | "CreditNoteLine"))
                .count()
        };
        let mut docs: Vec<pb::Invoice> = vec![
            maximal("380"),
            maximal("381"),
            example("standard-tax-invoice"),
        ];
        for set in [
            json!({}),
            json!({"lines[0].id": "  "}),
            json!({"lines[0].unit_code": "C62"}),
            json!({"lines[0].quantity": "x1"}),
            json!({"lines[0].price.net_price": "bad"}),
            json!({"lines[0].price.base_quantity_unit_code": "C62"}),
            json!({"lines[0].tax.tax_scheme": " "}),
            json!({"lines[0].id": "1"}),
            json!({"lines[0].note": "n"}),
            json!({"lines[0].price.discount": "1"}),
            json!({"lines[0].amount_aed": "1"}),
            json!({"lines[0].vat_amount_aed": "1"}),
            json!({"lines[0].tax.code": "S"}),
            json!({"lines[0].tax.tax_scheme": "VAT"}),
            json!({"lines[0].allowances_charges[0].reason": "r"}),
            json!({"lines[0].item.attributes[0].value": "v"}),
            json!({"lines[0].item.attributes[0].name": "n"}),
            json!({"lines[0].item.classifications[0].scheme_id": "HS"}),
            json!({"lines[0].item.classifications[0].code": "1"}),
            json!({"lines[0].period.start_date": "2024-01-01"}),
            json!({"lines[0].object_identifier.scheme_id": "x"}),
            json!({"lines[0].object_identifier.id": "x"}),
        ] {
            let mut inv = pb::Invoice {
                lines: vec![pb::InvoiceLine::default()],
                ..pb::Invoice::default()
            };
            apply_patch(&mut inv, set.as_object().unwrap(), &[]).unwrap();
            docs.push(inv);
        }
        docs.push(pb::Invoice::default());
        for inv in &docs {
            let doc = Doc::new(inv);
            assert_eq!(
                doc.lines_exist(),
                line_elements(inv) > 0,
                "{:?}",
                inv.lines.first()
            );
        }
    }

    /// `ibr-126`: with no document currency every written amount outside `ItemPriceExtension`
    /// fails, and the findings are exactly the amount elements of the exported XML.
    #[test]
    fn ibr_126_fires_once_per_exported_amount_when_the_currency_is_missing() {
        let rule = rule_of("ibr-126");
        assert!(paths(rule, "ibr-126", &example("standard-tax-invoice")).is_empty());
        assert!(paths(rule, "ibr-126", &maximal("380")).is_empty());
        for (inv, name) in [
            (
                patched("standard-tax-invoice", json!({"currency": ""}), &[]),
                "sti",
            ),
            (
                patched("standard-tax-credit-note", json!({"currency": " "}), &[]),
                "stc",
            ),
            (
                patched("standard-invoice-extensive", json!({"currency": ""}), &[]),
                "sie",
            ),
            (patched("exports", json!({"currency": ""}), &[]), "exports"),
            (
                {
                    let mut m = maximal("380");
                    m.currency.clear();
                    m
                },
                "maximal",
            ),
        ] {
            let xml = export_str(&inv);
            let tree = roxmltree::Document::parse(&xml).unwrap();
            let amounts = tree
                .descendants()
                .filter(|n| {
                    matches!(
                        n.tag_name().name(),
                        "Amount"
                            | "BaseAmount"
                            | "PriceAmount"
                            | "LineExtensionAmount"
                            | "TaxExclusiveAmount"
                            | "TaxInclusiveAmount"
                            | "AllowanceTotalAmount"
                            | "ChargeTotalAmount"
                            | "PrepaidAmount"
                            | "PayableRoundingAmount"
                            | "PayableAmount"
                    ) && !n
                        .ancestors()
                        .any(|a| a.tag_name().name() == "ItemPriceExtension")
                })
                .count();
            let found = findings(rule, "ibr-126", &inv);
            assert!(
                amounts > 0 && found.len() == amounts,
                "{name}: {} vs {amounts}",
                found.len()
            );
            assert!(found.iter().all(|f| f.business_term.is_some()), "{name}");
        }
        let found = findings(
            rule,
            "ibr-126",
            &patched("standard-tax-invoice", json!({"currency": ""}), &[]),
        );
        let first: Vec<_> = found
            .iter()
            .take(3)
            .map(|f| (f.path.as_str(), f.business_term))
            .collect();
        assert_eq!(
            first,
            [
                ("total_amount", Some("IBT-112")),
                ("allowances_charges[0].amount", Some("IBT-092")),
                ("allowances_charges[0].base_amount", Some("IBT-093")),
            ]
        );
        // Charges use their own terms.
        assert!(found.iter().any(
            |f| f.path == "allowances_charges[1].amount" && f.business_term == Some("IBT-099")
        ));
    }

    #[test]
    fn ibr_133_ae_accepts_a_seller_tin_and_rejects_everything_but_exact_vat() {
        let rule = rule_of("ibr-133-ae");
        assert!(paths(rule, "ibr-133-ae", &example("seller-tin-identifier")).is_empty());
        for ok in ["1234567890", "1000000000"] {
            let inv = patched(
                "standard-tax-invoice",
                json!({"seller.tax_registration_identifier": ok, "lines[0].tax.tax_scheme": "GST"}),
                &[],
            );
            assert!(paths(rule, "ibr-133-ae", &inv).is_empty(), "{ok}");
        }
        for bad in [
            "2234567890",
            "123456789",
            "12345678901",
            "123456789a",
            "١234567890",
        ] {
            let inv = patched(
                "standard-tax-invoice",
                json!({"seller.tax_registration_identifier": bad, "lines[0].tax.tax_scheme": "GST"}),
                &[],
            );
            assert_eq!(
                paths(rule, "ibr-133-ae", &inv),
                [
                    "seller.tax_registration_identifier",
                    "lines[0].tax.tax_scheme"
                ],
                "{bad}"
            );
        }
        // A seller VAT identifier of ten digits starting with 1 counts as well.
        let inv = patched(
            "standard-tax-invoice",
            json!({"seller_trn": "1234567890", "lines[0].tax.tax_scheme": "GST"}),
            &[],
        );
        assert!(paths(rule, "ibr-133-ae", &inv).is_empty());
        // Without that, one finding per scheme that is not exactly VAT.
        let inv = patched(
            "standard-tax-invoice",
            json!({
                "tax_breakdown[0].category.tax_scheme": "vat",
                "allowances_charges[0].tax_category.tax_scheme": "GST",
                "allowances_charges[1].tax_category.tax_scheme": "GST",
                "lines[0].tax.tax_scheme": " Vat ",
                "buyer.tax_registration_identifier": "1234567890",
            }),
            &[],
        );
        let found = findings(rule, "ibr-133-ae", &inv);
        let got: Vec<_> = found
            .iter()
            .map(|f| (f.path.as_str(), f.business_term))
            .collect();
        assert_eq!(
            got,
            [
                ("buyer.tax_registration_identifier", Some("IBT-032")),
                ("tax_breakdown[0].category.tax_scheme", None),
                (
                    "allowances_charges[0].tax_category.tax_scheme",
                    Some("IBT-095-1")
                ),
                (
                    "allowances_charges[1].tax_category.tax_scheme",
                    Some("IBT-102-1")
                ),
                ("lines[0].tax.tax_scheme", Some("IBT-167")),
            ]
        );
    }

    #[test]
    fn ibr_139_ae_reports_each_context_at_its_own_field() {
        let inv = patched(
            "standard-tax-invoice",
            json!({
                "tax_breakdown[0].category.code": "X",
                "allowances_charges[0].tax_category.code": "s",
                "allowances_charges[1].tax_category.code": "AE ",
                "lines[0].tax.code": "S E",
            }),
            &[],
        );
        let found = findings(rule_of("ibr-139-ae"), "ibr-139-ae", &inv);
        let got: Vec<_> = found
            .iter()
            .map(|f| (f.path.as_str(), f.business_term))
            .collect();
        // "AE " is trimmed by the exporter and passes.
        assert_eq!(
            got,
            [
                ("tax_breakdown[0].category.code", None),
                ("allowances_charges[0].tax_category.code", Some("IBT-095")),
                ("lines[0].tax.code", Some("IBT-151")),
            ]
        );
    }

    #[test]
    fn rules_that_read_tax_categories_only_ignore_the_lines() {
        // ibr-190-ae and ibr-105-ae read cac:TaxCategory, never a line's ClassifiedTaxCategory.
        let inv = patched(
            "standard-tax-invoice",
            json!({"lines[0].tax.rate": "6"}),
            &[],
        );
        assert!(paths(rule_of("ibr-190-ae"), "ibr-190-ae", &inv).is_empty());
        let inv = patched("margin-scheme", json!({}), &["tax_breakdown[0]"]);
        assert!(paths(rule_of("ibr-105-ae"), "ibr-105-ae", &inv).is_empty());
        // ... and the breakdown count of ibr-105-ae has no scheme test.
        let inv = patched(
            "margin-scheme",
            json!({"tax_breakdown[0].category.tax_scheme": "GST"}),
            &[],
        );
        assert!(paths(rule_of("ibr-105-ae"), "ibr-105-ae", &inv).is_empty());
        let found = findings(
            rule_of("ibr-190-ae"),
            "ibr-190-ae",
            &patched(
                "standard-tax-invoice",
                json!({"allowances_charges[1].tax_category.rate": "6"}),
                &[],
            ),
        );
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].path, "allowances_charges[1].tax_category.rate");
        assert_eq!(found[0].business_term, Some("IBT-103"));
    }

    #[test]
    fn aligned_ibrp_s_01_has_no_scheme_test_and_counts_both_directions() {
        let rule = rule_of("aligned-ibrp-s-01");
        let inv = patched(
            "standard-tax-invoice",
            json!({"tax_breakdown[0].category.tax_scheme": "GST"}),
            &[],
        );
        assert!(paths(rule, "aligned-ibrp-s-01", &inv).is_empty());
        let inv = patched("standard-tax-invoice", json!({}), &["tax_breakdown[0]"]);
        assert_eq!(
            paths(rule, "aligned-ibrp-s-01", &inv),
            ["tax_breakdown[0].category.code"]
        );
        let inv = patched(
            "zero-rated-supplies",
            json!({"tax_breakdown[1].category.code": "S", "tax_breakdown[1].category.rate": "5"}),
            &[],
        );
        assert_eq!(paths(rule, "aligned-ibrp-s-01", &inv).len(), 1);
    }

    #[test]
    fn category_rules_without_a_scheme_test_see_other_schemes() {
        // ibr-119-ae, ibr-122-ae, ibr-116-ae and ibr-151-ae compare the code only.
        let inv = patched(
            "doc-level-allowance-e-category",
            json!({"tax_breakdown[0].category.rate": "0", "tax_breakdown[0].category.tax_scheme": "GST"}),
            &[],
        );
        assert_eq!(
            paths(rule_of("ibr-119-ae"), "ibr-119-ae", &inv),
            ["tax_breakdown[0].category.rate"]
        );
        // ... while ibr-121-ae (VAT scheme) no longer selects it.
        assert!(paths(rule_of("ibr-121-ae"), "ibr-121-ae", &inv).is_empty());
        let inv = patched(
            "standard-tax-invoice",
            json!({"invoice_type_code": "480", "lines[0].tax.tax_scheme": "GST"}),
            &[],
        );
        assert_eq!(
            paths(rule_of("ibr-122-ae"), "ibr-122-ae", &inv),
            ["invoice_type_code"]
        );
        // 480 with only E, O, Z passes whatever the scheme.
        let inv = patched(
            "commercial-invoice",
            json!({"lines[0].tax.tax_scheme": "GST"}),
            &[],
        );
        assert!(paths(rule_of("ibr-122-ae"), "ibr-122-ae", &inv).is_empty());
        // ibr-151-ae: a line category without a code counts as "neither E nor O".
        let inv = patched(
            "doc-level-allowance-e-category",
            json!({"invoice_type_code": "380", "lines[0].tax.code": ""}),
            &[],
        );
        assert!(paths(rule_of("ibr-151-ae"), "ibr-151-ae", &inv).is_empty());
    }

    #[test]
    fn value_fixes_carry_their_value() {
        let one = |rule: &str, inv: &pb::Invoice| -> Option<String> {
            findings(rule_of(rule), rule, inv)
                .into_iter()
                .next()
                .and_then(|f| f.suggested_value)
        };
        let inv = patched(
            "supply-under-reverse-charge-mechanism",
            json!({"tax_breakdown[0].tax_amount": "9"}),
            &[],
        );
        assert_eq!(one("aligned-ibrp-ae-09-ae", &inv).as_deref(), Some("0.00"));
        let inv = patched(
            "doc-level-allowance-ae-category",
            json!({"allowances_charges[0].tax_category.rate": "5"}),
            &[],
        );
        assert_eq!(one("aligned-ibrp-ae-06", &inv).as_deref(), Some("0"));
        let inv = patched(
            "zero-rated-supplies",
            json!({"lines[0].tax.rate": "5", "tax_breakdown[0].category.rate": ""}),
            &[],
        );
        assert_eq!(one("aligned-ibrp-z-05", &inv).as_deref(), Some("0"));
        assert_eq!(one("ibr-120-ae", &inv).as_deref(), Some("0"));
        let inv = patched(
            "margin-scheme",
            json!({"tax_breakdown[0].tax_amount": "5"}),
            &[],
        );
        assert_eq!(one("ibr-108-ae", &inv).as_deref(), Some("0.00"));
        // ibr-co-14: the suggestion is round2 of the sum (532.1645 -> 532.16).
        let inv = patched("standard-tax-invoice", json!({"vat_amount": "1"}), &[]);
        assert_eq!(one("ibr-co-14", &inv).as_deref(), Some("532.16"));
        let inv = patched(
            "standard-invoice-extensive",
            json!({"tax_breakdown[0].tax_amount": "115.705"}),
            &[],
        );
        assert_eq!(one("ibr-co-14", &inv).as_deref(), Some("115.71"));
        // Each of these rows says `value`; the others suggest nothing.
        for e in default_ruleset().catalog().entries() {
            if e.family == Family::Vat && e.status == Status::Implemented {
                let rule = rule_of(e.rule_id);
                for inv in [
                    example("standard-tax-invoice"),
                    maximal("380"),
                    inv_all_wrong(),
                ] {
                    for f in findings(rule, e.rule_id, &inv) {
                        assert!(
                            f.suggested_value.is_none() || e.fix == crate::catalog::Fix::Value,
                            "{}",
                            e.rule_id
                        );
                    }
                }
            }
        }
    }

    /// A document that violates most of the family at once.
    fn inv_all_wrong() -> pb::Invoice {
        let mut inv = maximal("380");
        inv.currency.clear();
        inv.vat_amount = "1.2345".into();
        inv
    }

    #[test]
    fn every_finding_path_is_a_valid_canonical_path() {
        use std::collections::BTreeSet;
        // Every path a rule reports, over the corpus, the maximal documents and mutations, is in
        // the CI section 12 grammar and names a string or bool field.
        let mut bases: Vec<pb::Invoice> = examples().into_iter().map(|(_, i)| i).collect();
        bases.push(maximal("380"));
        bases.push(maximal("381"));
        bases.push(inv_all_wrong());
        let fixtures = conformance::mutations(Family::Vat).unwrap();
        let all = examples();
        bases.extend(fixtures.iter().map(|m| m.apply(&all).unwrap()));
        let mut seen = BTreeSet::new();
        for inv in &bases {
            for r in RULES {
                for f in findings(r.check, r.id, inv) {
                    let template: String = f
                        .path
                        .split('.')
                        .map(|seg| match seg.split_once('[') {
                            Some((name, _)) => format!("{name}[#]"),
                            None => seg.to_string(),
                        })
                        .collect::<Vec<_>>()
                        .join(".");
                    crate::rule::check_template(&template)
                        .unwrap_or_else(|e| panic!("{}: {}: {e}", r.id, f.path));
                    seen.insert((r.id, template));
                }
            }
        }
        assert!(seen.len() > 56);
    }

    fn named<'a, 'b>(n: roxmltree::Node<'a, 'b>, name: &str) -> Vec<roxmltree::Node<'a, 'b>> {
        n.children()
            .filter(|c| c.tag_name().name() == name)
            .collect()
    }

    /// `ibr-sr-32` is `structural`: every breakdown entry has one category, and a category has at
    /// most one `TaxExemptionReason`, however the breakdown is filled in.
    #[test]
    fn structural_rules_hold_on_every_exported_breakdown() {
        let degenerate = patched(
            "standard-tax-invoice",
            json!({
                "tax_breakdown[0].category.exemption_reason_text": "reason",
                "tax_breakdown[0].category.exemption_reason_code": "VATEX",
                "tax_breakdown[1].category.exemption_reason_text": "second",
            }),
            &[],
        );
        let docs = [
            maximal("380"),
            maximal("381"),
            example("standard-invoice-extensive"),
            example("standard-tax-credit-note"),
            degenerate,
        ];
        for inv in &docs {
            let xml = export_str(inv);
            let tree = roxmltree::Document::parse(&xml).expect("well-formed");
            let mut entries = 0;
            for sub in tree
                .descendants()
                .filter(|n| n.tag_name().name() == "TaxSubtotal")
            {
                entries += 1;
                let categories = named(sub, "TaxCategory");
                assert!(categories.len() <= 1);
                for c in categories {
                    assert!(named(c, "TaxExemptionReason").len() <= 1);
                }
            }
            assert!(entries > 0);
        }
    }
}
