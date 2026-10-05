//! `header` family rules (spec 5.2.4): the 69 asserts whose context is the document root or one of
//! its document-level aggregates (`InvoicePeriod`, `Delivery`, `PaymentMeans`, `PaymentTerms`,
//! `BillingReference`, `AdditionalDocumentReference`), dates and times, `BinaryObject`,
//! `DiscrepancyResponse` and `CalculationRate`, plus the root rules about header business terms.
//! 38 are `implemented` here; 31 are `structural` (the canonical model and the exporter make a
//! violation impossible, proved by [`tests::structural_rules_hold_on_every_export`]); none is
//! `upstream_noop`. The rows are in `rulesets/pint-ae-1.0.4/coverage/header.tsv`, the failing
//! fixtures in `mutations/header.jsonl`.
//!
//! Every rule below means "this assert fails on the XML the exporter writes for this `Doc`" (CI
//! rule 13). Three facts about that XML decide most details:
//!
//! * an aggregate is written only when it has a child, so a context node such as `PaymentMeans`
//!   or `BillingReference` exists exactly when one of its fields is present (the `*_written`
//!   helpers; [`tests::payment_means_written_matches_the_exporter`] checks them against the
//!   exporter);
//! * Saxon applies, per node, only the first template of a pattern that matches it, so a broad
//!   context such as `cac:InvoicePeriod` does not reach the line periods that `ibr-030` takes
//!   first (the fixtures were checked against Saxon, `compare.py`);
//! * XPath `<` / `>=` between two untyped values is a string comparison, and `xs:date()` of text
//!   that is not a date is a dynamic error (the schematron run aborts), which a rule here treats
//!   as "nothing to compare" because `ibr-073` reports the date.

use rust_decimal::Decimal;

use super::platform::SELFBILLING_SPECIFICATION_PREFIX;
use crate::codelists;
use crate::doc::{Doc, DocKind, text};
use crate::pb;
use crate::rule::{Rule, Sink, fill};

/// Prefix of the in-scope specification identifier (CI rule 9).
const BILLING_SPECIFICATION_PREFIX: &str = "urn:peppol:pint:billing-1@ae-1";

pub static RULES: &[Rule] = &[
    Rule {
        id: "aligned-ibrp-001-ae",
        check: aligned_ibrp_001_ae,
    },
    Rule {
        id: "aligned-ibrp-002-ae",
        check: aligned_ibrp_002_ae,
    },
    Rule {
        id: "ibr-001-ae",
        check: ibr_001_ae,
    },
    Rule {
        id: "ibr-002",
        check: ibr_002,
    },
    Rule {
        id: "ibr-002-ae",
        check: ibr_002_ae,
    },
    Rule {
        id: "ibr-003",
        check: ibr_003,
    },
    Rule {
        id: "ibr-004",
        check: ibr_004,
    },
    Rule {
        id: "ibr-005",
        check: ibr_005,
    },
    Rule {
        id: "ibr-005-ae",
        check: ibr_005_ae,
    },
    Rule {
        id: "ibr-029",
        check: ibr_029,
    },
    Rule {
        id: "ibr-049",
        check: ibr_049,
    },
    Rule {
        id: "ibr-052",
        check: ibr_052,
    },
    Rule {
        id: "ibr-054",
        check: ibr_054,
    },
    Rule {
        id: "ibr-055",
        check: ibr_055,
    },
    Rule {
        id: "ibr-055-ae",
        check: ibr_055_ae,
    },
    Rule {
        id: "ibr-057",
        check: ibr_057,
    },
    Rule {
        id: "ibr-066",
        check: ibr_066,
    },
    Rule {
        id: "ibr-067",
        check: ibr_067,
    },
    Rule {
        id: "ibr-073",
        check: ibr_073,
    },
    Rule {
        id: "ibr-077",
        check: ibr_077,
    },
    Rule {
        id: "ibr-119",
        check: ibr_119,
    },
    Rule {
        id: "ibr-124-ae",
        check: ibr_124_ae,
    },
    Rule {
        id: "ibr-127-ae",
        check: ibr_127_ae,
    },
    Rule {
        id: "ibr-138-ae",
        check: ibr_138_ae,
    },
    Rule {
        id: "ibr-140-ae",
        check: ibr_140_ae,
    },
    Rule {
        id: "ibr-141-ae",
        check: ibr_141_ae,
    },
    Rule {
        id: "ibr-142-ae",
        check: ibr_142_ae,
    },
    Rule {
        id: "ibr-152-ae",
        check: ibr_152_ae,
    },
    Rule {
        id: "ibr-154-ae",
        check: ibr_154_ae,
    },
    Rule {
        id: "ibr-157-ae",
        check: ibr_157_ae,
    },
    Rule {
        id: "ibr-158-ae",
        check: ibr_158_ae,
    },
    Rule {
        id: "ibr-159-ae",
        check: ibr_159_ae,
    },
    Rule {
        id: "ibr-160-ae",
        check: ibr_160_ae,
    },
    Rule {
        id: "ibr-191-ae",
        check: ibr_191_ae,
    },
    Rule {
        id: "ibr-192-ae",
        check: ibr_192_ae,
    },
    Rule {
        id: "ibr-193-ae",
        check: ibr_193_ae,
    },
    Rule {
        id: "ibr-sr-07",
        check: ibr_sr_07,
    },
    Rule {
        id: "ibr-sr-63",
        check: ibr_sr_63,
    },
];

// ------------------------------------------------------------------------------------------------
// Helpers

/// XML whitespace, the characters `normalize-space` strips and collapses.
fn is_xml_space(c: char) -> bool {
    matches!(c, ' ' | '\t' | '\r' | '\n')
}

/// `normalize-space(a) = normalize-space(b)` for text that [`text`] has already trimmed.
fn same_normalized(a: &str, b: &str) -> bool {
    a.split(is_xml_space)
        .filter(|w| !w.is_empty())
        .eq(b.split(is_xml_space).filter(|w| !w.is_empty()))
}

/// `^[0-9]+(\.[0-9]{1,6})?$`.
fn is_rate_text(s: &str) -> bool {
    let (int, fraction) = match s.split_once('.') {
        Some((int, fraction)) => (int, Some(fraction)),
        None => (s, None),
    };
    !int.is_empty()
        && int.bytes().all(|b| b.is_ascii_digit())
        && fraction
            .is_none_or(|f| (1..=6).contains(&f.len()) && f.bytes().all(|b| b.is_ascii_digit()))
}

/// Two ASCII digits at `i`.
fn two_digits(b: &[u8], i: usize) -> Option<u32> {
    match (b.get(i), b.get(i + 1)) {
        (Some(x @ b'0'..=b'9'), Some(y @ b'0'..=b'9')) => {
            Some(u32::from(x - b'0') * 10 + u32::from(y - b'0'))
        }
        _ => None,
    }
}

/// A real day of the proleptic Gregorian calendar (year 0 is a leap year, as in XML Schema 1.1).
fn real_day(y: u32, m: u32, d: u32) -> bool {
    let leap = y.is_multiple_of(4) && (!y.is_multiple_of(100) || y.is_multiple_of(400));
    let days = match m {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if leap => 29,
        2 => 28,
        _ => return false,
    };
    (1..=days).contains(&d)
}

