//! `codelists` family rules (spec 5.2.4): the `ibr-cl-*` asserts, each "this coded value is in
//! its list". Coverage: `rulesets/pint-ae-1.0.4/coverage/codelists.tsv`; fixtures:
//! `mutations/codelists.jsonl`, run by `tests/family_fixtures.rs`.
//!
//! Membership comes from [`crate::codelists::sets`] (list id = rule id, finding F10); a list of
//! fewer than five codes stays literal in the rule (`ibr-cl-01`, the `SEPA` of `ibr-cl-10`).
//! Every official test has the shape
//! `not(contains(normalize-space(x), ' ')) and contains(' LIST ', concat(' ', normalize-space(x), ' '))`,
//! which is exact membership of the value: a code holds no space, so a value with an inner space
//! is not in the list, and the exporter writes values already trimmed.
//!
//! The context of each rule is an element or attribute the exporter writes, so a coded value that
//! is empty (not written) is never a finding, and a scheme or list attribute is checked only when
//! the identifier it belongs to is written (an `Identifier` without an id is dropped with its
//! scheme).
//!
//! Status of the 18 rows: 17 implemented ([`RULES`]) and `ibr-cl-24`, structural.

use crate::codelists::sets;
use crate::doc::{Doc, DocKind, text};
use crate::pb;
use crate::rule::{Rule, Sink, fill};

/// Every implemented rule of the family.
pub static RULES: &[Rule] = &[
    Rule {
        id: "ibr-cl-01",
        check: ibr_cl_01,
    },
    Rule {
        id: "ibr-cl-03",
        check: ibr_cl_03,
    },
    Rule {
        id: "ibr-cl-04",
        check: ibr_cl_04,
    },
    Rule {
        id: "ibr-cl-05",
        check: ibr_cl_05,
    },
    Rule {
        id: "ibr-cl-07",
        check: ibr_cl_07,
    },
    Rule {
        id: "ibr-cl-10",
        check: ibr_cl_10,
    },
    Rule {
        id: "ibr-cl-11",
        check: ibr_cl_11,
    },
    Rule {
        id: "ibr-cl-13",
        check: ibr_cl_13,
    },
    Rule {
        id: "ibr-cl-14",
        check: ibr_cl_14,
    },
    Rule {
        id: "ibr-cl-15",
        check: ibr_cl_15,
    },
    Rule {
        id: "ibr-cl-16",
        check: ibr_cl_16,
    },
    Rule {
        id: "ibr-cl-19",
        check: ibr_cl_19,
    },
    Rule {
        id: "ibr-cl-20",
        check: ibr_cl_20,
    },
    Rule {
        id: "ibr-cl-21",
        check: ibr_cl_21,
    },
    Rule {
        id: "ibr-cl-23",
        check: ibr_cl_23,
    },
    Rule {
        id: "ibr-cl-25",
        check: ibr_cl_25,
    },
    Rule {
        id: "ibr-cl-26",
        check: ibr_cl_26,
    },
];

/// Whether `code` is in list `list` of `codelists.tsv`.
fn member(list: &str, code: &str) -> bool {
    sets().contains(list, code)
}

/// The scheme of an identifier that is written with one: an `Identifier` is written only with a
/// non-empty id, and its scheme attribute only when non-empty.
fn scheme(i: Option<&pb::Identifier>) -> Option<&str> {
    i.filter(|i| text(&i.id).is_some())
        .and_then(|i| text(&i.scheme_id))
}

/// `ibr-cl-01`, context `cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode`, test
/// `(self::cbc:InvoiceTypeCode and ((not(contains(normalize-space(.), ' ')) and contains(' 380 480 ', concat(' ', normalize-space(.), ' '))))) or (self::cbc:CreditNoteTypeCode and ((not(contains(normalize-space(.), ' ')) and contains(' 81 381 ', concat(' ', normalize-space(.), ' ')))))`.
/// The root is a `CreditNote` exactly for the codes 381 and 81, so only the invoice branch can
/// fail.
fn ibr_cl_01(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let Some(code) = text(&doc.inv.invoice_type_code) else {
        return;
    };
    if doc.kind == DocKind::Invoice && !matches!(code, "380" | "480") {
        sink.fail(&[]);
    }
}

