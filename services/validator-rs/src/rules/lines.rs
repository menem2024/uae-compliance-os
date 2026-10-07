//! `lines` family rules (spec 5.2.4): the invoice lines (IBG-25) with their price, item, period
//! and VAT information (`cac:InvoiceLine` / `cac:CreditNoteLine`), plus the root term IBG-25
//! (`ibr-016`). Coverage: `rulesets/pint-ae-1.0.4/coverage/lines.tsv`; fixtures:
//! `mutations/lines.jsonl`, run by `tests/family_fixtures.rs`.
//!
//! Each rule is "this official assert fails on the XML the exporter writes for this `Doc`"
//! (Rule authoring protocol). What the exporter writes is the whole story: a line field that is
//! absent or not a decimal is not written, an aggregate without content is not written, and
//! `cac:Item`, `cac:Price` and `cac:ClassifiedTaxCategory` exist exactly when one of their fields
//! does ([`item_written`], [`price_written`], [`tax_written`]).
//!
//! Status of the 46 rows: 35 implemented ([`RULES`]), 10 structural (the model plus the exporter
//! cannot produce a violation, proved by `structural_rules_hold_on_every_exported_line`) and one
//! `upstream_noop` ([`UPSTREAM_NOOP`]).

use rust_decimal::Decimal;

use crate::codelists::sets;
use crate::decimal::{self, Cents};
use crate::doc::{Doc, DocKind, LineDec, tax_category_written, text};
use crate::pb;
use crate::rule::{Rule, Sink};
use crate::rules::platform::is_xsd_date;

/// Official asserts of this family whose XPath test is the constant `true()` (contract upstream
/// defect 4). They are listed `upstream_noop` in the coverage TSV and never registered.
pub const UPSTREAM_NOOP: &[&str] = &["ibr-187-ae"];

/// Every implemented rule of the family; the coverage rows with status `implemented` are exactly
/// these.
pub static RULES: &[Rule] = &[
    Rule {
        id: "aligned-ibrp-004",
        check: aligned_ibrp_004,
    },
    Rule {
        id: "ibr-006-ae",
        check: ibr_006_ae,
    },
    Rule {
        id: "ibr-016",
        check: ibr_016,
    },
    Rule {
        id: "ibr-021",
        check: ibr_021,
    },
    Rule {
        id: "ibr-022",
        check: ibr_022,
    },
    Rule {
        id: "ibr-023",
        check: ibr_023,
    },
    Rule {
        id: "ibr-024",
        check: ibr_024,
    },
    Rule {
        id: "ibr-025",
        check: ibr_025,
    },
    Rule {
        id: "ibr-026",
        check: ibr_026,
    },
    Rule {
        id: "ibr-027",
        check: ibr_027,
    },
    Rule {
        id: "ibr-028",
        check: ibr_028,
    },
    Rule {
        id: "ibr-030",
        check: ibr_030,
    },
    Rule {
        id: "ibr-064",
        check: ibr_064,
    },
    Rule {
        id: "ibr-065",
        check: ibr_065,
    },
    Rule {
        id: "ibr-085",
        check: ibr_085,
    },
    Rule {
        id: "ibr-086",
        check: ibr_086,
    },
    Rule {
        id: "ibr-087",
        check: ibr_087,
    },
    Rule {
        id: "ibr-088",
        check: ibr_088,
    },
    Rule {
        id: "ibr-092",
        check: ibr_092,
    },
    Rule {
        id: "ibr-104-ae",
        check: ibr_104_ae,
    },
    Rule {
        id: "ibr-111-ae",
        check: ibr_111_ae,
    },
    Rule {
        id: "ibr-123-ae",
        check: ibr_123_ae,
    },
    Rule {
        id: "ibr-125-ae",
        check: ibr_125_ae,
    },
    Rule {
        id: "ibr-126-ae",
        check: ibr_126_ae,
    },
    Rule {
        id: "ibr-145-ae",
        check: ibr_145_ae,
    },
    Rule {
        id: "ibr-147-ae",
        check: ibr_147_ae,
    },
    Rule {
        id: "ibr-166-ae",
        check: ibr_166_ae,
    },
    Rule {
        id: "ibr-167-ae",
        check: ibr_167_ae,
    },
    Rule {
        id: "ibr-184-ae",
        check: ibr_184_ae,
    },
    Rule {
        id: "ibr-185-ae",
        check: ibr_185_ae,
    },
    Rule {
        id: "ibr-186-ae",
        check: ibr_186_ae,
    },
    Rule {
        id: "ibr-188-ae",
        check: ibr_188_ae,
    },
    Rule {
        id: "ibr-189-ae",
        check: ibr_189_ae,
    },
    Rule {
        id: "ibr-194-ae",
        check: ibr_194_ae,
    },
    Rule {
        id: "ibr-sr-58",
        check: ibr_sr_58,
    },
];

// ------------------------------------------------------------------------------ what is written

