//! The normalised document view (spec 5.2.3), built once per validation from a `pb::Invoice`. It
//! is the only thing rules and the exporter read, which fixes the equivalence "rule over the
//! canonical invoice = official schematron over our XML" (CI rule 13):
//!
//! * strings are trimmed and empty means absent ([`text`]); the exporter writes the trimmed value;
//! * the defaults the exporter writes are visible here (IBT-023, IBT-024, tax scheme `VAT`, price
//!   `ChargeIndicator` false), so rules never see the empty values;
//! * every decimal field is parsed once into a [`Dec`]; an invalid one is absent for every rule
//!   except `AE-FMT-001`, which reports it.

use rust_decimal::Decimal;

use crate::decimal;
use crate::pb;

/// IBT-023 written when `process.business_process_type` is empty.
pub const DEFAULT_BUSINESS_PROCESS_TYPE: &str = "urn:peppol:bis:billing";
/// IBT-024 written when `process.specification_identifier` is empty.
pub const DEFAULT_SPECIFICATION_IDENTIFIER: &str = "urn:peppol:pint:billing-1@ae-1";
/// `cac:TaxScheme/cbc:ID` written when a tax category's `tax_scheme` is empty.
pub const DEFAULT_TAX_SCHEME: &str = "VAT";
/// The price-level `cac:AllowanceCharge/cbc:ChargeIndicator` the exporter always writes (the
/// canonical price discount is an allowance).
pub const PRICE_CHARGE_INDICATOR: bool = false;

/// `s` trimmed (Unicode whitespace at both ends), `None` when nothing is left (CI rule 4).
pub fn text(s: &str) -> Option<&str> {
    let t = s.trim();
    (!t.is_empty()).then_some(t)
}

/// The tax scheme of a tax category with the exporter's default applied.
pub fn tax_scheme(category: &pb::TaxCategory) -> &str {
    text(&category.tax_scheme).unwrap_or(DEFAULT_TAX_SCHEME)
}

/// UBL root element the exporter writes (CI rule 8).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum DocKind {
    Invoice,
    CreditNote,
}

impl DocKind {
    /// `381` and `81` are credit notes; anything else, including an empty code, is an invoice.
    pub fn from_type_code(code: &str) -> Self {
        match text(code) {
            Some("381" | "81") => DocKind::CreditNote,
            _ => DocKind::Invoice,
        }
    }
}

/// A decimal field: its trimmed text and its value. `value` is `None` when the field is absent
/// or breaks the grammar of [`decimal::parse`].
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Dec<'a> {
    pub raw: &'a str,
    pub value: Option<Decimal>,
}

impl<'a> Dec<'a> {
    pub const ABSENT: Dec<'static> = Dec {
        raw: "",
        value: None,
    };

    pub fn new(s: &'a str) -> Self {
        match text(s) {
            None => Dec::default(),
            Some(raw) => Dec {
                raw,
                value: decimal::parse(raw).ok(),
            },
        }
    }

    /// XPath "the node exists": false for absent and for invalid values.
    pub fn exists(&self) -> bool {
        self.value.is_some()
    }

    /// XPath effective boolean value of `xs:decimal(x)`: false when absent, invalid or zero.
    pub fn ebv(&self) -> bool {
        decimal::ebv(self.value)
    }

    /// Non-empty text that is not a decimal (`AE-FMT-001`).
    pub fn is_invalid(&self) -> bool {
        !self.raw.is_empty() && self.value.is_none()
    }
}

/// One entry of [`Doc::DECIMAL_FIELDS`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct DecimalField {
    /// Path template (CI section 12 grammar, `#` per index).
    pub path: &'static str,
    /// Business term; for an allowance or charge field, the allowance's term.
    pub term: &'static str,
    /// Business term when the enclosing allowance or charge is a charge.
    pub charge_term: Option<&'static str>,
}

const fn field(path: &'static str, term: &'static str) -> DecimalField {
    DecimalField {
        path,
        term,
        charge_term: None,
    }
}