/// `string-length(text()) = 10 and (string(.) castable as xs:date)` for [`text`]: ten ASCII
/// characters `YYYY-MM-DD` that are a real day, year 0000 included. Returns `(year, month, day)`.
fn plain_date(s: &str) -> Option<(u32, u32, u32)> {
    let b = s.as_bytes();
    if b.len() != 10 || b[4] != b'-' || b[7] != b'-' {
        return None;
    }
    let year = two_digits(b, 0)? * 100 + two_digits(b, 2)?;
    let (month, day) = (two_digits(b, 5)?, two_digits(b, 8)?);
    real_day(year, month, day).then_some((year, month, day))
}

/// `string(.) castable as xs:time`: `hh:mm:ss`, an optional fraction, an optional `Z` or
/// `[+-]hh:mm` zone. `24:00:00` (zero fraction) is the end of the day; Saxon accepts zone hours up
/// to 14 with any minute below 60.
fn is_xs_time(s: &str) -> bool {
    let b = s.as_bytes();
    if b.len() < 8 || b[2] != b':' || b[5] != b':' {
        return false;
    }
    let (Some(h), Some(m), Some(sec)) = (two_digits(b, 0), two_digits(b, 3), two_digits(b, 6))
    else {
        return false;
    };
    let mut rest = &b[8..];
    let mut fraction_is_zero = true;
    if rest.first() == Some(&b'.') {
        let digits = rest[1..].iter().take_while(|c| c.is_ascii_digit()).count();
        if digits == 0 {
            return false;
        }
        fraction_is_zero = rest[1..=digits].iter().all(|&c| c == b'0');
        rest = &rest[digits + 1..];
    }
    let zone_ok = match rest {
        [] | [b'Z'] => true,
        [b'+' | b'-', zone @ ..] => {
            zone.len() == 5
                && zone[2] == b':'
                && two_digits(zone, 0).is_some_and(|zh| zh <= 14)
                && two_digits(zone, 3).is_some_and(|zm| zm <= 59)
        }
        _ => false,
    };
    let time_ok = if h == 24 {
        m == 0 && sec == 0 && fraction_is_zero
    } else {
        h < 24 && m < 60 && sec < 60
    };
    zone_ok && time_ok
}

/// The exporter writes the address element: any of its seven fields is present.
fn address_written(a: &pb::PostalAddress) -> bool {
    [
        &a.line1,
        &a.line2,
        &a.line3,
        &a.city,
        &a.post_code,
        &a.country_subdivision,
        &a.country_code,
    ]
    .into_iter()
    .any(|s| text(s).is_some())
}

/// The exporter writes a `CardAccount` (it adds the `NA` network id itself).
fn card_written(c: &pb::PaymentCard) -> bool {
    [&c.primary_account_number, &c.holder_name, &c.network_id]
        .into_iter()
        .any(|s| text(s).is_some())
}

/// The exporter writes a `PaymentMandate`.
fn mandate_written(d: &pb::DirectDebit) -> bool {
    text(&d.mandate_reference).is_some() || text(&d.debited_account).is_some()
}

/// The exporter writes `cac:PaymentMeans` for `payment_instructions[i]`: one of its children
/// (`ID`, `PaymentMeansCode`, a `PaymentID`, `CardAccount`, `PayeeFinancialAccount`,
/// `PaymentMandate`) is present, or it is the first instruction of a credit note that has IBT-009
/// (`PaymentDueDate`, which the CreditNote XSD only allows there).
fn payment_means_written(doc: &Doc<'_>, i: usize) -> bool {
    let inv = doc.inv;
    let pi = &inv.payment_instructions[i];
    let account = |ct: &pb::CreditTransfer| {
        ct.account.as_ref().is_some_and(|a| text(&a.id).is_some())
            || text(&ct.account_name).is_some()
            || text(&ct.service_provider_id).is_some()
            || ct.institution_address.as_ref().is_some_and(address_written)
    };
    text(&pi.id).is_some()
        || text(&pi.means_code).is_some()
        || (i == 0 && doc.kind == DocKind::CreditNote && text(&inv.payment_due_date).is_some())
        || pi
            .remittance_information
            .iter()
            .any(|r| text(&r.id).is_some())
        || pi.card.as_ref().is_some_and(card_written)
        || pi.credit_transfer.as_ref().is_some_and(account)
        || pi.direct_debit.as_ref().is_some_and(mandate_written)
}

/// The exporter writes the document-level `cac:InvoicePeriod`.
fn invoicing_period_written(inv: &pb::Invoice) -> bool {
    inv.invoicing_period
        .as_ref()
        .is_some_and(|p| text(&p.start_date).is_some() || text(&p.end_date).is_some())
        || text(&inv.billing_frequency).is_some()
}

/// The first of IBT-075, IBT-077, IBT-079 that the deliver-to address lacks: its path and term.
fn first_missing_delivery_field(inv: &pb::Invoice) -> Option<(&'static str, &'static str)> {
    let address = inv.delivery.as_ref().and_then(|d| d.address.as_ref());
    let has =
        |field: fn(&pb::PostalAddress) -> &str| address.is_some_and(|a| text(field(a)).is_some());
    if !has(|a| &a.line1) {
        Some(("delivery.address.line1", "IBT-075"))
    } else if !has(|a| &a.city) {
        Some(("delivery.address.city", "IBT-077"))
    } else if !has(|a| &a.country_subdivision) {
        Some(("delivery.address.country_subdivision", "IBT-079"))
    } else {
        None
    }
}

/// `ibr-055` and `ibr-sr-07`: a `BillingReference` (written for IBT-025 or IBT-026) without IBT-025.
fn preceding_invoices_without_id(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, p) in doc.inv.preceding_invoices.iter().enumerate() {
        if text(&p.id).is_none() && text(&p.issue_date).is_some() {
            sink.fail(&[i]);
        }
    }
}

// ------------------------------------------------------------------------------------------------
// Rules

/// `aligned-ibrp-001-ae` (error, fix `llm`): IBT-024 (defaulted, trimmed) starts with neither accepted prefix; the prefix match is case-sensitive.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `starts-with(normalize-space(cbc:CustomizationID/text()), 'urn:peppol:pint:billing-1@ae-1') or starts-with(normalize-space(cbc:CustomizationID/text()), 'urn:peppol:pint:selfbilling-1@ae-1')`
fn aligned_ibrp_001_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let id = doc.specification_identifier();
    if !(id.starts_with(BILLING_SPECIFICATION_PREFIX)
        || id.starts_with(SELFBILLING_SPECIFICATION_PREFIX))
    {
        sink.fail(&[]);
    }
}

/// `aligned-ibrp-002-ae` (error, fix `llm`): IBT-023 (defaulted, trimmed) starts with neither `urn:peppol:bis:billing` nor `urn:peppol:bis:selfbilling`. The exporter always writes `ProfileID`, so the `exists` half of the test cannot fail.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `/*/cbc:ProfileID and (matches(normalize-space(/*/cbc:ProfileID), '^urn:peppol:bis:billing') or matches(normalize-space(/*/cbc:ProfileID), '^urn:peppol:bis:selfbilling'))`
fn aligned_ibrp_002_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let process = doc.business_process_type();
    if !(process.starts_with("urn:peppol:bis:billing")
        || process.starts_with("urn:peppol:bis:selfbilling"))
    {
        sink.fail(&[]);
    }
}