/// The amount fields the exporter writes with `currencyID` = IBT-005 (`export::B::amount`), as
/// paths of [`Doc::DECIMAL_FIELDS`]. BTAE-10 and BTAE-08 carry `AED`, IBT-111 carries IBT-006, and
/// BTAE-20 is text in a document description.
const IBT_005_AMOUNTS: &[&str] = &[
    "total_amount",
    "vat_amount",
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
    "tax_breakdown[#].taxable_amount",
    "tax_breakdown[#].tax_amount",
    "lines[#].net_amount",
    "lines[#].allowances_charges[#].amount",
    "lines[#].allowances_charges[#].base_amount",
    "lines[#].price.net_price",
    "lines[#].price.discount",
    "lines[#].price.gross_price",
];

/// `ibr-cl-03`, context `cbc:Amount | cbc:BaseAmount | cbc:PriceAmount | cbc:TaxAmount | cbc:TaxableAmount | cbc:LineExtensionAmount | cbc:TaxExclusiveAmount | cbc:TaxInclusiveAmount | cbc:AllowanceTotalAmount | cbc:ChargeTotalAmount | cbc:PrepaidAmount | cbc:PayableRoundingAmount | cbc:PayableAmount`,
/// test `((not(contains(normalize-space(@currencyID), ' ')) and contains(' AED AFN … ZWG ', concat(' ', normalize-space(@currencyID), ' '))))`.
/// One finding per written amount whose `currencyID` is not in the list, at that amount's field
/// (F7). The attribute is always written, even for an empty currency, and is IBT-005 on every
/// amount but BTAE-10/BTAE-08 (`AED`, always valid) and IBT-111 (IBT-006). The VAT breakdown's
/// amounts are written inside the document-currency `TaxTotal`, which needs IBT-110.
fn ibr_cl_03(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    if !member("ibr-cl-03", text(&inv.currency).unwrap_or("")) {
        let breakdown_written = doc.vat_amount.exists();
        doc.each_decimal(|field, idx, term, dec| {
            let in_breakdown = field.path.starts_with("tax_breakdown[");
            if dec.exists()
                && IBT_005_AMOUNTS.contains(&field.path)
                && (breakdown_written || !in_breakdown)
            {
                sink.fail_at(fill(field.path, idx)).term(term);
            }
        });
    }
    let accounting = &doc.totals.tax_amount_accounting_currency;
    if accounting.exists() && !member("ibr-cl-03", text(&inv.tax_currency).unwrap_or("")) {
        sink.fail_at("totals.tax_amount_accounting_currency")
            .term("IBT-111");
    }
}

/// `ibr-cl-04`, context `cbc:DocumentCurrencyCode`, same list test as `ibr-cl-03`.
fn ibr_cl_04(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(code) = text(&doc.inv.currency)
        && !member("ibr-cl-04", code)
    {
        sink.fail(&[]);
    }
}

/// `ibr-cl-05`, context `cbc:TaxCurrencyCode`, same list test.
fn ibr_cl_05(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(code) = text(&doc.inv.tax_currency)
        && !member("ibr-cl-05", code)
    {
        sink.fail(&[]);
    }
}

/// `ibr-cl-07`, context
/// `cac:AdditionalDocumentReference[cbc:DocumentTypeCode = '130']/cbc:ID[@schemeID] | cac:DocumentReference[cbc:DocumentTypeCode = '130']/cbc:ID[@schemeID]`,
/// test: `@schemeID` in the UNTDID 1153 subset. The document-level one is IBT-018-1, each line's
/// IBT-128-1.
fn ibr_cl_07(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let object = doc
        .inv
        .references
        .as_ref()
        .and_then(|r| r.invoiced_object.as_ref());
    if let Some(s) = scheme(object)
        && !member("ibr-cl-07", s)
    {
        sink.fail(&[]);
    }
    for (i, l) in doc.inv.lines.iter().enumerate() {
        if let Some(s) = scheme(l.object_identifier.as_ref())
            && !member("ibr-cl-07", s)
        {
            sink.fail_at(format!("lines[{i}].object_identifier.scheme_id"))
                .term("IBT-128-1");
        }
    }
}