const fn ac_field(
    path: &'static str,
    allowance: &'static str,
    charge: &'static str,
) -> DecimalField {
    DecimalField {
        path,
        term: allowance,
        charge_term: Some(charge),
    }
}

/// The decimals of one document- or line-level allowance or charge.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct AllowanceChargeDec<'a> {
    pub amount: Dec<'a>,
    pub base_amount: Dec<'a>,
    pub percentage: Dec<'a>,
    /// `tax_category.rate`; always absent on a line, where the exporter writes no tax category.
    pub rate: Dec<'a>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct PaymentTermsDec<'a> {
    pub amount: Dec<'a>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct TotalsDec<'a> {
    pub line_extension_amount: Dec<'a>,
    pub allowance_total_amount: Dec<'a>,
    pub charge_total_amount: Dec<'a>,
    pub tax_exclusive_amount: Dec<'a>,
    pub paid_amount: Dec<'a>,
    pub rounding_amount: Dec<'a>,
    pub payable_amount: Dec<'a>,
    pub tax_amount_accounting_currency: Dec<'a>,
    pub total_with_tax_aed: Dec<'a>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct TaxSubtotalDec<'a> {
    pub taxable_amount: Dec<'a>,
    pub tax_amount: Dec<'a>,
    /// `category.rate`.
    pub rate: Dec<'a>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct PriceDec<'a> {
    pub net_price: Dec<'a>,
    pub discount: Dec<'a>,
    pub gross_price: Dec<'a>,
    pub base_quantity: Dec<'a>,
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct LineDec<'a> {
    pub quantity: Dec<'a>,
    pub net_amount: Dec<'a>,
    pub amount_aed: Dec<'a>,
    pub vat_amount_aed: Dec<'a>,
    pub allowances_charges: Vec<AllowanceChargeDec<'a>>,
    pub price: PriceDec<'a>,
    /// `tax.rate`.
    pub rate: Dec<'a>,
}

/// The normalised view of one invoice or credit note. `inv` is the canonical message; the decimal
/// tables mirror its repeated fields index for index (`lines[i]` is `inv.lines[i]`).
#[derive(Debug, Clone)]
pub struct Doc<'a> {
    pub inv: &'a pb::Invoice,
    pub kind: DocKind,
    pub total_amount: Dec<'a>,
    pub vat_amount: Dec<'a>,
    pub exchange_rate: Dec<'a>,
    /// `references.contract_value`.
    pub contract_value: Dec<'a>,
    pub payment_terms: Vec<PaymentTermsDec<'a>>,
    pub allowances_charges: Vec<AllowanceChargeDec<'a>>,
    pub totals: TotalsDec<'a>,
    pub tax_breakdown: Vec<TaxSubtotalDec<'a>>,
    pub lines: Vec<LineDec<'a>>,
}

impl<'a> Doc<'a> {
    /// Every decimal field of the canonical invoice (spec 5.2.2), in proto order.
    pub const DECIMAL_FIELDS: &'static [DecimalField] = &[
        field("total_amount", "IBT-112"),
        field("vat_amount", "IBT-110"),
        field("exchange_rate", "BTAE-04"),
        field("references.contract_value", "BTAE-05"),
        field("payment_terms[#].amount", "IBT-176"),
        ac_field("allowances_charges[#].amount", "IBT-092", "IBT-099"),
        ac_field("allowances_charges[#].base_amount", "IBT-093", "IBT-100"),
        ac_field("allowances_charges[#].percentage", "IBT-094", "IBT-101"),
        ac_field(
            "allowances_charges[#].tax_category.rate",
            "IBT-096",
            "IBT-103",
        ),
        field("totals.line_extension_amount", "IBT-106"),
        field("totals.allowance_total_amount", "IBT-107"),
        field("totals.charge_total_amount", "IBT-108"),
        field("totals.tax_exclusive_amount", "IBT-109"),
        field("totals.paid_amount", "IBT-113"),
        field("totals.rounding_amount", "IBT-114"),
        field("totals.payable_amount", "IBT-115"),
        field("totals.tax_amount_accounting_currency", "IBT-111"),
        field("totals.total_with_tax_aed", "BTAE-20"),
        field("tax_breakdown[#].taxable_amount", "IBT-116"),
        field("tax_breakdown[#].tax_amount", "IBT-117"),
        field("tax_breakdown[#].category.rate", "IBT-119"),
        field("lines[#].quantity", "IBT-129"),
        field("lines[#].net_amount", "IBT-131"),
        field("lines[#].amount_aed", "BTAE-10"),
        field("lines[#].vat_amount_aed", "BTAE-08"),
        ac_field(
            "lines[#].allowances_charges[#].amount",
            "IBT-136",
            "IBT-141",
        ),
        ac_field(
            "lines[#].allowances_charges[#].base_amount",
            "IBT-137",
            "IBT-142",
        ),
        ac_field(
            "lines[#].allowances_charges[#].percentage",
            "IBT-138",
            "IBT-143",
        ),
        field("lines[#].price.net_price", "IBT-146"),
        field("lines[#].price.discount", "IBT-147"),
        field("lines[#].price.gross_price", "IBT-148"),
        field("lines[#].price.base_quantity", "IBT-149"),
        field("lines[#].tax.rate", "IBT-152"),
    ];

    pub fn new(inv: &'a pb::Invoice) -> Self {
        let totals = inv
            .totals
            .as_ref()
            .map_or_else(TotalsDec::default, |t| TotalsDec {
                line_extension_amount: Dec::new(&t.line_extension_amount),
                allowance_total_amount: Dec::new(&t.allowance_total_amount),
                charge_total_amount: Dec::new(&t.charge_total_amount),
                tax_exclusive_amount: Dec::new(&t.tax_exclusive_amount),
                paid_amount: Dec::new(&t.paid_amount),
                rounding_amount: Dec::new(&t.rounding_amount),
                payable_amount: Dec::new(&t.payable_amount),
                tax_amount_accounting_currency: Dec::new(&t.tax_amount_accounting_currency),
                total_with_tax_aed: Dec::new(&t.total_with_tax_aed),
            });
        Doc {
            inv,
            kind: DocKind::from_type_code(&inv.invoice_type_code),
            total_amount: Dec::new(&inv.total_amount),
            vat_amount: Dec::new(&inv.vat_amount),
            exchange_rate: Dec::new(&inv.exchange_rate),
            contract_value: inv
                .references
                .as_ref()
                .map_or(Dec::ABSENT, |r| Dec::new(&r.contract_value)),
            payment_terms: inv
                .payment_terms
                .iter()
                .map(|p| PaymentTermsDec {
                    amount: Dec::new(&p.amount),
                })
                .collect(),
            allowances_charges: inv
                .allowances_charges
                .iter()
                .map(|a| AllowanceChargeDec {
                    rate: rate(a.tax_category.as_ref()),
                    ..allowance_charge(a)
                })
                .collect(),
            totals,
            tax_breakdown: inv
                .tax_breakdown
                .iter()
                .map(|t| TaxSubtotalDec {
                    taxable_amount: Dec::new(&t.taxable_amount),
                    tax_amount: Dec::new(&t.tax_amount),
                    rate: rate(t.category.as_ref()),
                })
                .collect(),
            lines: inv.lines.iter().map(line).collect(),
        }
    }

    /// IBT-023 with the exporter's default.
    pub fn business_process_type(&self) -> &'a str {
        self.inv
            .process
            .as_ref()
            .and_then(|p| text(&p.business_process_type))
            .unwrap_or(DEFAULT_BUSINESS_PROCESS_TYPE)
    }

    /// IBT-024 with the exporter's default.
    pub fn specification_identifier(&self) -> &'a str {
        self.inv
            .process
            .as_ref()
            .and_then(|p| text(&p.specification_identifier))
            .unwrap_or(DEFAULT_SPECIFICATION_IDENTIFIER)
    }

    /// BTAE-02 flag at position `pos` (1 = free trade zone ... 8 = exports). Every flag is false
    /// unless the code matches `^[01]{8}$`, as XPath `matches(cbc:ProfileExecutionID, ...)` is.
    pub fn txn(&self, pos: usize) -> bool {
        let Some(code) = text(&self.inv.transaction_type_code) else {
            return false;
        };
        let flags = code.as_bytes();
        flags.len() == 8
            && flags.iter().all(|c| matches!(c, b'0' | b'1'))
            && (1..=8).contains(&pos)
            && flags[pos - 1] == b'1'
    }

    /// Visits every non-empty decimal field (valid or not), element by element and in
    /// [`Self::DECIMAL_FIELDS`] order within an element: `f(field, indices, business term, value)`.
    /// The term is the charge term when the enclosing allowance or charge is a charge.
    pub fn each_decimal(
        &self,
        mut f: impl FnMut(&'static DecimalField, &[usize], &'static str, &Dec<'a>),
    ) {
        let fields = Self::DECIMAL_FIELDS;
        let mut visit = |k: usize, idx: &[usize], charge: bool, dec: &Dec<'a>| {
            if dec.raw.is_empty() {
                return;
            }
            let field = &fields[k];
            let term = match field.charge_term {
                Some(term) if charge => term,
                _ => field.term,
            };
            f(field, idx, term, dec);
        };
        visit(0, &[], false, &self.total_amount);
        visit(1, &[], false, &self.vat_amount);
        visit(2, &[], false, &self.exchange_rate);
        visit(3, &[], false, &self.contract_value);
        for (i, p) in self.payment_terms.iter().enumerate() {
            visit(4, &[i], false, &p.amount);
        }
        for (i, (a, src)) in self
            .allowances_charges
            .iter()
            .zip(&self.inv.allowances_charges)
            .enumerate()
        {
            visit(5, &[i], src.is_charge, &a.amount);
            visit(6, &[i], src.is_charge, &a.base_amount);
            visit(7, &[i], src.is_charge, &a.percentage);
            visit(8, &[i], src.is_charge, &a.rate);
        }
        let t = &self.totals;
        for (k, dec) in [
            &t.line_extension_amount,
            &t.allowance_total_amount,
            &t.charge_total_amount,
            &t.tax_exclusive_amount,
            &t.paid_amount,
            &t.rounding_amount,
            &t.payable_amount,
            &t.tax_amount_accounting_currency,
            &t.total_with_tax_aed,
        ]
        .into_iter()
        .enumerate()
        {
            visit(9 + k, &[], false, dec);
        }
        for (i, s) in self.tax_breakdown.iter().enumerate() {
            visit(18, &[i], false, &s.taxable_amount);
            visit(19, &[i], false, &s.tax_amount);
            visit(20, &[i], false, &s.rate);
        }
        for (i, (l, src)) in self.lines.iter().zip(&self.inv.lines).enumerate() {
            visit(21, &[i], false, &l.quantity);
            visit(22, &[i], false, &l.net_amount);
            visit(23, &[i], false, &l.amount_aed);
            visit(24, &[i], false, &l.vat_amount_aed);
            for (j, (a, a_src)) in l
                .allowances_charges
                .iter()
                .zip(&src.allowances_charges)
                .enumerate()
            {
                visit(25, &[i, j], a_src.is_charge, &a.amount);
                visit(26, &[i, j], a_src.is_charge, &a.base_amount);
                visit(27, &[i, j], a_src.is_charge, &a.percentage);
            }
            visit(28, &[i], false, &l.price.net_price);
            visit(29, &[i], false, &l.price.discount);
            visit(30, &[i], false, &l.price.gross_price);
            visit(31, &[i], false, &l.price.base_quantity);
            visit(32, &[i], false, &l.rate);
        }
    }

    /// Whether `cac:InvoiceLine` / `cac:CreditNoteLine` number `i` is written. The exporter
    /// drops an aggregate without a written child (`export::B::line`), so a model line with,
    /// say, only a unit code (an attribute of the absent quantity) is no element at all: no line
    /// context of the official schematron sees it, and alone it leaves the invoice without a
    /// line (`ibr-016`).
    pub fn line_written(&self, i: usize) -> bool {
        match (self.inv.lines.get(i), self.lines.get(i)) {
            (Some(l), Some(d)) => line_written(l, d),
            _ => false,
        }
    }

    /// `exists(cac:InvoiceLine) or exists(cac:CreditNoteLine)`: some line is written.
    pub fn lines_exist(&self) -> bool {
        (0..self.inv.lines.len()).any(|i| self.line_written(i))
    }
}