/// `ibr-001-ae` (error, fix `llm`): A credit note (the only root that carries `DiscrepancyResponse`) whose BTAE-03 is not one of the six reason codes (list `ibr-001-ae`). A code with an inner blank is never a list member.
///
/// Official context: `cac:DiscrepancyResponse/cbc:ResponseCode`
///
/// Official test: `( ( not(contains(normalize-space(.),' ')) and contains( ' DL8.61.1.A DL8.61.1.B DL8.61.1.C DL8.61.1.D DL8.61.1.E VD ',concat(' ',normalize-space(.),' ') ) ) )`
fn ibr_001_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.kind == DocKind::CreditNote
        && let Some(code) = text(&doc.inv.credit_note_reason_code)
        && !codelists::sets().contains("ibr-001-ae", code)
    {
        sink.fail(&[]);
    }
}

/// `ibr-002` (error, fix `none`): IBT-001 absent or blank.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `normalize-space(cbc:ID) !=''`
fn ibr_002(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.invoice_number).is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-002-ae` (error, fix `llm`): BTAE-04 written (a valid decimal) in a form other than digits with at most six decimals.
///
/// Official context: `cbc:CalculationRate`
///
/// Official test: `matches(., concat('^[0-9]+(\.[0-9]', codepoints-to-string(123), '1,6', codepoints-to-string(125), ')?$'))`
fn ibr_002_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.exchange_rate.exists() && !is_rate_text(doc.exchange_rate.raw) {
        sink.fail(&[]);
    }
}

/// `ibr-003` (error, fix `none`): IBT-002 absent or blank.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `normalize-space(cbc:IssueDate) !=''`
fn ibr_003(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.issue_date).is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-004` (error, fix `none`): IBT-003 absent or blank (neither type-code element is written).
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `normalize-space(cbc:InvoiceTypeCode) !='' or normalize-space(cbc:CreditNoteTypeCode) !=''`
fn ibr_004(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.invoice_type_code).is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-005` (error, fix `none`): IBT-005 absent or blank.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `normalize-space(cbc:DocumentCurrencyCode) !=''`
fn ibr_005(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.currency).is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-005-ae` (error, fix `llm`): BTAE-06 (`InvoicePeriod/DescriptionCode`) present and not in the frequency list (`ibr-005-ae`).
///
/// Official context: `cac:InvoicePeriod/cbc:DescriptionCode`
///
/// Official test: `((not(contains(normalize-space(.), ' ')) and contains(' DLY WKY Q15 MTH Q45 Q60 QTR YRL HYR OTH ', concat(' ', normalize-space(.), ' '))))`
fn ibr_005_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(code) = text(&doc.inv.billing_frequency)
        && !codelists::sets().contains("ibr-005-ae", code)
    {
        sink.fail(&[]);
    }
}

/// `ibr-029` (error, fix `llm`): The document-level period has both dates, both castable as `xs:date`, and the end is before the start. The line periods are `ibr-030`'s: Saxon matches them with the earlier template of the same pattern (priority 1029 against 1028). An uncastable date is a dynamic error in XPath; the rule then reports nothing, because `ibr-073` reports the date, and a date with a time zone is not compared.
///
/// Official context: `cac:InvoicePeriod`
///
/// Official test: `(exists(cbc:EndDate) and exists(cbc:StartDate) and xs:date(cbc:EndDate) >= xs:date(cbc:StartDate)) or not(exists(cbc:StartDate)) or not(exists(cbc:EndDate))`
fn ibr_029(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let Some(p) = &doc.inv.invoicing_period else {
        return;
    };
    if let (Some(start), Some(end)) = (text(&p.start_date), text(&p.end_date))
        && let (Some(start), Some(end)) = (plain_date(start), plain_date(end))
        && end < start
    {
        sink.fail(&[]);
    }
}

/// `ibr-049` (error, fix `none`): A `PaymentMeans` the exporter writes (`payment_means_written`) without `PaymentMeansCode`.
///
/// Official context: `cac:PaymentMeans`
///
/// Official test: `exists(cbc:PaymentMeansCode)`
fn ibr_049(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, pi) in doc.inv.payment_instructions.iter().enumerate() {
        if text(&pi.means_code).is_none() && payment_means_written(doc, i) {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-052` (error, fix `none`): A supporting document the exporter writes (IBT-122, IBT-123 or IBT-124 present) without IBT-122. The context also matches the invoiced object, the project reference and BTAE-20, which always carry an `ID`; an attachment-only document is not written.
///
/// Official context: `cac:AdditionalDocumentReference`
///
/// Official test: `(normalize-space(cbc:ID)) != ''`
fn ibr_052(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, d) in doc.inv.supporting_documents.iter().enumerate() {
        let written = text(&d.reference).is_some()
            || text(&d.description).is_some()
            || text(&d.external_uri).is_some();
        if written && text(&d.reference).is_none() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-054` (error, fix `none`): An item attribute with a name (an unnamed one is not written) and no value.
///
/// Official context: `//cac:AdditionalItemProperty`
///
/// Official test: `exists(cbc:Name) and exists(cbc:Value)`
fn ibr_054(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, l) in doc.inv.lines.iter().enumerate() {
        let Some(item) = &l.item else { continue };
        for (j, a) in item.attributes.iter().enumerate() {
            if text(&a.name).is_some() && text(&a.value).is_none() {
                sink.fail(&[i, j]);
            }
        }
    }
}

/// `ibr-055` (error, fix `none`): A preceding invoice written (IBT-025 or IBT-026 present) without IBT-025.
///
/// Official context: `cac:BillingReference`
///
/// Official test: `exists(cac:InvoiceDocumentReference/cbc:ID)`
fn ibr_055(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    preceding_invoices_without_id(doc, sink);
}

/// `ibr-055-ae` (error, fix `none`): A credit note whose `BillingReference` / `ResponseCode` combination is not (reference present and a code other than `VD`) or (no reference and `VD`); an absent code makes both branches false. Path and term are BTAE-03 when the reason is absent, or is `VD` while references exist (the reason or the references must change), otherwise IBT-025 (the first reference to add).
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `(((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "381" or (cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81") and (((cac:BillingReference) and  (cac:DiscrepancyResponse/cbc:ResponseCode != "VD")) or (not(cac:BillingReference) and  (cac:DiscrepancyResponse/cbc:ResponseCode = "VD"))))  or not((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "381" or (cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81")`
fn ibr_055_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.kind != DocKind::CreditNote {
        return;
    }
    let references = doc
        .inv
        .preceding_invoices
        .iter()
        .any(|p| text(&p.id).is_some() || text(&p.issue_date).is_some());
    let reason = text(&doc.inv.credit_note_reason_code);
    let holds = match reason {
        Some("VD") => !references,
        Some(_) => references,
        None => false,
    };
    if holds {
        return;
    }
    if reason.is_none() || references {
        sink.fail_at("credit_note_reason_code").term("BTAE-03");
    } else {
        sink.fail(&[]);
    }
}