/// `(index, line, decimals)` for every written line ([`Doc::line_written`]: a line without a
/// written child is no `cac:InvoiceLine`, so no line context sees it); the decimal table mirrors
/// the lines index for index and the index is the model's.
fn each<'d, 'a>(
    doc: &'d Doc<'a>,
) -> impl Iterator<Item = (usize, &'a pb::InvoiceLine, &'d LineDec<'a>)> + 'd {
    doc.inv
        .lines
        .iter()
        .zip(&doc.lines)
        .enumerate()
        .filter(|&(i, _)| doc.line_written(i))
        .map(|(i, (l, d))| (i, l, d))
}

/// IBT-151 as written (`cac:ClassifiedTaxCategory/cbc:ID`).
fn tax_code(l: &pb::InvoiceLine) -> Option<&str> {
    l.tax.as_ref().and_then(|c| text(&c.code))
}

/// Whether `cac:ClassifiedTaxCategory` is written: any of IBT-151, IBT-152 (a valid decimal),
/// IBT-186, IBT-185 or the tax scheme IBT-167 is present (`export::tax_category`).
fn tax_written(l: &pb::InvoiceLine, d: &LineDec<'_>) -> bool {
    tax_category_written(l.tax.as_ref(), d.rate)
}

/// Whether `cac:Item` is written: the VAT information, the batch number or any item field.
fn item_written(l: &pb::InvoiceLine, d: &LineDec<'_>) -> bool {
    tax_written(l, d)
        || text(&l.batch_number).is_some()
        || item(l).is_some_and(|it| {
            text(&it.description).is_some()
                || text(&it.name).is_some()
                || text(&it.buyer_item_id).is_some()
                || text(&it.seller_item_id).is_some()
                || it
                    .standard_id
                    .as_ref()
                    .is_some_and(|s| text(&s.id).is_some())
                || written_codes(&it.service_accounting_codes).next().is_some()
                || text(&it.origin_country).is_some()
                || text(&it.goods_service_type).is_some()
                || text(&it.item_type).is_some()
                || written_codes(&it.classifications).next().is_some()
                || it.attributes.iter().any(|a| text(&a.name).is_some())
        })
}

/// Whether `cac:Price` is written: one of IBT-146, IBT-149, IBT-147 or IBT-148 is a valid decimal.
fn price_written(d: &LineDec<'_>) -> bool {
    let p = &d.price;
    p.net_price.exists()
        || p.base_quantity.exists()
        || p.discount.exists()
        || p.gross_price.exists()
}

/// The classifications that are written (a non-empty code), with their index in the model.
fn written_codes(
    list: &[pb::Classification],
) -> impl Iterator<Item = (usize, &pb::Classification)> {
    list.iter()
        .enumerate()
        .filter(|(_, c)| text(&c.code).is_some())
}

/// The item of a line, if the model has one.
fn item(l: &pb::InvoiceLine) -> Option<&pb::Item> {
    l.item.as_ref()
}

/// The unit of the price base quantity (IBT-150) as written.
fn base_unit(l: &pb::InvoiceLine) -> Option<&str> {
    l.price
        .as_ref()
        .and_then(|p| text(&p.base_quantity_unit_code))
}

/// The item type BTAE-13 of a line (`cbc:CommodityCode`).
fn item_type(l: &pb::InvoiceLine) -> Option<&str> {
    item(l).and_then(|it| text(&it.item_type))
}

/// Whether a classification code (IBT-158) is written.
fn has_classification(l: &pb::InvoiceLine) -> bool {
    item(l).is_some_and(|it| written_codes(&it.classifications).next().is_some())
}

/// Whether a service accounting code (BTAE-17, `cac:AdditionalItemIdentification/cbc:ID`) is
/// written.
fn has_sac(l: &pb::InvoiceLine) -> bool {
    item(l).is_some_and(|it| written_codes(&it.service_accounting_codes).next().is_some())
}

// ------------------------------------------------------------------------------------- rules

/// `ibr-016`, context `/ubl:Invoice | /cn:CreditNote`, test
/// `exists(cac:InvoiceLine) or exists(cac:CreditNoteLine)`. A line without a written child is
/// not written, so it fails when no line is ([`Doc::lines_exist`]), not only when there is none.
fn ibr_016(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if !doc.lines_exist() {
        sink.fail(&[]);
    }
}

/// `ibr-021`, context `cac:InvoiceLine | cac:CreditNoteLine`, test
/// `normalize-space(cbc:ID) != ''`.
fn ibr_021(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        if text(&l.id).is_none() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-022`, context line, test `exists(cbc:InvoicedQuantity) or exists(cbc:CreditedQuantity)`.
fn ibr_022(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        if !d.quantity.exists() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-023`, context line, test
/// `exists(cbc:InvoicedQuantity/@unitCode) or exists(cbc:CreditedQuantity/@unitCode)`. The unit
/// attribute is written only on a written quantity.
fn ibr_023(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, d) in each(doc) {
        if !(d.quantity.exists() && text(&l.unit_code).is_some()) {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-024`, context line, test `exists(cbc:LineExtensionAmount)`.
fn ibr_024(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        if !d.net_amount.exists() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-025`, context line, test `normalize-space(cac:Item/cbc:Name) != ''`.
fn ibr_025(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        if item(l).and_then(|it| text(&it.name)).is_none() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-026`, context line, test `exists(cac:Price/cbc:PriceAmount)`.
fn ibr_026(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        if !d.price.net_price.exists() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-027`, context line, test `(cac:Price/cbc:PriceAmount) >= 0`. The general comparison with
/// an empty node set is false, so a line without a price fails too.
fn ibr_027(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        if !d.price.net_price.value.is_some_and(|v| v >= Decimal::ZERO) {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-028`, context line, test
/// `(cac:Price/cac:AllowanceCharge/cbc:BaseAmount) >= 0 or not(exists(cac:Price/cac:AllowanceCharge/cbc:BaseAmount))`.
fn ibr_028(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        if d.price.gross_price.value.is_some_and(|v| v < Decimal::ZERO) {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-087`, context line, test
/// `not(cac:Price/cbc:BaseQuantity) or xs:decimal(cac:Price/cbc:BaseQuantity) > 0`.
fn ibr_087(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        if d.price
            .base_quantity
            .value
            .is_some_and(|v| v <= Decimal::ZERO)
        {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-088`, context `cac:Price/cbc:BaseQuantity[@unitCode]`, test
/// `not(../../cbc:InvoicedQuantity or ../../cbc:CreditedQuantity) or (@unitCode = ../../cbc:InvoicedQuantity/@unitCode) or (@unitCode = ../../cbc:CreditedQuantity/@unitCode)`.
/// The context node is a written base quantity with a unit; the comparison with an absent unit
/// attribute of the quantity is false.
fn ibr_088(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, d) in each(doc) {
        let Some(unit) = base_unit(l) else { continue };
        if !d.price.base_quantity.exists() || !d.quantity.exists() {
            continue;
        }
        let quantity_unit = text(&l.unit_code);
        if quantity_unit != Some(unit) {
            let f = sink.fail(&[i]);
            if let Some(u) = quantity_unit {
                f.suggest(u);
            }
        }
    }
}

/// `ibr-092`, context `cac:InvoiceLine/cac:DespatchLineReference/cac:DocumentReference/cbc:ID | …`,
/// test `count(//cac:DespatchDocumentReference) = 0`: IBT-016 is on the document.
fn ibr_092(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let document_level = doc
        .inv
        .references
        .as_ref()
        .is_some_and(|r| text(&r.despatch_advice_reference).is_some());
    if !document_level {
        return;
    }
    for (i, l, _) in each(doc) {
        if text(&l.despatch_advice_reference).is_some() {
            sink.fail(&[i]);
        }
    }
}

/// `aligned-ibrp-004`, context `cac:Price/cac:AllowanceCharge`, test
/// `not(cbc:BaseAmount) or (../cbc:PriceAmount castable as xs:decimal and cbc:BaseAmount castable as xs:decimal and cbc:Amount castable as xs:decimal and xs:decimal(../cbc:PriceAmount) = xs:decimal(cbc:BaseAmount) - xs:decimal(cbc:Amount))`.
fn aligned_ibrp_004(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        let p = &d.price;
        let Some(gross) = p.gross_price.value else {
            continue;
        };
        // Exact (`xs:decimal`): `decimal::sub` would round a difference wider than its
        // mantissa (gross 1e25 less a discount of 1e-25 is not 1e25).
        let expected = p
            .discount
            .value
            .and_then(|disc| Cents::of(gross)?.checked_sub(Cents::of(disc)?));
        let holds = match (p.net_price.value.and_then(Cents::of), expected) {
            (Some(net), Some(expected)) => net == expected,
            _ => false,
        };
        if !holds {
            let f = sink.fail(&[i]);
            if let Some(expected) = expected.and_then(Cents::to_decimal_exact) {
                f.suggest(expected);
            }
        }
    }
}

/// `ibr-104-ae`, context line, test
/// `not(exists(cac:Item/cac:ClassifiedTaxCategory[normalize-space(cbc:ID) != "E"])) or (exists(cac:ItemPriceExtension/cac:TaxTotal/cbc:TaxAmount) and exists(cac:ItemPriceExtension/cbc:Amount))`.
/// A category without a code counts as "not E". The extension is written with BTAE-10 only, and
/// its tax total with BTAE-08.
fn ibr_104_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, d) in each(doc) {
        if !tax_written(l, d) || tax_code(l) == Some("E") {
            continue;
        }
        if !d.amount_aed.exists() {
            sink.fail(&[i]);
        } else if !d.vat_amount_aed.exists() {
            sink.fail_at(format!("lines[{i}].vat_amount_aed"))
                .term("BTAE-08");
        }
    }
}

/// `ibr-111-ae`, context line, test
/// `(cac:Item/cac:ClassifiedTaxCategory/cbc:ID="N" and cac:Item/cac:ClassifiedTaxCategory/cbc:Percent > 0) or not(cac:Item/cac:ClassifiedTaxCategory/cbc:ID="N")`.
fn ibr_111_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, d) in each(doc) {
        if tax_code(l) == Some("N") && !d.rate.value.is_some_and(|r| r > Decimal::ZERO) {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-123-ae`, context line, test
/// `((../(cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81" or ../(cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "480") and count(cac:Item/cac:ClassifiedTaxCategory) = 1) or not(../(cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81" or ../(cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "480")`.
/// Only the type codes 81 and 480 are constrained (contract rule 13). A line has one tax message,
/// so "count = 1" fails only for a missing category.
fn ibr_123_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if !matches!(text(&doc.inv.invoice_type_code), Some("81" | "480")) {
        return;
    }
    for (i, l, d) in each(doc) {
        if !tax_written(l, d) {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-125-ae`, context `cac:Item`, test `boolean(cbc:Description)`.
fn ibr_125_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, d) in each(doc) {
        if item_written(l, d) && item(l).and_then(|it| text(&it.description)).is_none() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-126-ae`, context `cac:Price`, test
/// `boolean(cbc:BaseQuantity) and boolean(cac:AllowanceCharge/cbc:BaseAmount)`. One finding per
/// price: at the base quantity when it is missing, else at the gross price (F7).
fn ibr_126_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        if !price_written(d) {
            continue;
        }
        if !d.price.base_quantity.exists() {
            sink.fail(&[i]);
        } else if !d.price.gross_price.exists() {
            sink.fail_at(format!("lines[{i}].price.gross_price"))
                .term("IBT-148");
        }
    }
}

/// `ibr-145-ae`, context line, test `(cac:Item/cac:ClassifiedTaxCategory)`.
fn ibr_145_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, d) in each(doc) {
        if !tax_written(l, d) {
            sink.fail(&[i]);
        }
    }
}

/// IBT-131 computed by `ibr-147-ae`: `qty * (price div base) + sum(charges) - sum(allowances)`,
/// in decimal (`qty * price` first, so the quotient is rounded once, at 28 digits), the sum and
/// difference exact in [`Cents`]. `None` where the XPath has an empty or non-finite operand: a
/// missing quantity, price or base quantity, a zero base quantity (`INF` or `NaN` in
/// `xs:double`), or an overflow.
fn expected_net_amount(l: &pb::InvoiceLine, d: &LineDec<'_>) -> Option<Cents> {
    let quantity = d.quantity.value?;
    let price = d.price.net_price.value?;
    let base = d.price.base_quantity.value?;
    if base.is_zero() {
        return None;
    }
    let extended = decimal::mul(quantity, price)?.checked_div(base)?;
    let amounts = || {
        l.allowances_charges
            .iter()
            .zip(&d.allowances_charges)
            .filter_map(|(a, ad)| ad.amount.value.map(|v| (a.is_charge, v)))
    };
    let charges = Cents::sum(amounts().filter(|(c, _)| *c).map(|(_, v)| v))?;
    let allowances = Cents::sum(amounts().filter(|(c, _)| !*c).map(|(_, v)| v))?;
    Cents::of(extended)?
        .checked_add(charges)?
        .checked_sub(allowances)
}

/// `ibr-147-ae`, context line, test
/// `round(cbc:LineExtensionAmount * 100) div 100 = round((((cbc:InvoicedQuantity | cbc:CreditedQuantity) * (cac:Price/cbc:PriceAmount div cac:Price/cbc:BaseQuantity)) + sum(cac:AllowanceCharge[cbc:ChargeIndicator="true"]/cbc:Amount) - sum(cac:AllowanceCharge[cbc:ChargeIndicator="false"]/cbc:Amount)) * 100) div 100`.
/// Both sides are rounded to two decimals, half toward positive infinity.
fn ibr_147_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, d) in each(doc) {
        let expected = expected_net_amount(l, d).and_then(Cents::round_half_up);
        let net = d
            .net_amount
            .value
            .and_then(Cents::of)
            .and_then(Cents::round_half_up);
        let holds = match (net, expected) {
            (Some(net), Some(expected)) => net == expected,
            _ => false,
        };
        if !holds {
            let f = sink.fail(&[i]);
            if let Some(expected) = expected.and_then(Cents::to_decimal) {
                f.suggest(expected);
            }
        }
    }
}

/// `ibr-166-ae`, context `cac:Item`, test
/// `not(cac:ClassifiedTaxCategory/cbc:ID = "AE") or exists(cac:CommodityClassification/cbc:NatureCode)`.
fn ibr_166_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        if tax_code(l) == Some("AE")
            && item(l)
                .and_then(|it| text(&it.goods_service_type))
                .is_none()
        {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-167-ae`, context line, test
/// `not(cac:Item/cac:ClassifiedTaxCategory/cbc:ID = "E" and not(exists(cac:Item/cac:ClassifiedTaxCategory/cbc:TaxExemptionReasonCode)))`.
fn ibr_167_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        let reason = l.tax.as_ref().and_then(|c| text(&c.exemption_reason_code));
        if tax_code(l) == Some("E") && reason.is_none() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-184-ae`, context `cac:Item`, test
/// `not(cac:CommodityClassification/cbc:CommodityCode = "G") or cac:CommodityClassification/cbc:ItemClassificationCode`.
fn ibr_184_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        if item_type(l) == Some("G") && !has_classification(l) {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-185-ae`, context `cac:Item`, test
/// `not(cac:CommodityClassification/cbc:CommodityCode = "S") or cac:AdditionalItemIdentification/cbc:ID`.
fn ibr_185_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        if item_type(l) == Some("S") && !has_sac(l) {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-186-ae`, context `cac:Item`, test
/// `not(cac:CommodityClassification/cbc:CommodityCode = "B") or (exists(cac:CommodityClassification/cbc:ItemClassificationCode) and exists(cac:AdditionalItemIdentification/cbc:ID))`.
/// One finding per item: at the classification when it is missing, else at the service
/// accounting code (BTAE-17).
fn ibr_186_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        if item_type(l) != Some("B") {
            continue;
        }
        if !has_classification(l) {
            sink.fail(&[i]);
        } else if !has_sac(l) {
            sink.fail_at(format!("lines[{i}].item.service_accounting_codes[0].code"))
                .term("BTAE-17");
        }
    }
}

/// `ibr-188-ae`, context `cac:Item`, test
/// `not(cac:CommodityClassification/cbc:ItemClassificationCode) or cac:CommodityClassification/cbc:ItemClassificationCode/@listID = "HS"`.
/// The existential comparison passes when any written code has scheme `HS`; otherwise the finding
/// is at the first written code.
fn ibr_188_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        let Some(it) = item(l) else { continue };
        let Some((first, _)) = written_codes(&it.classifications).next() else {
            continue;
        };
        if !written_codes(&it.classifications).any(|(_, c)| text(&c.scheme_id) == Some("HS")) {
            sink.fail(&[i, first]).suggest("HS");
        }
    }
}

/// `ibr-189-ae`, context `cac:Item`, test
/// `not(cac:AdditionalItemIdentification/cbc:ID) or cac:AdditionalItemIdentification/cbc:ID/@schemeID = "SAC"`.
fn ibr_189_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        let Some(it) = item(l) else { continue };
        let Some((first, _)) = written_codes(&it.service_accounting_codes).next() else {
            continue;
        };
        if !written_codes(&it.service_accounting_codes)
            .any(|(_, c)| text(&c.scheme_id) == Some("SAC"))
        {
            sink.fail(&[i, first]).suggest("SAC");
        }
    }
}

/// `ibr-194-ae`, context line, test `exists(cac:ItemPriceExtension/cbc:Amount)`.
fn ibr_194_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, _, d) in each(doc) {
        if !d.amount_aed.exists() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-064`, context `cac:InvoiceLine/cac:Item/cac:StandardItemIdentification/cbc:ID | …`, test
/// `exists(@schemeID)`. The identifier is written with a non-empty id.
fn ibr_064(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        let Some(id) = item(l).and_then(|it| it.standard_id.as_ref()) else {
            continue;
        };
        if text(&id.id).is_some() && text(&id.scheme_id).is_none() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-065`, context `cac:InvoiceLine/cac:Item/cac:CommodityClassification/cbc:ItemClassificationCode | …`,
/// test `exists(@listID)`.
fn ibr_065(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        let Some(it) = item(l) else { continue };
        for (j, c) in written_codes(&it.classifications) {
            if text(&c.scheme_id).is_none() {
                sink.fail(&[i, j]);
            }
        }
    }
}

/// `ibr-sr-58`, context `cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory`, test
/// `exists(cbc:ID)`.
///
/// Upstream defect 6: unlike its neighbours the context has no `| cac:CreditNoteLine/...`
/// alternative, so the official schematron never fires it on a credit note; the rule follows
/// the official context and does not evaluate credit notes.
fn ibr_sr_58(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.kind != DocKind::Invoice {
        return;
    }
    for (i, l, d) in each(doc) {
        if tax_written(l, d) && tax_code(l).is_none() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-006-ae`, context `cbc:NatureCode`, test
/// `((not(contains(normalize-space(.), ' ')) and contains(' DL8.48.8.2 DL8.48.8.1 DL8.48.3.1 DL8.48.3.2 DL8.48.3.3 ', concat(' ', normalize-space(.), ' '))))`.
/// The five codes are list `ibr-006-ae` of `codelists.tsv`; the written `NatureCode` is BTAE-09.
fn ibr_006_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        if let Some(code) = item(l).and_then(|it| text(&it.goods_service_type))
            && !sets().contains("ibr-006-ae", code)
        {
            sink.fail(&[i]);
        }
    }
}

/// A line or invoicing period date that `xs:date` accepts (the others are reported by
/// `AE-EXP-008`; the official XPath would raise a dynamic error on them).
fn date(s: &str) -> Option<&str> {
    text(s).filter(|d| is_xsd_date(d))
}

/// The period of a line, when it is written (at least one date).
fn line_period(l: &pb::InvoiceLine) -> Option<&pb::Period> {
    l.period
        .as_ref()
        .filter(|p| text(&p.start_date).is_some() || text(&p.end_date).is_some())
}

/// `ibr-085`, context `cac:InvoiceLine/cac:InvoicePeriod | …`, test
/// `(cbc:StartDate >= xs:date(../../cac:InvoicePeriod/cbc:StartDate)) or not(cbc:StartDate) or not(../../cac:InvoicePeriod/cbc:StartDate)`.
/// Dates are `YYYY-MM-DD`, so the text order is the date order.
fn ibr_085(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let Some(start) = doc
        .inv
        .invoicing_period
        .as_ref()
        .and_then(|p| date(&p.start_date))
    else {
        return;
    };
    for (i, l, _) in each(doc) {
        if let Some(line) = line_period(l).and_then(|p| date(&p.start_date))
            && line < start
        {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-086`, same contexts, test
/// `(cbc:EndDate <= xs:date(../../cac:InvoicePeriod/cbc:EndDate)) or not(cbc:EndDate) or not(../../cac:InvoicePeriod/cbc:EndDate)`.
fn ibr_086(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let Some(end) = doc
        .inv
        .invoicing_period
        .as_ref()
        .and_then(|p| date(&p.end_date))
    else {
        return;
    };
    for (i, l, _) in each(doc) {
        if let Some(line) = line_period(l).and_then(|p| date(&p.end_date))
            && line > end
        {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-030`, same contexts, test
/// `(exists(cbc:EndDate) and exists(cbc:StartDate) and xs:date(cbc:EndDate) >= xs:date(cbc:StartDate)) or not(exists(cbc:StartDate)) or not(exists(cbc:EndDate))`.
fn ibr_030(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l, _) in each(doc) {
        let Some(p) = line_period(l) else { continue };
        if let (Some(start), Some(end)) = (date(&p.start_date), date(&p.end_date))
            && end < start
        {
            sink.fail(&[i]);
        }
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

    fn patched(slug: &str, set: Value, remove: &[&str]) -> pb::Invoice {
        let mut inv = example(slug);
        let set = set.as_object().expect("an object").clone();
        let remove: Vec<String> = remove.iter().map(|s| s.to_string()).collect();
        apply_patch(&mut inv, &set, &remove).expect("patch applies");
        inv
    }

    type Check = fn(&Doc<'_>, &mut Sink<'_>);

    /// `(path, suggested value)` of every finding of `rule` over `inv`.
    fn run(rule: Check, template: &str, inv: &pb::Invoice) -> Vec<(String, String)> {
        let doc = Doc::new(inv);
        let mut sink = Sink::new(template);
        rule(&doc, &mut sink);
        sink.into_findings()
            .into_iter()
            .map(|f| (f.path, f.suggested_value.unwrap_or_default()))
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
            .filter(|e| e.family == Family::Lines && e.status == Status::Implemented)
            .map(|e| e.rule_id)
            .collect();
        rows.sort_unstable();
        assert_eq!(registered, rows);
        assert_eq!(registered.len(), 35);
    }

    #[test]
    fn ibr_187_ae_is_a_constant_true_upstream() {
        let e = default_ruleset()
            .catalog()
            .get("ibr-187-ae")
            .expect("listed");
        assert_eq!(e.status, Status::UpstreamNoop);
        assert_eq!(UPSTREAM_NOOP, ["ibr-187-ae"]);
        assert!(RULES.iter().all(|r| !UPSTREAM_NOOP.contains(&r.id)));
        let upstream = include_str!("../../rulesets/pint-ae-1.0.4/upstream/rules-ae.tsv");
        let row = upstream
            .lines()
            .find(|l| l.starts_with("ibr-187-ae\t"))
            .expect("upstream row");
        assert_eq!(row.split('\t').nth(3), Some("true()"));
    }

    /// The ten `structural` rows: they cannot fail on the XML the exporter writes, whatever the
    /// document. Checked on documents with every field set and on degenerate ones.
    #[test]
    fn structural_rules_hold_on_every_exported_line() {
        let degenerate = patched(
            "standard-tax-invoice",
            json!({
                "lines[0].period.start_date": "",
                "lines[0].period.end_date": "",
                "lines[0].order_line_reference": "7",
                "lines[0].despatch_advice_reference": "D-1",
                "lines[0].object_identifier.id": "OBJ",
            }),
            &[],
        );
        let docs = [
            maximal("380"),
            maximal("381"),
            example("standard-invoice-extensive"),
            degenerate,
        ];
        for inv in &docs {
            let xml = export_str(inv);
            let tree = roxmltree::Document::parse(&xml).expect("well-formed");
            let lines: Vec<_> = tree
                .root_element()
                .children()
                .filter(|n| matches!(n.tag_name().name(), "InvoiceLine" | "CreditNoteLine"))
                .collect();
            assert_eq!(lines.len(), inv.lines.len());
            for line in lines {
                let count = |path: &[&str]| {
                    let mut level = vec![line];
                    for name in path {
                        level = level
                            .iter()
                            .flat_map(|n| n.children().filter(|c| c.tag_name().name() == *name))
                            .collect();
                    }
                    level
                };
                // ibr-sr-34, ibr-110, ibr-109, ibr-sr-50, ibr-sr-38, ibr-sr-62, ibr-111.
                for path in [
                    &["Note"][..],
                    &["InvoicePeriod"],
                    &["OrderLineReference"],
                    &["OrderLineReference", "LineID"],
                    &["Item", "Description"],
                    &["Item", "ClassifiedTaxCategory"],
                    &["Item", "ClassifiedTaxCategory", "TaxExemptionReason"],
                    &["DespatchLineReference"],
                    &["DespatchLineReference", "DocumentReference"],
                    &["Price", "AllowanceCharge"],
                    &["Price", "AllowanceCharge", "Amount"],
                ] {
                    assert!(count(path).len() <= 1, "{path:?}");
                }
                // ibr-089: at most one DocumentReference with type code 130 per line.
                let invoiced_objects = count(&["DocumentReference"])
                    .into_iter()
                    .filter(|r| {
                        r.children().any(|c| {
                            c.tag_name().name() == "DocumentTypeCode" && c.text() == Some("130")
                        })
                    })
                    .count();
                assert!(invoiced_objects <= 1);
                // ibr-083: the price-level charge indicator is always false.
                for ac in count(&["Price", "AllowanceCharge"]) {
                    let indicator: Vec<_> = ac
                        .children()
                        .filter(|c| c.tag_name().name() == "ChargeIndicator")
                        .collect();
                    assert_eq!(indicator.len(), 1);
                    assert_eq!(indicator[0].text(), Some("false"));
                }
                // ibr-co-20: a written line period has a start or an end date.
                for period in count(&["InvoicePeriod"]) {
                    assert!(
                        period
                            .children()
                            .any(|c| matches!(c.tag_name().name(), "StartDate" | "EndDate"))
                    );
                }
            }
        }
        // The line period message without a date is not written at all.
        let xml = export_str(&docs[3]);
        let tree = roxmltree::Document::parse(&xml).expect("well-formed");
        let line = tree
            .root_element()
            .children()
            .find(|n| n.tag_name().name() == "InvoiceLine")
            .expect("a line");
        assert!(
            !line
                .children()
                .any(|c| c.tag_name().name() == "InvoicePeriod")
        );
    }

    #[test]
    fn a_line_without_a_price_fails_both_the_presence_and_the_sign_rule() {
        let t = "lines[#].price.net_price";
        let inv = patched(
            "standard-tax-invoice",
            json!({"lines[0].price.net_price": ""}),
            &[],
        );
        assert_eq!(paths(run(ibr_026, t, &inv)), ["lines[0].price.net_price"]);
        assert_eq!(paths(run(ibr_027, t, &inv)), ["lines[0].price.net_price"]);
        // Zero and negative zero are not negative.
        for zero in ["0.00", "-0"] {
            let inv = patched(
                "standard-tax-invoice",
                json!({"lines[0].price.net_price": zero}),
                &[],
            );
            assert!(run(ibr_027, t, &inv).is_empty(), "{zero}");
        }
    }

    #[test]
    fn ibr_147_ae_rounds_half_up_and_proposes_the_computed_amount() {
        let t = "lines[#].net_amount";
        let with = |qty: &str, price: &str, net: &str| {
            patched(
                "standard-invoice-mandatory-fields",
                json!({
                    "lines[0].quantity": qty,
                    "lines[0].price.net_price": price,
                    "lines[0].price.gross_price": price,
                    "lines[0].price.discount": "0",
                    "lines[0].net_amount": net,
                }),
                &[],
            )
        };
        // 3 * 0.0533 = 0.1599 -> 0.16.
        assert!(run(ibr_147_ae, t, &with("3", "0.0533", "0.16")).is_empty());
        assert!(run(ibr_147_ae, t, &with("3", "0.0533", "0.1599")).is_empty());
        assert_eq!(
            run(ibr_147_ae, t, &with("3", "0.0533", "0.15")),
            [("lines[0].net_amount".to_string(), "0.16".to_string())]
        );
        // The half is rounded up: 2 * 0.2275 = 0.455 -> 0.46.
        assert!(run(ibr_147_ae, t, &with("2", "0.2275", "0.46")).is_empty());
        assert_eq!(run(ibr_147_ae, t, &with("2", "0.2275", "0.45")).len(), 1);
        // A zero base quantity makes the quotient infinite: the rule fires, without a proposal.
        let zero = patched(
            "standard-invoice-mandatory-fields",
            json!({"lines[0].price.base_quantity": "0"}),
            &[],
        );
        assert_eq!(
            run(ibr_147_ae, t, &zero),
            [("lines[0].net_amount".to_string(), String::new())]
        );
    }

    #[test]
    fn ibr_147_ae_does_not_lose_a_third() {
        // 3 * (1 / 3) is exactly 1 in the XPath and must be 1.00 here too.
        let inv = patched(
            "standard-invoice-mandatory-fields",
            json!({
                "lines[0].quantity": "3",
                "lines[0].price.net_price": "1",
                "lines[0].price.gross_price": "1",
                "lines[0].price.base_quantity": "3",
                "lines[0].net_amount": "1",
            }),
            &[],
        );
        assert!(run(ibr_147_ae, "lines[#].net_amount", &inv).is_empty());
    }

    #[test]
    fn line_charges_and_allowances_enter_ibr_147_ae_with_their_signs() {
        let t = "lines[#].net_amount";
        assert!(run(ibr_147_ae, t, &example("standard-tax-invoice")).is_empty());
        let flipped = patched(
            "standard-tax-invoice",
            json!({"lines[0].allowances_charges[0].is_charge": true}),
            &[],
        );
        assert_eq!(run(ibr_147_ae, t, &flipped).len(), 1);
    }

    #[test]
    fn period_rules_ignore_unreal_dates_and_a_missing_invoicing_period() {
        let start = "lines[#].period.start_date";
        let unreal = patched(
            "standard-tax-invoice",
            json!({"lines[0].period.start_date": "2025-02-30"}),
            &[],
        );
        assert!(run(ibr_085, start, &unreal).is_empty());
        assert!(run(ibr_030, "lines[#].period.end_date", &unreal).is_empty());
        let no_period = patched(
            "standard-tax-invoice",
            json!({"lines[0].period.start_date": "2020-01-01"}),
            &["invoicing_period"],
        );
        assert!(run(ibr_085, start, &no_period).is_empty());
        let early = patched(
            "standard-tax-invoice",
            json!({"lines[0].period.start_date": "2025-01-30"}),
            &[],
        );
        assert_eq!(
            paths(run(ibr_085, start, &early)),
            ["lines[0].period.start_date"]
        );
    }

    #[test]
    fn ibr_088_proposes_the_quantity_unit() {
        let t = "lines[#].price.base_quantity_unit_code";
        let inv = patched(
            "standard-tax-invoice",
            json!({"lines[0].price.base_quantity_unit_code": "KGM"}),
            &[],
        );
        assert_eq!(
            run(ibr_088, t, &inv),
            [(
                "lines[0].price.base_quantity_unit_code".to_string(),
                "H87".to_string()
            )]
        );
        // Without a quantity there is nothing to compare with.
        let no_qty = patched(
            "standard-tax-invoice",
            json!({"lines[0].price.base_quantity_unit_code": "KGM", "lines[0].quantity": ""}),
            &[],
        );
        assert!(run(ibr_088, t, &no_qty).is_empty());
    }

    #[test]
    fn ibr_126_ae_reports_one_finding_per_price_at_the_first_missing_field() {
        let t = "lines[#].price.base_quantity";
        let both = patched(
            "standard-tax-invoice",
            json!({"lines[0].price.base_quantity": "", "lines[0].price.gross_price": ""}),
            &[],
        );
        assert_eq!(
            paths(run(ibr_126_ae, t, &both)),
            ["lines[0].price.base_quantity"]
        );
        let gross = patched(
            "standard-tax-invoice",
            json!({"lines[0].price.gross_price": ""}),
            &[],
        );
        assert_eq!(
            paths(run(ibr_126_ae, t, &gross)),
            ["lines[0].price.gross_price"]
        );
        // No price at all: no Price element, no finding.
        let none = patched("standard-tax-invoice", json!({}), &["lines[0].price"]);
        assert!(run(ibr_126_ae, t, &none).is_empty());
    }

    #[test]
    fn item_and_tax_aggregates_exist_exactly_when_one_of_their_fields_does() {
        let mut inv = example("standard-tax-invoice");
        let doc = Doc::new(&inv);
        assert!(item_written(&inv.lines[0], &doc.lines[0]));
        inv.lines[0].item = None;
        inv.lines[0].tax = None;
        let doc = Doc::new(&inv);
        assert!(!item_written(&inv.lines[0], &doc.lines[0]));
        assert!(!tax_written(&inv.lines[0], &doc.lines[0]));
        // A tax scheme alone is written (and makes the category, so ibr-sr-58 fires); a lot
        // number makes the item (so ibr-125-ae fires).
        inv.lines[0].tax = Some(pb::TaxCategory {
            tax_scheme: "VAT".into(),
            ..Default::default()
        });
        inv.lines[0].batch_number = "L1".into();
        let doc = Doc::new(&inv);
        assert!(tax_written(&inv.lines[0], &doc.lines[0]));
        assert!(item_written(&inv.lines[0], &doc.lines[0]));
        assert_eq!(
            paths(run(ibr_sr_58, "lines[#].tax.code", &inv)),
            ["lines[0].tax.code"]
        );
        assert_eq!(
            paths(run(ibr_125_ae, "lines[#].item.description", &inv)),
            ["lines[0].item.description"]
        );
    }

    /// Upstream defect 6: the official context of `ibr-sr-58` is `cac:InvoiceLine/cac:Item/
    /// cac:ClassifiedTaxCategory` only (every neighbouring rule says `| cac:CreditNoteLine/...`),
    /// so Saxon never fires it on a credit note. The differential corpus found this (fuzz
    /// document 1698); the rule follows the official XPath.
    #[test]
    fn ibr_sr_58_applies_to_invoices_only_like_the_official_context() {
        let mut inv = example("standard-tax-invoice");
        inv.lines[0].tax.as_mut().expect("line tax").code = String::new();
        assert_eq!(
            paths(run(ibr_sr_58, "lines[#].tax.code", &inv)),
            ["lines[0].tax.code"]
        );
        inv.invoice_type_code = "381".into();
        assert!(run(ibr_sr_58, "lines[#].tax.code", &inv).is_empty());
        inv.invoice_type_code = "81".into();
        assert!(run(ibr_sr_58, "lines[#].tax.code", &inv).is_empty());
    }

    #[test]
    fn classification_rules_use_the_written_codes_and_their_model_index() {
        let t = "lines[#].item.classifications[#].scheme_id";
        let inv = patched(
            "standard-invoice-mandatory-fields",
            json!({
                "lines[0].item.classifications[0].code": "",
                "lines[0].item.classifications[1].code": "8501",
                "lines[0].item.classifications[1].scheme_id": "ZZZ",
            }),
            &[],
        );
        // Index 0 has no code and is not written, so index 1 is the first (only) written one.
        assert_eq!(
            run(ibr_188_ae, t, &inv),
            [(
                "lines[0].item.classifications[1].scheme_id".to_string(),
                "HS".to_string()
            )]
        );
        assert!(run(ibr_184_ae, "lines[#].item.classifications[0].code", &inv).is_empty());
        // One HS code among others satisfies the existential comparison.
        let mixed = patched(
            "standard-invoice-mandatory-fields",
            json!({
                "lines[0].item.classifications[1].code": "8501",
                "lines[0].item.classifications[1].scheme_id": "ZZZ",
            }),
            &[],
        );
        assert!(run(ibr_188_ae, t, &mixed).is_empty());
    }

    #[test]
    fn credit_note_lines_are_checked_like_invoice_lines() {
        let inv = patched("standard-tax-credit-note", json!({"lines[0].id": ""}), &[]);
        assert_eq!(paths(run(ibr_021, "lines[#].id", &inv)), ["lines[0].id"]);
    }

    /// A line with no written child is not an element of the exported XML (the unit code is an
    /// attribute of the absent quantity), so no line context sees it and, alone, it leaves the
    /// invoice without a line (Saxon: `ibr-016` and nothing of this family on the line).
    #[test]
    fn a_line_the_exporter_drops_is_not_a_line() {
        let ids = |inv: &pb::Invoice| -> Vec<String> {
            let doc = Doc::new(inv);
            let mut out = Vec::new();
            let catalog = default_ruleset().catalog();
            for rule in RULES {
                let mut sink = Sink::new(catalog.get(rule.id).unwrap().path);
                (rule.check)(&doc, &mut sink);
                out.extend(
                    sink.into_findings()
                        .into_iter()
                        .map(|_| rule.id.to_string()),
                );
            }
            out
        };
        let extra = patched(
            "standard-tax-invoice",
            json!({"lines[1].unit_code": "C62"}),
            &[],
        );
        assert_eq!(extra.lines.len(), 2);
        assert!(ids(&extra).is_empty(), "{:?}", ids(&extra));
        let alone = patched(
            "standard-invoice-mandatory-fields",
            json!({"lines[1].unit_code": "C62"}),
            &["lines[0]"],
        );
        assert_eq!(alone.lines.len(), 1);
        assert_eq!(ids(&alone), ["ibr-016"]);
        assert!(!export_str(&alone).contains("InvoiceLine"));
    }
}