/// Whether a `cac:TaxCategory` / `cac:ClassifiedTaxCategory` is written: any of its code, rate
/// (a valid decimal), exemption reason code or text, or tax scheme is present
/// (`export::tax_category`).
pub fn tax_category_written(c: Option<&pb::TaxCategory>, rate: Dec<'_>) -> bool {
    c.is_some_and(|c| {
        text(&c.code).is_some()
            || rate.exists()
            || text(&c.exemption_reason_code).is_some()
            || text(&c.exemption_reason_text).is_some()
            || text(&c.tax_scheme).is_some()
    })
}

/// [`Doc::line_written`] for one line and its decimals: any child of the line is written.
fn line_written(l: &pb::InvoiceLine, d: &LineDec<'_>) -> bool {
    let classified = |list: &[pb::Classification]| list.iter().any(|c| text(&c.code).is_some());
    text(&l.id).is_some()
        || text(&l.note).is_some()
        || d.quantity.exists()
        || d.net_amount.exists()
        || text(&l.accounting_reference).is_some()
        || l.period
            .as_ref()
            .is_some_and(|p| text(&p.start_date).is_some() || text(&p.end_date).is_some())
        || text(&l.order_reference).is_some()
        || text(&l.order_line_reference).is_some()
        || text(&l.despatch_advice_reference).is_some()
        || l.object_identifier
            .as_ref()
            .is_some_and(|o| text(&o.id).is_some())
        || !l.allowances_charges.is_empty()
        || tax_category_written(l.tax.as_ref(), d.rate)
        || text(&l.batch_number).is_some()
        || l.item.as_ref().is_some_and(|it| {
            text(&it.description).is_some()
                || text(&it.name).is_some()
                || text(&it.buyer_item_id).is_some()
                || text(&it.seller_item_id).is_some()
                || it
                    .standard_id
                    .as_ref()
                    .is_some_and(|s| text(&s.id).is_some())
                || classified(&it.service_accounting_codes)
                || text(&it.origin_country).is_some()
                || text(&it.goods_service_type).is_some()
                || text(&it.item_type).is_some()
                || classified(&it.classifications)
                || it.attributes.iter().any(|a| text(&a.name).is_some())
        })
        || d.price.net_price.exists()
        || d.price.base_quantity.exists()
        || d.price.discount.exists()
        || d.price.gross_price.exists()
        || d.amount_aed.exists()
}