/// `ibr-057` (error, fix `none`): A delivery address the exporter writes (any of its seven fields present) without a country code.
///
/// Official context: `cac:Delivery/cac:DeliveryLocation/cac:Address`
///
/// Official test: `exists(cac:Country/cbc:IdentificationCode)`
fn ibr_057(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(a) = doc.inv.delivery.as_ref().and_then(|d| d.address.as_ref())
        && address_written(a)
        && text(&a.country_code).is_none()
    {
        sink.fail(&[]);
    }
}

/// `ibr-066` (error, fix `none`): More than one `CardAccount` (a card with IBT-087, IBT-088 or a network id); one finding, at the second.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `count(cac:PaymentMeans/cac:CardAccount) <= 1`
fn ibr_066(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let mut cards = doc
        .inv
        .payment_instructions
        .iter()
        .enumerate()
        .filter(|(_, pi)| pi.card.as_ref().is_some_and(card_written));
    if let (Some(_), Some((i, _))) = (cards.next(), cards.next()) {
        sink.fail(&[i]);
    }
}

/// `ibr-067` (error, fix `none`): More than one `PaymentMandate` (a direct debit with IBT-089 or IBT-091); one finding, at the second.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `count(cac:PaymentMeans/cac:PaymentMandate) <= 1`
fn ibr_067(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let mut mandates = doc
        .inv
        .payment_instructions
        .iter()
        .enumerate()
        .filter(|(_, pi)| pi.direct_debit.as_ref().is_some_and(mandate_written));
    if let (Some(_), Some((i, _))) = (mandates.next(), mandates.next()) {
        sink.fail(&[i]);
    }
}

/// `ibr-073` (error, fix `llm`): One finding per written date element that is not exactly ten characters castable as `xs:date` (XML Schema 1.1: year 0000 passes): IBT-002, IBT-007, IBT-009 (an invoice's `DueDate`; a credit note's `PaymentDueDate` is outside the context, `AE-EXP-008`), IBT-026, IBT-072, IBT-073, IBT-074, IBT-134, IBT-135.
///
/// Official context: `cbc:IssueDate | cbc:DueDate | cbc:TaxPointDate | cbc:StartDate | cbc:EndDate | cbc:ActualDeliveryDate`
///
/// Official test: `string-length(text()) = 10 and (string(.) castable as xs:date)`
fn ibr_073(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    let mut check = |template: &'static str, idx: &[usize], term: &'static str, value: &str| {
        if let Some(date) = text(value)
            && plain_date(date).is_none()
        {
            sink.fail_at(fill(template, idx)).term(term);
        }
    };
    check("issue_date", &[], "IBT-002", &inv.issue_date);
    check("tax_point_date", &[], "IBT-007", &inv.tax_point_date);
    if doc.kind == DocKind::Invoice {
        check("payment_due_date", &[], "IBT-009", &inv.payment_due_date);
    }
    for (i, p) in inv.preceding_invoices.iter().enumerate() {
        check(
            "preceding_invoices[#].issue_date",
            &[i],
            "IBT-026",
            &p.issue_date,
        );
    }
    if let Some(d) = &inv.delivery {
        check(
            "delivery.actual_delivery_date",
            &[],
            "IBT-072",
            &d.actual_delivery_date,
        );
    }
    if let Some(p) = &inv.invoicing_period {
        check("invoicing_period.start_date", &[], "IBT-073", &p.start_date);
        check("invoicing_period.end_date", &[], "IBT-074", &p.end_date);
    }
    for (i, l) in inv.lines.iter().enumerate() {
        if let Some(p) = &l.period {
            check("lines[#].period.start_date", &[i], "IBT-134", &p.start_date);
            check("lines[#].period.end_date", &[i], "IBT-135", &p.end_date);
        }
    }
}

/// `ibr-077` (error, fix `none`): IBT-006 present and equal, after `normalize-space`, to IBT-005 (an absent IBT-005 is the empty string).
///
/// Official context: `cbc:TaxCurrencyCode`
///
/// Official test: `not(normalize-space(text()) = normalize-space(../cbc:DocumentCurrencyCode/text()))`
fn ibr_077(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(tax) = text(&doc.inv.tax_currency)
        && same_normalized(tax, text(&doc.inv.currency).unwrap_or(""))
    {
        sink.fail(&[]);
    }
}

/// `ibr-119` (error, fix `llm`): IBT-168 (`cbc:IssueTime`, the only CBC element whose name ends in `Time`) that is not castable as `xs:time`.
///
/// Official context: `//*[namespace-uri()='urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2' and ends-with(local-name(), 'Time')]`
///
/// Official test: `(string(.) castable as xs:time)`
fn ibr_119(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(time) = text(&doc.inv.issue_time)
        && !is_xs_time(time)
    {
        sink.fail(&[]);
    }
}

/// `ibr-124-ae` (error, fix `none`): A credit note (`381` or `81`) with IBT-007.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "381" or (cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81") or not(cbc:TaxPointDate)`
fn ibr_124_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.kind == DocKind::CreditNote && text(&doc.inv.tax_point_date).is_some() {
        sink.fail(&[]);
    }
}

/// `ibr-127-ae` (error, fix `none`): An invoice (a credit note passes on its type code; `CreditNoteTypeCode = 261` is never true, because `261` is written as `InvoiceTypeCode`) that is not a deemed supply (BTAE-02 position 2), with IBT-115 greater than zero (a valid decimal) and no `DueDate`.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "381" or (cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81" or cbc:CreditNoteTypeCode = "261" or matches(cbc:ProfileExecutionID, "^[01]1[01]{6}$")) or not(cac:LegalMonetaryTotal/cbc:PayableAmount > 0) or exists(cbc:DueDate)`
fn ibr_127_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.kind == DocKind::Invoice
        && !doc.txn(2)
        && doc
            .totals
            .payable_amount
            .value
            .is_some_and(|v| v > Decimal::ZERO)
        && text(&doc.inv.payment_due_date).is_none()
    {
        sink.fail(&[]);
    }
}

/// `ibr-138-ae` (error, fix `none`): A summary invoice (BTAE-02 position 4) without a document-level `InvoicePeriod` (written when IBT-073, IBT-074 or BTAE-06 is present).
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `(matches(cbc:ProfileExecutionID, "^[01]{3}1[01]{4}$") and cac:InvoicePeriod) or not(matches(cbc:ProfileExecutionID, "^[01]{3}1[01]{4}$"))`
fn ibr_138_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.txn(4) && !invoicing_period_written(doc.inv) {
        sink.fail(&[]);
    }
}

/// `ibr-140-ae` (error, fix `llm`): IBT-006 present and other than `AED`.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not(cbc:TaxCurrencyCode) or cbc:TaxCurrencyCode = "AED"`
fn ibr_140_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.tax_currency).is_some_and(|c| c != "AED") {
        sink.fail(&[]);
    }
}

/// `ibr-141-ae` (error, fix `none`): IBT-007 present and not before IBT-002. `<` between two untyped values is a string comparison in XPath (code point order, which is byte order of UTF-8); an absent IBT-002 makes it false.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not(cbc:TaxPointDate) or cbc:TaxPointDate < cbc:IssueDate`
fn ibr_141_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(point) = text(&doc.inv.tax_point_date)
        && !text(&doc.inv.issue_date).is_some_and(|issue| point < issue)
    {
        sink.fail(&[]);
    }
}