/// `ibr-cl-10`, context `cac:PartyIdentification/cbc:ID[@schemeID]`, test
/// `((… contains(' 0002 0003 … 0248 ', …))) or ((not(contains(normalize-space(@schemeID), ' ')) and contains(' SEPA ', concat(' ', normalize-space(@schemeID), ' '))) and ((ancestor::cac:AccountingSupplierParty) or (ancestor::cac:PayeeParty)))`.
/// The ISO 6523 ICD list is `ibr-cl-10` of `codelists.tsv`; `SEPA` (one code, literal) is also
/// valid for the seller's and the payee's identifiers, not for the buyer's. The identifiers of
/// `BuyerCustomerParty` and `SellerSupplierParty` (BTAE-01, BTAE-14) have no scheme.
fn ibr_cl_10(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    if let Some(seller) = &inv.seller {
        for (j, id) in seller.identifiers.iter().enumerate() {
            if let Some(s) = scheme(Some(id))
                && s != "SEPA"
                && !member("ibr-cl-10", s)
            {
                sink.fail_at(format!("seller.identifiers[{j}].scheme_id"))
                    .term("IBT-029-1");
            }
        }
    }
    if let Some(buyer) = &inv.buyer {
        for (j, id) in buyer.identifiers.iter().enumerate() {
            if let Some(s) = scheme(Some(id))
                && !member("ibr-cl-10", s)
            {
                sink.fail(&[j]);
            }
        }
    }
    if let Some(s) = scheme(inv.payee.as_ref().and_then(|p| p.identifier.as_ref()))
        && s != "SEPA"
        && !member("ibr-cl-10", s)
    {
        sink.fail_at("payee.identifier.scheme_id").term("IBT-060-1");
    }
}

/// The scheme of a party's `cbc:CompanyID`, written with a non-empty registration id.
fn company_scheme(p: &pb::Party) -> Option<&str> {
    p.legal_registration
        .as_ref()
        .filter(|lr| text(&lr.id).is_some())
        .and_then(|lr| text(&lr.scheme_id))
}

/// `ibr-cl-11`, context `cac:PartyLegalEntity/cbc:CompanyID[@schemeID]`, test: `@schemeID` in the
/// ISO 6523 ICD list (`ibr-cl-11`). Seller IBT-030-1, buyer IBT-047-1, payee IBT-061-1.
fn ibr_cl_11(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    if let Some(s) = inv.seller.as_ref().and_then(company_scheme)
        && !member("ibr-cl-11", s)
    {
        sink.fail(&[]);
    }
    if let Some(s) = inv.buyer.as_ref().and_then(company_scheme)
        && !member("ibr-cl-11", s)
    {
        sink.fail_at("buyer.legal_registration.scheme_id")
            .term("IBT-047-1");
    }
    if let Some(s) = scheme(
        inv.payee
            .as_ref()
            .and_then(|p| p.legal_registration.as_ref()),
    ) && !member("ibr-cl-11", s)
    {
        sink.fail_at("payee.legal_registration.scheme_id")
            .term("IBT-061-1");
    }
}

/// `ibr-cl-13`, context `cac:CommodityClassification/cbc:ItemClassificationCode[@listID]`, test:
/// `@listID` in the UNTDID 7143 list. One finding per written classification with a scheme.
fn ibr_cl_13(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l) in doc.inv.lines.iter().enumerate() {
        let Some(item) = &l.item else { continue };
        for (j, c) in item.classifications.iter().enumerate() {
            if text(&c.code).is_some()
                && let Some(s) = text(&c.scheme_id)
                && !member("ibr-cl-13", s)
            {
                sink.fail(&[i, j]);
            }
        }
    }
}