fn rate(category: Option<&pb::TaxCategory>) -> Dec<'_> {
    category.map_or(Dec::ABSENT, |c| Dec::new(&c.rate))
}

/// The decimals of an allowance or charge without its tax rate (a line-level one has none).
fn allowance_charge(a: &pb::AllowanceCharge) -> AllowanceChargeDec<'_> {
    AllowanceChargeDec {
        amount: Dec::new(&a.amount),
        base_amount: Dec::new(&a.base_amount),
        percentage: Dec::new(&a.percentage),
        rate: Dec::ABSENT,
    }
}

fn line(l: &pb::InvoiceLine) -> LineDec<'_> {
    LineDec {
        quantity: Dec::new(&l.quantity),
        net_amount: Dec::new(&l.net_amount),
        amount_aed: Dec::new(&l.amount_aed),
        vat_amount_aed: Dec::new(&l.vat_amount_aed),
        allowances_charges: l.allowances_charges.iter().map(allowance_charge).collect(),
        price: l
            .price
            .as_ref()
            .map_or_else(PriceDec::default, |p| PriceDec {
                net_price: Dec::new(&p.net_price),
                discount: Dec::new(&p.discount),
                gross_price: Dec::new(&p.gross_price),
                base_quantity: Dec::new(&p.base_quantity),
            }),
        rate: rate(l.tax.as_ref()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::conformance::apply_patch;
    use crate::rule::{check_template, fill};
    use serde_json::{Map, Value};
    use std::collections::BTreeSet;
    use std::str::FromStr;

    fn d(s: &str) -> Decimal {
        Decimal::from_str(s).unwrap()
    }

    fn invoice(set: &[(&str, &str)]) -> pb::Invoice {
        let mut inv = pb::Invoice::default();
        let map: Map<String, Value> = set
            .iter()
            .map(|(k, v)| ((*k).to_string(), Value::String((*v).to_string())))
            .collect();
        apply_patch(&mut inv, &map, &[]).unwrap();
        inv
    }

    #[test]
    fn text_trims_unicode_whitespace_and_empty_is_absent() {
        assert_eq!(text("abc"), Some("abc"));
        assert_eq!(text("  a b \t\n"), Some("a b"));
        assert_eq!(text("\u{a0}\u{2003}x\u{3000}"), Some("x"));
        assert_eq!(text(""), None);
        assert_eq!(text("   "), None);
        assert_eq!(text("\u{a0}\n\t"), None);
    }

    #[test]
    fn dec_parses_once_and_invalid_is_absent() {
        let ok = Dec::new(" 1050.00 ");
        assert_eq!(ok.raw, "1050.00");
        assert_eq!(ok.value, Some(d("1050.00")));
        assert!(ok.exists() && ok.ebv() && !ok.is_invalid());

        let zero = Dec::new("0.00");
        assert!(zero.exists() && !zero.ebv() && !zero.is_invalid());

        for blank in ["", "  "] {
            let absent = Dec::new(blank);
            assert_eq!(absent, Dec::ABSENT);
            assert!(!absent.exists() && !absent.ebv() && !absent.is_invalid());
        }

        for bad in ["1,050.00", "١٠٥٠", "1e3", "AED200000"] {
            let invalid = Dec::new(bad);
            assert_eq!(invalid.raw, bad);
            assert_eq!(invalid.value, None);
            assert!(
                !invalid.exists() && !invalid.ebv() && invalid.is_invalid(),
                "{bad}"
            );
        }
    }

    #[test]
    fn exporter_defaults_are_applied() {
        let empty = pb::Invoice::default();
        let doc = Doc::new(&empty);
        assert_eq!(doc.business_process_type(), "urn:peppol:bis:billing");
        assert_eq!(
            doc.specification_identifier(),
            "urn:peppol:pint:billing-1@ae-1"
        );

        let blank = invoice(&[
            ("process.business_process_type", " "),
            ("process.specification_identifier", "\t"),
        ]);
        let doc = Doc::new(&blank);
        assert_eq!(doc.business_process_type(), DEFAULT_BUSINESS_PROCESS_TYPE);
        assert_eq!(
            doc.specification_identifier(),
            DEFAULT_SPECIFICATION_IDENTIFIER
        );

        let set = invoice(&[
            (
                "process.business_process_type",
                " urn:peppol:bis:selfbilling ",
            ),
            ("process.specification_identifier", " urn:x "),
        ]);
        let doc = Doc::new(&set);
        assert_eq!(doc.business_process_type(), "urn:peppol:bis:selfbilling");
        assert_eq!(doc.specification_identifier(), "urn:x");

        let mut category = pb::TaxCategory::default();
        assert_eq!(tax_scheme(&category), "VAT");
        category.tax_scheme = "  ".into();
        assert_eq!(tax_scheme(&category), "VAT");
        category.tax_scheme = " GST ".into();
        assert_eq!(tax_scheme(&category), "GST");

        const { assert!(!PRICE_CHARGE_INDICATOR) };
    }

    #[test]
    fn doc_kind_follows_the_type_code() {
        for code in ["381", "81", " 381 ", "\t81\n"] {
            assert_eq!(
                DocKind::from_type_code(code),
                DocKind::CreditNote,
                "{code:?}"
            );
        }
        for code in ["380", "480", "", "  ", "0381", "38 1", "396"] {
            assert_eq!(DocKind::from_type_code(code), DocKind::Invoice, "{code:?}");
        }
        let inv = invoice(&[("invoice_type_code", "81")]);
        assert_eq!(Doc::new(&inv).kind, DocKind::CreditNote);
        assert_eq!(Doc::new(&pb::Invoice::default()).kind, DocKind::Invoice);
    }

    #[test]
    fn txn_reads_a_valid_code_only() {
        let flags = |code: &str| {
            let inv = invoice(&[("transaction_type_code", code)]);
            let doc = Doc::new(&inv);
            (0..=9).filter(|&p| doc.txn(p)).collect::<Vec<_>>()
        };
        assert_eq!(flags("00000000"), Vec::<usize>::new());
        assert_eq!(flags("10000000"), [1]);
        assert_eq!(flags("00000001"), [8]);
        assert_eq!(flags("01001100"), [2, 5, 6]);
        assert_eq!(flags(" 00000101 "), [6, 8]);
        assert_eq!(flags("11111111"), [1, 2, 3, 4, 5, 6, 7, 8]);
        for invalid in [
            "1111111",
            "111111111",
            "1111111a",
            "11112111",
            "1111 111",
            "١١١١١١١١",
            "",
        ] {
            assert_eq!(flags(invalid), Vec::<usize>::new(), "{invalid:?}");
        }
    }

    #[test]
    fn decimal_tables_mirror_the_invoice() {
        let (_, inv) = crate::conformance::examples()
            .into_iter()
            .find(|(slug, _)| slug == "standard-tax-invoice")
            .unwrap();
        let doc = Doc::new(&inv);
        assert_eq!(doc.kind, DocKind::Invoice);
        assert_eq!(doc.total_amount.value, Some(d("11175.45")));
        assert_eq!(doc.vat_amount.value, Some(d("532.16")));
        assert!(!doc.exchange_rate.exists() && !doc.contract_value.exists());
        assert_eq!(doc.payment_terms.len(), inv.payment_terms.len());
        assert!(!doc.payment_terms[0].amount.exists());
        assert_eq!(doc.allowances_charges.len(), 2);
        assert_eq!(doc.allowances_charges[1].amount.value, Some(d("419.44")));
        assert_eq!(doc.allowances_charges[0].percentage.value, Some(d("2.5")));
        assert_eq!(doc.allowances_charges[0].rate.value, Some(d("5")));
        assert_eq!(doc.totals.payable_amount.value, Some(d("11175.5")));
        assert_eq!(doc.totals.rounding_amount.value, Some(d("0.05")));
        assert!(!doc.totals.paid_amount.exists());
        assert_eq!(doc.tax_breakdown[0].tax_amount.value, Some(d("532.1645")));
        assert_eq!(doc.tax_breakdown[0].rate.value, Some(d("5")));
        assert_eq!(doc.lines.len(), 1);
        let line = &doc.lines[0];
        assert_eq!(line.quantity.value, Some(d("2000")));
        assert_eq!(line.net_amount.value, Some(d("10486")));
        assert_eq!(line.amount_aed.value, Some(d("11010.3")));
        assert_eq!(line.vat_amount_aed.value, Some(d("524.3")));
        assert_eq!(line.allowances_charges[1].amount.value, Some(d("980")));
        assert_eq!(
            line.allowances_charges[0].base_amount.value,
            Some(d("9800"))
        );
        assert_eq!(line.price.net_price.value, Some(d("4.9")));
        assert_eq!(line.price.discount.value, Some(d("0.1")));
        assert_eq!(line.price.gross_price.value, Some(d("5")));
        assert_eq!(line.price.base_quantity.value, Some(d("1")));
        assert_eq!(line.rate.value, Some(d("5")));
    }

    #[test]
    fn a_line_allowance_has_no_tax_rate() {
        let inv = invoice(&[
            ("lines[0].allowances_charges[0].amount", "1"),
            ("lines[0].allowances_charges[0].tax_category.rate", "5"),
        ]);
        let doc = Doc::new(&inv);
        assert_eq!(doc.lines[0].allowances_charges[0].rate, Dec::ABSENT);
    }

    #[test]
    fn decimal_fields_are_unique_string_fields_of_the_invoice() {
        assert_eq!(Doc::DECIMAL_FIELDS.len(), 33);
        let paths: BTreeSet<_> = Doc::DECIMAL_FIELDS.iter().map(|f| f.path).collect();
        assert_eq!(paths.len(), Doc::DECIMAL_FIELDS.len());
        for f in Doc::DECIMAL_FIELDS {
            check_template(f.path).unwrap_or_else(|e| panic!("{}: {e}", f.path));
            assert!(
                f.path.contains("allowances_charges[#]") == f.charge_term.is_some(),
                "{}",
                f.path
            );
        }
    }

    /// Sets every decimal field (two elements per repeated level) to a distinct invalid string
    /// naming its own path, and checks the visitor hands back exactly those values.
    #[test]
    fn each_decimal_visits_every_field_with_its_own_value() {
        let mut set = Vec::new();
        for f in Doc::DECIMAL_FIELDS {
            let levels = f.path.matches('#').count();
            let combos: Vec<Vec<usize>> = match levels {
                0 => vec![vec![]],
                1 => vec![vec![0], vec![1]],
                _ => vec![vec![0, 0], vec![0, 1], vec![1, 0], vec![1, 1]],
            };
            for idx in combos {
                let path = fill(f.path, &idx);
                set.push((path.clone(), format!("bad:{path}")));
            }
        }
        let pairs: Vec<(&str, &str)> = set.iter().map(|(k, v)| (k.as_str(), v.as_str())).collect();
        let inv = invoice(&pairs);
        let doc = Doc::new(&inv);

        let mut seen = BTreeSet::new();
        doc.each_decimal(|field, idx, _term, dec| {
            let path = fill(field.path, idx);
            assert_eq!(dec.raw, format!("bad:{path}"));
            assert!(dec.is_invalid());
            assert!(seen.insert(path));
        });
        let want: BTreeSet<String> = set.into_iter().map(|(k, _)| k).collect();
        assert_eq!(seen, want);
    }

    #[test]
    fn each_decimal_resolves_allowance_and_charge_terms() {
        let mut inv = invoice(&[
            ("allowances_charges[0].amount", "1"),
            ("allowances_charges[1].amount", "2"),
            ("lines[0].allowances_charges[0].percentage", "3"),
            ("lines[0].allowances_charges[1].percentage", "4"),
            ("lines[0].price.net_price", "5"),
        ]);
        inv.allowances_charges[1].is_charge = true;
        inv.lines[0].allowances_charges[1].is_charge = true;
        let doc = Doc::new(&inv);
        let mut got = Vec::new();
        doc.each_decimal(|field, idx, term, dec| {
            got.push((fill(field.path, idx), term, dec.raw.to_string()));
        });
        assert_eq!(
            got,
            [
                (
                    "allowances_charges[0].amount".to_string(),
                    "IBT-092",
                    "1".to_string()
                ),
                (
                    "allowances_charges[1].amount".to_string(),
                    "IBT-099",
                    "2".to_string()
                ),
                (
                    "lines[0].allowances_charges[0].percentage".to_string(),
                    "IBT-138",
                    "3".to_string()
                ),
                (
                    "lines[0].allowances_charges[1].percentage".to_string(),
                    "IBT-143",
                    "4".to_string()
                ),
                (
                    "lines[0].price.net_price".to_string(),
                    "IBT-146",
                    "5".to_string()
                ),
            ]
        );
    }
}