/// `ibr-142-ae` (error, fix `none`): An e-commerce supply (BTAE-02 position 7) whose deliver-to address lacks IBT-075, IBT-077 or IBT-079; one finding, at the first missing field.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not(matches(cbc:ProfileExecutionID, "^[01]{6}1[01]$")) or (cac:Delivery/cac:DeliveryLocation/cac:Address/cbc:StreetName and cac:Delivery/cac:DeliveryLocation/cac:Address/cbc:CityName and  cac:Delivery/cac:DeliveryLocation/cac:Address/cbc:CountrySubentity)`
fn ibr_142_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.txn(7)
        && let Some((path, term)) = first_missing_delivery_field(doc.inv)
    {
        sink.fail_at(path).term(term);
    }
}

/// `ibr-152-ae` (error, fix `none`): An export (BTAE-02 position 8) whose deliver-to country is not `AE` (or absent) and whose address lacks IBT-075, IBT-077 or IBT-079; one finding, at the first missing field.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not(matches(cbc:ProfileExecutionID, "^[01]{7}1$")) or cac:Delivery/cac:DeliveryLocation/cac:Address/cac:Country/cbc:IdentificationCode = "AE" or (cac:Delivery/cac:DeliveryLocation/cac:Address/cbc:StreetName and cac:Delivery/cac:DeliveryLocation/cac:Address/cbc:CityName and cac:Delivery/cac:DeliveryLocation/cac:Address/cbc:CountrySubentity)`
fn ibr_152_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let country = doc
        .inv
        .delivery
        .as_ref()
        .and_then(|d| d.address.as_ref())
        .and_then(|a| text(&a.country_code));
    if doc.txn(8)
        && country != Some("AE")
        && let Some((path, term)) = first_missing_delivery_field(doc.inv)
    {
        sink.fail_at(path).term(term);
    }
}

/// `ibr-154-ae` (error, fix `llm`): BTAE-02 absent or not exactly eight characters of `0` and `1` (the message says 'no more than 8'; the regex is authoritative).
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `(matches(cbc:ProfileExecutionID, "^[01]{8}$"))`
fn ibr_154_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let valid = text(&doc.inv.transaction_type_code)
        .is_some_and(|c| c.len() == 8 && c.bytes().all(|b| matches!(b, b'0' | b'1')));
    if !valid {
        sink.fail(&[]);
    }
}

/// `ibr-157-ae` (error, fix `none`): IBT-003 is `480` or `81` and BTAE-02 has position 2, 3 or 4 set.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not(((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "480" or (cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81") and  matches(cbc:ProfileExecutionID, "^[01]{2}1[01]{5}$|^[01]1[01]{6}$|^[01]{3}1[01]{4}$"))`
fn ibr_157_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if matches!(text(&doc.inv.invoice_type_code), Some("480" | "81"))
        && (doc.txn(2) || doc.txn(3) || doc.txn(4))
    {
        sink.fail(&[]);
    }
}

/// `ibr-158-ae` (error, fix `none`): IBT-003 is exactly `381` and BTAE-03 is absent.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "381" and not(exists(cac:DiscrepancyResponse/cbc:ResponseCode)))`
fn ibr_158_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.invoice_type_code) == Some("381")
        && text(&doc.inv.credit_note_reason_code).is_none()
    {
        sink.fail(&[]);
    }
}

/// `ibr-159-ae` (error, fix `none`): Neither IBT-005 is `AED` nor (IBT-005 present and BTAE-04 written); an absent IBT-005 fails both branches.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `(cbc:DocumentCurrencyCode != "AED" and (exists(//cac:TaxExchangeRate/cbc:CalculationRate))) or cbc:DocumentCurrencyCode = "AED"`
fn ibr_159_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let currency = text(&doc.inv.currency);
    let holds = currency == Some("AED") || (currency.is_some() && doc.exchange_rate.exists());
    if !holds {
        sink.fail(&[]);
    }
}

/// `ibr-160-ae` (error, fix `none`): BTAE-06 is exactly `OTH` and IBT-022 is absent.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not(cac:InvoicePeriod/cbc:DescriptionCode = "OTH" and not(exists(cbc:Note)))`
fn ibr_160_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.billing_frequency) == Some("OTH") && text(&doc.inv.note).is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-191-ae` (error, fix `none`): An invoice (a credit note passes; the `261` disjunct is never true, see `ibr-127-ae`) that is not a deemed supply (BTAE-02 position 2) and has no `PaymentMeansCode`.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `((cbc:InvoiceTypeCode|cbc:CreditNoteTypeCode) = "81" or (cbc:InvoiceTypeCode|cbc:CreditNoteTypeCode) = "381" or (cbc:InvoiceTypeCode|cbc:CreditNoteTypeCode) = "261" or matches(cbc:ProfileExecutionID, "^[01]1[01]{6}$")) or exists(cac:PaymentMeans/cbc:PaymentMeansCode)`
fn ibr_191_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.kind == DocKind::Invoice
        && !doc.txn(2)
        && !doc
            .inv
            .payment_instructions
            .iter()
            .any(|pi| text(&pi.means_code).is_some())
    {
        sink.fail(&[]);
    }
}

/// `ibr-192-ae` (error, fix `none`): A `PaymentMeans` with code exactly `30` and no `PayeeFinancialAccount/ID` (IBT-084).
///
/// Official context: `cac:PaymentMeans`
///
/// Official test: `not(cbc:PaymentMeansCode = "30") or cac:PayeeFinancialAccount/cbc:ID`
fn ibr_192_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, pi) in doc.inv.payment_instructions.iter().enumerate() {
        let account = pi
            .credit_transfer
            .as_ref()
            .and_then(|ct| ct.account.as_ref())
            .and_then(|a| text(&a.id));
        if text(&pi.means_code) == Some("30") && account.is_none() {
            sink.fail(&[i]);
        }
    }
}

/// `ibr-193-ae` (error, fix `none`): BTAE-07 absent or blank.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `exists(cbc:UUID)`
fn ibr_193_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.uuid).is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-sr-07` (error, fix `none`): Same condition as `ibr-055`: a preceding invoice written without IBT-025.
///
/// Official context: `cac:BillingReference`
///
/// Official test: `(cac:InvoiceDocumentReference/cbc:ID)`
fn ibr_sr_07(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    preceding_invoices_without_id(doc, sink);
}