/// `ibr-cl-14`, context `cac:Country/cbc:IdentificationCode`, test: the code in ISO 3166-1
/// (`ibr-cl-14`). The countries of the five addresses the exporter writes: seller IBT-040, buyer
/// IBT-055, tax representative IBT-069, delivery IBT-080 and the financial institutions IBT-175.
/// (`cac:OriginCountry` is another element, see `ibr-cl-15`.)
fn ibr_cl_14(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    let bad = |a: Option<&pb::PostalAddress>| {
        a.and_then(|a| text(&a.country_code))
            .is_some_and(|c| !member("ibr-cl-14", c))
    };
    if bad(inv.seller.as_ref().and_then(|p| p.postal_address.as_ref())) {
        sink.fail(&[]);
    }
    if bad(inv.buyer.as_ref().and_then(|p| p.postal_address.as_ref())) {
        sink.fail_at("buyer.postal_address.country_code")
            .term("IBT-055");
    }
    if bad(inv
        .tax_representative
        .as_ref()
        .and_then(|r| r.postal_address.as_ref()))
    {
        sink.fail_at("tax_representative.postal_address.country_code")
            .term("IBT-069");
    }
    if bad(inv.delivery.as_ref().and_then(|d| d.address.as_ref())) {
        sink.fail_at("delivery.address.country_code")
            .term("IBT-080");
    }
    for (k, pi) in inv.payment_instructions.iter().enumerate() {
        let institution = pi
            .credit_transfer
            .as_ref()
            .and_then(|ct| ct.institution_address.as_ref());
        if bad(institution) {
            sink.fail_at(format!(
                "payment_instructions[{k}].credit_transfer.institution_address.country_code"
            ))
            .term("IBT-175");
        }
    }
}