/// `ibr-sr-63` (error, fix `llm`): IBT-024 (defaulted, trimmed) contains `*`.
///
/// Official context: `/ubl:Invoice | /cn:CreditNote`
///
/// Official test: `not(contains(normalize-space(cbc:CustomizationID), '*'))`
fn ibr_sr_63(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.specification_identifier().contains('*') {
        sink.fail(&[]);
    }
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use roxmltree::{Document, Node};
    use serde_json::{Map, Value};

    use super::*;
    use crate::catalog::{self, Family, Status};
    use crate::conformance::{apply_patch, examples, mutations};
    use crate::export::{self, testing::maximal};

    /// A payment-instruction shape: label, example, instruction fields, is `PaymentMeans` written.
    type Shape<'a> = (&'a str, &'a str, &'a [(&'a str, &'a str)], bool);

    fn xml_of(inv: &pb::Invoice) -> String {
        String::from_utf8(export::to_xml(&Doc::new(inv)).expect("exportable")).unwrap()
    }

    fn patched(base: &str, set: &[(&str, &str)], remove: &[&str]) -> pb::Invoice {
        let mut inv = examples()
            .into_iter()
            .find(|(slug, _)| slug == base)
            .unwrap_or_else(|| panic!("no example {base}"))
            .1;
        let map: Map<String, Value> = set
            .iter()
            .map(|(k, v)| ((*k).to_string(), Value::String((*v).to_string())))
            .collect();
        let remove: Vec<String> = remove.iter().map(|s| (*s).to_string()).collect();
        apply_patch(&mut inv, &map, &remove).unwrap();
        inv
    }

    /// `(text, castable as xs:date with exactly ten characters)`, measured with SaxonC-HE 13.0.
    #[test]
    fn dates_follow_the_saxon_cast() {
        let saxon: &[(&str, bool)] = &[
            ("2024-02-29", true),
            ("2023-02-29", false),
            ("0000-01-01", true),
            ("0000-02-29", true),
            ("9999-12-31", true),
            ("2024-1-31", false),
            ("2024-13-01", false),
            ("2024-00-10", false),
            ("2024-01-00", false),
            ("2024-01-32", false),
            ("2024-04-31", false),
            ("١٢٣٤-01-01", false),
            ("2024-01-01Z", false),
            ("+024-01-01", false),
            ("-024-01-01", false),
        ];
        for (s, castable) in saxon {
            assert_eq!(plain_date(s).is_some(), *castable, "{s}");
        }
        assert_eq!(plain_date("2025-02-06"), Some((2025, 2, 6)));
    }

    /// `(text, castable as xs:time)`, measured with SaxonC-HE 13.0.
    #[test]
    fn times_follow_the_saxon_cast() {
        let saxon: &[(&str, bool)] = &[
            ("12:00:00", true),
            ("24:00:00", true),
            ("24:00:01", false),
            ("24:00:00.000", true),
            ("24:01:00", false),
            ("23:59:59.999", true),
            ("23:59:60", false),
            ("12:00", false),
            ("12:00:00Z", true),
            ("12:00:00+14:00", true),
            ("12:00:00+14:30", true),
            ("12:00:00+14:59", true),
            ("12:00:00+15:00", false),
            ("12:00:00-14:30", true),
            ("12:00:00+04:00", true),
            ("12:00:00+4:00", false),
            ("12:00:00.", false),
            ("12:00:00.5+05:30", true),
            ("1:00:00", false),
            ("12:0:00", false),
            ("12:00:00 +04:00", false),
            ("12:00:00+04", false),
            ("12:00:00z", false),
            ("12:60:00", false),
            ("00:00:00-00:00", true),
            ("12:00:00+05:60", false),
            ("12:00:00+23:00", false),
        ];
        for (s, castable) in saxon {
            assert_eq!(is_xs_time(s), *castable, "{s}");
        }
    }

    #[test]
    fn rates_have_at_most_six_decimals_and_no_sign() {
        for ok in ["3", "3.6", "3.672850", "0.000001", "007.5"] {
            assert!(is_rate_text(ok), "{ok}");
        }
        for bad in [
            "",
            ".5",
            "3.",
            "3.6728501",
            "-3.6",
            "+3.6",
            "3,6",
            "3.6e0",
            "١.٥",
        ] {
            assert!(!is_rate_text(bad), "{bad}");
        }
    }

    #[test]
    fn normalize_space_collapses_inner_blanks_only() {
        assert!(same_normalized("AED", "AED"));
        assert!(same_normalized("A  E\tD", "A E D"));
        assert!(!same_normalized("AED", "USD"));
        assert!(!same_normalized("AED", ""));
        assert!(same_normalized("", ""));
    }

    /// `payment_means_written` is the context-node test of `ibr-049`; it must say exactly when the
    /// exporter writes `cac:PaymentMeans`.
    #[test]
    fn payment_means_written_matches_the_exporter() {
        let shapes: &[Shape] = &[
            ("an id", "standard-tax-invoice", &[("id", "P1")], true),
            (
                "a code",
                "standard-tax-invoice",
                &[("means_code", "30")],
                true,
            ),
            (
                "a text only",
                "standard-tax-invoice",
                &[("means_text", "x")],
                false,
            ),
            (
                "a remittance id",
                "standard-tax-invoice",
                &[("remittance_information[0].id", "R")],
                true,
            ),
            (
                "a remittance scheme only",
                "standard-tax-invoice",
                &[("remittance_information[0].scheme_id", "S")],
                false,
            ),
            (
                "a card number",
                "standard-tax-invoice",
                &[("card.primary_account_number", "X")],
                true,
            ),
            (
                "a card network",
                "standard-tax-invoice",
                &[("card.network_id", "V")],
                true,
            ),
            (
                "a card holder",
                "standard-tax-invoice",
                &[("card.holder_name", "H")],
                true,
            ),
            (
                "an account id",
                "standard-tax-invoice",
                &[("credit_transfer.account.id", "A")],
                true,
            ),
            (
                "an account scheme only",
                "standard-tax-invoice",
                &[("credit_transfer.account.scheme_id", "IBAN")],
                false,
            ),
            (
                "an account name",
                "standard-tax-invoice",
                &[("credit_transfer.account_name", "N")],
                true,
            ),
            (
                "a service provider",
                "standard-tax-invoice",
                &[("credit_transfer.service_provider_id", "B")],
                true,
            ),
            (
                "an institution address",
                "standard-tax-invoice",
                &[("credit_transfer.institution_address.city", "C")],
                true,
            ),
            (
                "a mandate",
                "standard-tax-invoice",
                &[("direct_debit.mandate_reference", "M")],
                true,
            ),
            (
                "a debited account",
                "standard-tax-invoice",
                &[("direct_debit.debited_account", "D")],
                true,
            ),
            (
                "a creditor identifier only",
                "standard-tax-invoice",
                &[("direct_debit.creditor_identifier", "C")],
                false,
            ),
            (
                "nothing",
                "standard-tax-invoice",
                &[("means_text", " ")],
                false,
            ),
            (
                "a credit note due date on a text-only first instruction",
                "standard-tax-credit-note",
                &[("means_text", "x")],
                true,
            ),
        ];
        for (label, base, fields, written) in shapes {
            let credit_note_due = *base == "standard-tax-credit-note";
            let mut set: Vec<(String, &str)> = fields
                .iter()
                .map(|(k, v)| (format!("payment_instructions[0].{k}"), *v))
                .collect();
            if credit_note_due {
                set.push(("payment_due_date".into(), "2025-02-20"));
            }
            let set: Vec<(&str, &str)> = set.iter().map(|(k, v)| (k.as_str(), *v)).collect();
            let mut inv = patched(base, &[], &[]);
            inv.payment_instructions.clear();
            let map: Map<String, Value> = set
                .iter()
                .map(|(k, v)| ((*k).to_string(), Value::String((*v).to_string())))
                .collect();
            apply_patch(&mut inv, &map, &[]).unwrap();
            let doc = Doc::new(&inv);
            assert_eq!(inv.payment_instructions.len(), 1, "{label}");
            assert_eq!(payment_means_written(&doc, 0), *written, "{label}");
            let in_xml = xml_of(&inv).matches("<cac:PaymentMeans>").count();
            assert_eq!(in_xml, usize::from(*written), "{label}: the exporter");
        }
    }

    // --------------------------------------------------------------------------------------------
    // The structural rows

    /// The `structural` rows of `coverage/header.tsv`.
    const STRUCTURAL: [&str; 31] = [
        "ibr-001",
        "ibr-071",
        "ibr-072",
        "ibr-074",
        "ibr-075",
        "ibr-076",
        "ibr-078",
        "ibr-079",
        "ibr-090",
        "ibr-093",
        "ibr-094",
        "ibr-095",
        "ibr-096",
        "ibr-097",
        "ibr-107",
        "ibr-108",
        "ibr-196-ae",
        "ibr-co-19",
        "ibr-sr-05",
        "ibr-sr-06",
        "ibr-sr-27",
        "ibr-sr-28",
        "ibr-sr-33",
        "ibr-sr-39",
        "ibr-sr-46",
        "ibr-sr-49",
        "ibr-sr-51",
        "ibr-sr-52",
        "ibr-sr-56",
        "ibr-sr-59",
        "ibr-sr-60",
    ];

    type Els<'a, 'i> = Vec<Node<'a, 'i>>;

    fn local<'a>(n: Node<'a, '_>) -> &'a str {
        n.tag_name().name()
    }

    /// The element children named `name`.
    fn kids<'a, 'i>(n: Node<'a, 'i>, name: &str) -> Els<'a, 'i> {
        n.children()
            .filter(|c| c.is_element() && local(*c) == name)
            .collect()
    }

    /// The elements at the `/`-separated relative path.
    fn at<'a, 'i>(n: Node<'a, 'i>, path: &str) -> Els<'a, 'i> {
        path.split('/').fold(vec![n], |cur, step| {
            cur.into_iter().flat_map(|c| kids(c, step)).collect()
        })
    }

    /// Every element named `name`, at any depth (`//cac:Name`).
    fn anywhere<'a, 'i>(doc: &'a Document<'i>, name: &str) -> Els<'a, 'i> {
        doc.descendants()
            .filter(|n| n.is_element() && local(*n) == name)
            .collect()
    }

    fn has_text(n: &Node<'_, '_>) -> bool {
        n.children()
            .filter(Node::is_text)
            .any(|t| t.text().unwrap_or("").chars().any(|c| !is_xml_space(c)))
    }

    fn with_type<'a, 'i>(refs: Els<'a, 'i>, code: &str) -> Els<'a, 'i> {
        refs.into_iter()
            .filter(|r| {
                at(*r, "DocumentTypeCode")
                    .iter()
                    .any(|c| c.text() == Some(code))
            })
            .collect()
    }

    /// The structural rows' official tests, evaluated on one exported document: the rule ids that
    /// would fail.
    fn structural_violations(xml: &str) -> BTreeSet<&'static str> {
        let doc = Document::parse(xml).unwrap();
        let root = doc.root_element();
        let mut bad = BTreeSet::new();
        let mut fail = |id: &'static str| {
            bad.insert(id);
        };

        // Root-level counts: `count(cac:X/cbc:ID) <= 1`.
        for (id, path) in [
            ("ibr-094", "ContractDocumentReference/ID"),
            ("ibr-095", "ReceiptDocumentReference/ID"),
            ("ibr-096", "DespatchDocumentReference/ID"),
            ("ibr-sr-52", "OriginatorDocumentReference/ID"),
            ("ibr-sr-39", "ProjectReference/ID"),
            ("ibr-097", "InvoicePeriod"),
            ("ibr-107", "Delivery"),
            ("ibr-sr-49", "InvoicePeriod/DescriptionCode"),
            ("ibr-sr-51", "Note"),
        ] {
            if at(root, path).len() > 1 {
                fail(id);
            }
        }
        if !at(root, "CustomizationID").iter().any(has_text) {
            fail("ibr-001");
        }
        if at(root, "ProfileID").is_empty() {
            fail("ibr-076");
        }
        let refs = kids(root, "AdditionalDocumentReference");
        if with_type(refs.clone(), "130").len() > 1 {
            fail("ibr-078");
        }
        if kids(root, "ProjectReference").len() > 1 || with_type(refs, "50").len() > 1 {
            fail("ibr-090");
        }
        // `cac:InvoicePeriod` (the document-level one; Saxon gives line periods to ibr-co-20).
        for p in kids(root, "InvoicePeriod") {
            let (start, end) = (!at(p, "StartDate").is_empty(), !at(p, "EndDate").is_empty());
            if !(start || end || (!at(p, "DescriptionCode").is_empty() && !start && !end)) {
                fail("ibr-co-19");
            }
        }
        // `cac:PrepaidPayment[1]`.
        for p in anywhere(&doc, "PrepaidPayment") {
            let first = !p
                .prev_siblings()
                .skip(1)
                .any(|s| s.is_element() && local(s) == "PrepaidPayment");
            let parent = p.parent_element().unwrap();
            if first && at(parent, "LegalMonetaryTotal/PrepaidAmount").is_empty() {
                fail("ibr-093");
            }
        }
        for r in anywhere(&doc, "AdditionalDocumentReference") {
            let type_130 = !with_type(vec![r], "130").is_empty();
            if type_130 && !at(r, "Attachment").is_empty() {
                fail("ibr-071");
            }
            if type_130 && !at(r, "DocumentDescription").is_empty() {
                fail("ibr-072");
            }
            if at(r, "DocumentDescription").len() > 1 {
                fail("ibr-sr-33");
            }
        }
        for o in doc
            .descendants()
            .filter(|n| n.is_element() && local(*n).ends_with("BinaryObject"))
        {
            if o.attribute("mimeCode").is_none() {
                fail("ibr-074");
            }
            if o.attribute("filename").is_none() {
                fail("ibr-075");
            }
        }
        // `//*[not(*) and not(normalize-space())]`.
        for n in doc.descendants().filter(Node::is_element) {
            if !n.children().any(|c| c.is_element()) && !has_text(&n) {
                fail("ibr-079");
            }
        }
        for d in anywhere(&doc, "Delivery") {
            if at(d, "DeliveryParty/PartyName/Name").len() > 1 {
                fail("ibr-108");
            }
            for a in at(d, "DeliveryLocation/Address") {
                if at(a, "AddressLine/Line").len() > 1 {
                    fail("ibr-sr-56");
                }
            }
        }
        for t in anywhere(&doc, "DeliveryTerms") {
            if at(t, "ID").is_empty() {
                fail("ibr-196-ae");
            }
        }
        for t in anywhere(&doc, "PaymentTerms") {
            if at(t, "Note").len() > 1 {
                fail("ibr-sr-05");
            }
            if at(t, "PaymentMeansID").len() > 1 {
                fail("ibr-sr-60");
            }
        }
        for b in anywhere(&doc, "BillingReference") {
            if at(b, "InvoiceDocumentReference").len() > 1 {
                fail("ibr-sr-06");
            }
        }
        for m in anywhere(&doc, "PaymentMeans") {
            let codes = at(m, "PaymentMeansCode");
            if codes.len() > 1 {
                fail("ibr-sr-27");
            }
            if codes
                .iter()
                .filter(|c| c.attribute("name").is_some())
                .count()
                > 1
            {
                fail("ibr-sr-46");
            }
            if at(m, "PaymentMandate/ID").len() > 1 {
                fail("ibr-sr-28");
            }
            for a in at(
                m,
                "PayeeFinancialAccount/FinancialInstitutionBranch/Address",
            ) {
                if at(a, "AddressLine/Line").len() > 1 {
                    fail("ibr-sr-59");
                }
            }
        }
        bad
    }

    /// The checker above must find every structural violation; otherwise the next test proves
    /// nothing.
    #[test]
    fn the_structural_checker_reports_every_violation() {
        const BAD: &str = r#"<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"
  xmlns:cac="urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2"
  xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">
  <cbc:CustomizationID> </cbc:CustomizationID>
  <cbc:Note>a</cbc:Note><cbc:Note>b</cbc:Note>
  <cac:InvoicePeriod><cbc:DescriptionCode>OTH</cbc:DescriptionCode><cbc:DescriptionCode>MTH</cbc:DescriptionCode></cac:InvoicePeriod>
  <cac:InvoicePeriod><cbc:ID>x</cbc:ID></cac:InvoicePeriod>
  <cac:BillingReference><cac:InvoiceDocumentReference><cbc:ID>1</cbc:ID></cac:InvoiceDocumentReference><cac:InvoiceDocumentReference><cbc:ID>2</cbc:ID></cac:InvoiceDocumentReference></cac:BillingReference>
  <cac:DespatchDocumentReference><cbc:ID>1</cbc:ID><cbc:ID>2</cbc:ID></cac:DespatchDocumentReference>
  <cac:ReceiptDocumentReference><cbc:ID>1</cbc:ID><cbc:ID>2</cbc:ID></cac:ReceiptDocumentReference>
  <cac:OriginatorDocumentReference><cbc:ID>1</cbc:ID><cbc:ID>2</cbc:ID></cac:OriginatorDocumentReference>
  <cac:ContractDocumentReference><cbc:ID>1</cbc:ID><cbc:ID>2</cbc:ID></cac:ContractDocumentReference>
  <cac:AdditionalDocumentReference><cbc:ID>a</cbc:ID><cbc:DocumentTypeCode>130</cbc:DocumentTypeCode><cac:Attachment><cbc:ID>x</cbc:ID></cac:Attachment><cbc:DocumentDescription>d</cbc:DocumentDescription><cbc:DocumentDescription>e</cbc:DocumentDescription></cac:AdditionalDocumentReference>
  <cac:AdditionalDocumentReference><cbc:ID>b</cbc:ID><cbc:DocumentTypeCode>130</cbc:DocumentTypeCode></cac:AdditionalDocumentReference>
  <cac:AdditionalDocumentReference><cbc:ID>c</cbc:ID><cbc:DocumentTypeCode>50</cbc:DocumentTypeCode></cac:AdditionalDocumentReference>
  <cac:AdditionalDocumentReference><cbc:ID>d</cbc:ID><cbc:DocumentTypeCode>50</cbc:DocumentTypeCode><cac:Attachment><cbc:EmbeddedDocumentBinaryObject>QQ==</cbc:EmbeddedDocumentBinaryObject></cac:Attachment></cac:AdditionalDocumentReference>
  <cac:ProjectReference><cbc:ID>p</cbc:ID><cbc:ID>q</cbc:ID></cac:ProjectReference>
  <cac:Delivery>
    <cac:DeliveryLocation><cac:Address><cac:AddressLine><cbc:Line>a</cbc:Line></cac:AddressLine><cac:AddressLine><cbc:Line>b</cbc:Line></cac:AddressLine></cac:Address></cac:DeliveryLocation>
    <cac:DeliveryParty><cac:PartyName><cbc:Name>a</cbc:Name></cac:PartyName><cac:PartyName><cbc:Name>b</cbc:Name></cac:PartyName></cac:DeliveryParty>
    <cac:DeliveryTerms><cbc:Amount>1</cbc:Amount></cac:DeliveryTerms>
  </cac:Delivery>
  <cac:Delivery><cbc:ID>1</cbc:ID></cac:Delivery>
  <cac:PaymentMeans>
    <cbc:PaymentMeansCode name="a">30</cbc:PaymentMeansCode><cbc:PaymentMeansCode name="b">49</cbc:PaymentMeansCode>
    <cac:PayeeFinancialAccount><cac:FinancialInstitutionBranch><cac:Address><cac:AddressLine><cbc:Line>a</cbc:Line></cac:AddressLine><cac:AddressLine><cbc:Line>b</cbc:Line></cac:AddressLine></cac:Address></cac:FinancialInstitutionBranch></cac:PayeeFinancialAccount>
    <cac:PaymentMandate><cbc:ID>1</cbc:ID><cbc:ID>2</cbc:ID></cac:PaymentMandate>
  </cac:PaymentMeans>
  <cac:PaymentTerms><cbc:Note>a</cbc:Note><cbc:Note>b</cbc:Note><cbc:PaymentMeansID>1</cbc:PaymentMeansID><cbc:PaymentMeansID>2</cbc:PaymentMeansID></cac:PaymentTerms>
  <cac:PrepaidPayment><cbc:ID>1</cbc:ID></cac:PrepaidPayment>
</Invoice>"#;
        let want: BTreeSet<&str> = STRUCTURAL.into_iter().collect();
        let got = structural_violations(BAD);
        assert_eq!(
            want.symmetric_difference(&got).collect::<Vec<_>>(),
            Vec::<&&str>::new(),
            "the checker finds exactly the 31 structural violations of the bad document"
        );
    }

    /// `structural` rows of `coverage/header.tsv`: the exporter's output, for every shape the
    /// tests can build, passes every one of their official tests.
    #[test]
    fn structural_rules_hold_on_every_export() {
        let rows: BTreeSet<&str> = catalog::pint_ae_1_0_4()
            .entries()
            .iter()
            .filter(|e| e.family == Family::Header && e.status == Status::Structural)
            .map(|e| e.rule_id)
            .collect();
        let listed: BTreeSet<&str> = STRUCTURAL.into_iter().collect();
        assert_eq!(
            rows, listed,
            "STRUCTURAL is the set of structural header rows"
        );

        let mut docs: Vec<(String, pb::Invoice)> = examples();
        docs.push(("maximal invoice".into(), maximal("380")));
        docs.push(("maximal credit note".into(), maximal("381")));
        let bases = examples();
        for m in mutations(Family::Header).unwrap() {
            let inv = m.apply(&bases).unwrap();
            docs.push((m.id.clone(), inv));
        }
        for (name, inv) in &docs {
            let Ok(bytes) = export::to_xml(&Doc::new(inv)) else {
                continue;
            };
            let xml = String::from_utf8(bytes).unwrap();
            let bad = structural_violations(&xml);
            assert!(bad.is_empty(), "{name}: {bad:?}");
        }
        assert!(docs.len() >= 90, "{} documents", docs.len());
    }
}