/// `ibr-cl-15`, context `cac:OriginCountry/cbc:IdentificationCode`, test: the code in ISO 3166-1
/// (`ibr-cl-15`). IBT-159.
fn ibr_cl_15(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l) in doc.inv.lines.iter().enumerate() {
        if let Some(code) = l.item.as_ref().and_then(|it| text(&it.origin_country))
            && !member("ibr-cl-15", code)
        {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-cl-16`, context `cac:PaymentMeans/cbc:PaymentMeansCode`, test: the code in the UNCL4461
/// subset (`ibr-cl-16`). IBT-081.
fn ibr_cl_16(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (k, pi) in doc.inv.payment_instructions.iter().enumerate() {
        if let Some(code) = text(&pi.means_code)
            && !member("ibr-cl-16", code)
        {
            sink.fail(&[k]);
        }
    }
}

/// Reason codes of the allowances (`charge` false, list `ibr-cl-19`) or charges (`charge` true,
/// list `ibr-cl-20`): `cac:AllowanceCharge[cbc:ChargeIndicator = false()]/cbc:AllowanceChargeReasonCode`
/// and the `true()` twin. Document level IBT-098/IBT-105, line level IBT-140/IBT-145. The
/// price-level `AllowanceCharge` (indicator `false`) has no reason code.
fn reason_codes(doc: &Doc<'_>, sink: &mut Sink<'_>, charge: bool, list: &str) {
    let (document_term, line_term) = if charge {
        ("IBT-105", "IBT-145")
    } else {
        ("IBT-098", "IBT-140")
    };
    let bad = |a: &pb::AllowanceCharge| {
        a.is_charge == charge && text(&a.reason_code).is_some_and(|c| !member(list, c))
    };
    for (k, a) in doc.inv.allowances_charges.iter().enumerate() {
        if bad(a) {
            sink.fail_at(format!("allowances_charges[{k}].reason_code"))
                .term(document_term);
        }
    }
    for (i, l) in doc.inv.lines.iter().enumerate() {
        for (k, a) in l.allowances_charges.iter().enumerate() {
            if bad(a) {
                sink.fail_at(format!("lines[{i}].allowances_charges[{k}].reason_code"))
                    .term(line_term);
            }
        }
    }
}

/// `ibr-cl-19`: the allowance reason codes in UNCL 5189.
fn ibr_cl_19(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    reason_codes(doc, sink, false, "ibr-cl-19");
}

/// `ibr-cl-20`: the charge reason codes in UNCL 7161.
fn ibr_cl_20(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    reason_codes(doc, sink, true, "ibr-cl-20");
}

/// `ibr-cl-21`, context `cac:StandardItemIdentification/cbc:ID[@schemeID]`, test: `@schemeID` in
/// the ISO 6523 ICD list (`ibr-cl-21`). IBT-157-1.
fn ibr_cl_21(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l) in doc.inv.lines.iter().enumerate() {
        if let Some(s) = scheme(l.item.as_ref().and_then(|it| it.standard_id.as_ref()))
            && !member("ibr-cl-21", s)
        {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-cl-23`, context
/// `cbc:InvoicedQuantity[@unitCode] | cbc:BaseQuantity[@unitCode] | cbc:CreditedQuantity[@unitCode]`,
/// test: `@unitCode` in UN/ECE Rec 20 with the Rec 21 extension (`ibr-cl-23`). The quantity's unit
/// (IBT-130) is written with the quantity, the base quantity's (IBT-150) with the base quantity.
fn ibr_cl_23(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, (l, d)) in doc.inv.lines.iter().zip(&doc.lines).enumerate() {
        if d.quantity.exists()
            && let Some(unit) = text(&l.unit_code)
            && !member("ibr-cl-23", unit)
        {
            sink.fail(&[i]);
        }
        let base_unit = l
            .price
            .as_ref()
            .and_then(|p| text(&p.base_quantity_unit_code));
        if d.price.base_quantity.exists()
            && let Some(unit) = base_unit
            && !member("ibr-cl-23", unit)
        {
            sink.fail_at(format!("lines[{i}].price.base_quantity_unit_code"))
                .term("IBT-150");
        }
    }
}

/// `ibr-cl-25`, context `cbc:EndpointID[@schemeID]`, test: `@schemeID` in the CEF EAS list
/// (`ibr-cl-25`). Seller IBT-034-1, buyer IBT-049-1.
fn ibr_cl_25(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    if let Some(s) = scheme(
        inv.seller
            .as_ref()
            .and_then(|p| p.electronic_address.as_ref()),
    ) && !member("ibr-cl-25", s)
    {
        sink.fail(&[]);
    }
    if let Some(s) = scheme(
        inv.buyer
            .as_ref()
            .and_then(|p| p.electronic_address.as_ref()),
    ) && !member("ibr-cl-25", s)
    {
        sink.fail_at("buyer.electronic_address.scheme_id")
            .term("IBT-049-1");
    }
}

/// `ibr-cl-26`, context `cac:DeliveryLocation/cbc:ID[@schemeID]`, test: `@schemeID` in the ISO
/// 6523 ICD list (`ibr-cl-26`). IBT-071-1.
fn ibr_cl_26(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(s) = scheme(doc.inv.delivery.as_ref().and_then(|d| d.location.as_ref()))
        && !member("ibr-cl-26", s)
    {
        sink.fail(&[]);
    }
}

#[cfg(test)]
mod tests {
    use serde_json::{Value, json};

    use super::*;
    use crate::catalog::{Family, Status};
    use crate::conformance::{apply_patch, examples};
    use crate::export::testing::{export_str, maximal};
    use crate::ruleset::default_ruleset;

    fn example(slug: &str) -> pb::Invoice {
        examples()
            .into_iter()
            .find(|(s, _)| s == slug)
            .unwrap_or_else(|| panic!("no example {slug}"))
            .1
    }

    fn patched(slug: &str, set: Value) -> pb::Invoice {
        let mut inv = example(slug);
        let set = set.as_object().expect("an object").clone();
        apply_patch(&mut inv, &set, &[]).expect("patch applies");
        inv
    }

    type Check = fn(&Doc<'_>, &mut Sink<'_>);

    /// `(path, term)` of every finding of `rule` over `inv`; the term is the override or `""`.
    fn run(rule: Check, template: &str, inv: &pb::Invoice) -> Vec<(String, String)> {
        let doc = Doc::new(inv);
        let mut sink = Sink::new(template);
        rule(&doc, &mut sink);
        sink.into_findings()
            .into_iter()
            .map(|f| (f.path, f.business_term.unwrap_or_default().to_string()))
            .collect()
    }

    fn paths(v: Vec<(String, String)>) -> Vec<String> {
        v.into_iter().map(|(p, _)| p).collect()
    }

    #[test]
    fn the_registered_rules_are_exactly_the_implemented_rows() {
        let mut registered: Vec<&str> = RULES.iter().map(|r| r.id).collect();
        registered.sort_unstable();
        let mut rows: Vec<&str> = default_ruleset()
            .catalog()
            .entries()
            .iter()
            .filter(|e| e.family == Family::Codelists && e.status == Status::Implemented)
            .map(|e| e.rule_id)
            .collect();
        rows.sort_unstable();
        assert_eq!(registered, rows);
        assert_eq!(registered.len(), 17);
        // Every list the rules read exists in codelists.tsv.
        for id in registered
            .iter()
            .filter(|id| !matches!(**id, "ibr-cl-01" | "ibr-cl-10"))
        {
            assert!(sets().get(id).is_some(), "{id}");
        }
        assert!(sets().get("ibr-cl-10").is_some());
    }

    /// `ibr-cl-24` is structural: the exporter embeds no attachment, so there is no
    /// `EmbeddedDocumentBinaryObject[@mimeCode]` to check.
    #[test]
    fn ibr_cl_24_cannot_fail_because_no_attachment_is_exported() {
        let inv = maximal("380");
        let with_attachment = inv.supporting_documents.iter().any(|d| {
            d.attachment
                .as_ref()
                .is_some_and(|a| !a.mime_code.is_empty() && !a.object_key.is_empty())
        });
        assert!(with_attachment, "the maximal document sets the attachment");
        for inv in [inv, maximal("381"), example("standard-tax-invoice")] {
            let xml = export_str(&inv);
            assert!(!xml.contains("EmbeddedDocumentBinaryObject"));
            assert!(!xml.contains("mimeCode"));
        }
        // The upstream example has an attachment with a mime code in the model.
        let upstream = example("standard-tax-invoice");
        assert!(upstream.supporting_documents.iter().any(|d| {
            d.attachment
                .as_ref()
                .is_some_and(|a| a.mime_code == "application/pdf")
        }));
    }

    #[test]
    fn ibr_cl_01_checks_the_invoice_branch_only() {
        for (code, fails) in [
            ("380", false),
            ("480", false),
            ("381", false),
            ("81", false),
        ] {
            let inv = patched("standard-tax-invoice", json!({ "invoice_type_code": code }));
            assert_eq!(
                run(ibr_cl_01, "invoice_type_code", &inv).len() == 1,
                fails,
                "{code}"
            );
        }
        for code in ["379", "380 480", "38O", "1"] {
            let inv = patched("standard-tax-invoice", json!({ "invoice_type_code": code }));
            assert_eq!(
                paths(run(ibr_cl_01, "invoice_type_code", &inv)),
                ["invoice_type_code"],
                "{code}"
            );
        }
        // Not written, so not checked.
        let mut inv = example("standard-tax-invoice");
        inv.invoice_type_code.clear();
        assert!(run(ibr_cl_01, "invoice_type_code", &inv).is_empty());
    }

    #[test]
    fn ibr_cl_03_reports_every_written_amount_at_its_own_field() {
        let t = "totals.payable_amount";
        let inv = patched("standard-tax-invoice", json!({ "currency": "AEDX" }));
        let found = run(ibr_cl_03, t, &inv);
        // 22 amounts carry IBT-005 in this document; the two AED ones are valid.
        assert_eq!(found.len(), 22);
        assert!(
            found
                .iter()
                .any(|(p, term)| p == "lines[0].price.gross_price" && term == "IBT-148")
        );
        assert!(
            found
                .iter()
                .any(|(p, term)| p == "allowances_charges[1].amount" && term == "IBT-099")
        );
        assert!(found.iter().any(|(p, _)| p == "total_amount"));
        assert!(!found.iter().any(|(p, _)| p.contains("amount_aed")));
        // A valid currency fails nothing, and an empty one fails every amount.
        assert!(run(ibr_cl_03, t, &example("standard-tax-invoice")).is_empty());
        let mut empty = example("standard-tax-invoice");
        empty.currency.clear();
        assert_eq!(run(ibr_cl_03, t, &empty).len(), 22);
    }

    #[test]
    fn ibr_cl_03_skips_the_breakdown_the_exporter_drops_without_ibt_110() {
        let mut inv = patched("standard-tax-invoice", json!({ "currency": "AEDX" }));
        let with = run(ibr_cl_03, "totals.payable_amount", &inv).len();
        inv.vat_amount.clear();
        let without = run(ibr_cl_03, "totals.payable_amount", &inv);
        // IBT-110 itself and the two breakdown amounts are not written.
        assert_eq!(without.len(), with - 3);
        assert!(
            !without
                .iter()
                .any(|(p, _)| p.starts_with("tax_breakdown") || p == "vat_amount")
        );
    }

    #[test]
    fn ibr_cl_03_checks_the_accounting_currency_amount_against_ibt_006() {
        let inv = patched("exports", json!({ "tax_currency": "AEX" }));
        assert_eq!(
            run(ibr_cl_03, "totals.payable_amount", &inv),
            [(
                "totals.tax_amount_accounting_currency".to_string(),
                "IBT-111".to_string()
            )]
        );
    }

    #[test]
    fn identifier_rules_check_the_scheme_only_of_a_written_identifier() {
        // An identifier without an id is not written, so its scheme is not checked.
        let inv = patched(
            "standard-tax-invoice",
            json!({
                "seller.identifiers[0].scheme_id": "9999",
                "buyer.legal_registration.id": "",
                "buyer.legal_registration.scheme_id": "9999",
                "delivery.location.scheme_id": "9999",
            }),
        );
        assert!(run(ibr_cl_10, "buyer.identifiers[#].scheme_id", &inv).is_empty());
        assert!(run(ibr_cl_11, "seller.legal_registration.scheme_id", &inv).is_empty());
        assert!(run(ibr_cl_26, "delivery.location.scheme_id", &inv).is_empty());
    }

    #[test]
    fn sepa_is_valid_for_the_seller_and_the_payee_but_not_the_buyer() {
        let t = "buyer.identifiers[#].scheme_id";
        let inv = patched(
            "standard-tax-invoice",
            json!({
                "seller.identifiers[0].id": "1",
                "seller.identifiers[0].scheme_id": "SEPA",
                "buyer.identifiers[0].id": "2",
                "buyer.identifiers[0].scheme_id": "SEPA",
                "payee.identifier.id": "3",
                "payee.identifier.scheme_id": "SEPA",
            }),
        );
        assert_eq!(
            run(ibr_cl_10, t, &inv),
            [("buyer.identifiers[0].scheme_id".to_string(), String::new())]
        );
        let ok = patched(
            "standard-tax-invoice",
            json!({
                "seller.identifiers[0].id": "1",
                "seller.identifiers[0].scheme_id": "0088",
                "payee.identifier.id": "3",
                "payee.identifier.scheme_id": "SEPA",
            }),
        );
        assert!(run(ibr_cl_10, t, &ok).is_empty());
    }

    #[test]
    fn a_value_with_an_inner_space_is_not_in_the_list_and_trimming_is_the_exporters() {
        let t = "currency";
        let spaced = patched("standard-tax-invoice", json!({ "currency": "A ED" }));
        assert_eq!(run(ibr_cl_04, t, &spaced).len(), 1);
        let tab = patched("standard-tax-invoice", json!({ "currency": "AED\tUSD" }));
        assert_eq!(run(ibr_cl_04, t, &tab).len(), 1);
        // Leading and trailing whitespace is trimmed before the XML is written.
        let padded = patched("standard-tax-invoice", json!({ "currency": " AED " }));
        assert!(run(ibr_cl_04, t, &padded).is_empty());
        let lower = patched("standard-tax-invoice", json!({ "currency": "aed" }));
        assert_eq!(run(ibr_cl_04, t, &lower).len(), 1);
    }

    #[test]
    fn country_codes_are_checked_at_all_five_addresses() {
        let inv = patched(
            "standard-tax-invoice",
            json!({
                "seller.postal_address.country_code": "XX",
                "buyer.postal_address.country_code": "XX",
                "tax_representative.postal_address.country_code": "XX",
                "delivery.address.country_code": "XX",
                "payment_instructions[0].credit_transfer.institution_address.country_code": "XX",
            }),
        );
        let found = run(ibr_cl_14, "seller.postal_address.country_code", &inv);
        assert_eq!(
            found,
            [
                (
                    "seller.postal_address.country_code".to_string(),
                    String::new()
                ),
                (
                    "buyer.postal_address.country_code".to_string(),
                    "IBT-055".to_string()
                ),
                (
                    "tax_representative.postal_address.country_code".to_string(),
                    "IBT-069".to_string()
                ),
                (
                    "delivery.address.country_code".to_string(),
                    "IBT-080".to_string()
                ),
                (
                    "payment_instructions[0].credit_transfer.institution_address.country_code"
                        .to_string(),
                    "IBT-175".to_string()
                ),
            ]
        );
        // The country of origin is another rule.
        let origin = patched(
            "standard-tax-invoice",
            json!({ "lines[0].item.origin_country": "XX" }),
        );
        assert!(run(ibr_cl_14, "seller.postal_address.country_code", &origin).is_empty());
        assert_eq!(
            paths(run(ibr_cl_15, "lines[#].item.origin_country", &origin)),
            ["lines[0].item.origin_country"]
        );
    }

    #[test]
    fn allowance_and_charge_reasons_are_split_by_the_indicator_and_the_level() {
        let inv = patched(
            "standard-tax-invoice",
            json!({
                "allowances_charges[0].reason_code": "XX",
                "allowances_charges[1].reason_code": "XX",
                "lines[0].allowances_charges[0].reason_code": "XX",
                "lines[0].allowances_charges[1].reason_code": "XX",
            }),
        );
        assert_eq!(
            run(ibr_cl_19, "allowances_charges[#].reason_code", &inv),
            [
                (
                    "allowances_charges[0].reason_code".to_string(),
                    "IBT-098".to_string()
                ),
                (
                    "lines[0].allowances_charges[0].reason_code".to_string(),
                    "IBT-140".to_string()
                ),
            ]
        );
        assert_eq!(
            run(ibr_cl_20, "allowances_charges[#].reason_code", &inv),
            [
                (
                    "allowances_charges[1].reason_code".to_string(),
                    "IBT-105".to_string()
                ),
                (
                    "lines[0].allowances_charges[1].reason_code".to_string(),
                    "IBT-145".to_string()
                ),
            ]
        );
    }

    #[test]
    fn units_are_checked_with_their_quantity() {
        let t = "lines[#].unit_code";
        let inv = patched(
            "standard-tax-invoice",
            json!({ "lines[0].unit_code": "ZZZZ", "lines[0].price.base_quantity_unit_code": "BAD" }),
        );
        assert_eq!(
            run(ibr_cl_23, t, &inv),
            [
                ("lines[0].unit_code".to_string(), String::new()),
                (
                    "lines[0].price.base_quantity_unit_code".to_string(),
                    "IBT-150".to_string()
                ),
            ]
        );
        // The unit attribute of a quantity that is not written is not written either.
        let none = patched(
            "standard-tax-invoice",
            json!({ "lines[0].unit_code": "ZZZZ", "lines[0].quantity": "" }),
        );
        assert!(run(ibr_cl_23, t, &none).is_empty());
    }

    #[test]
    fn the_official_examples_pass_every_rule_of_the_family() {
        for (slug, inv) in examples() {
            let run = default_ruleset().validate(&inv);
            let family: Vec<_> = run
                .issues
                .iter()
                .filter(|i| i.rule_id.starts_with("ibr-cl-"))
                .collect();
            assert!(family.is_empty(), "{slug}: {family:?}");
        }
    }
}
