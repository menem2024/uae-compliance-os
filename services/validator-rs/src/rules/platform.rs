//! Platform rules (`AE-*`, spec 5.2.7): conditions the official schematron does not check but
//! the platform needs, because the canonical model can express them and the exporter cannot
//! write them. Their messages, severity and fix kind are in `coverage/platform.tsv`; their
//! failing fixtures are in `mutations/platform.jsonl`.
//!
//! `AE-EXP-001..005` are the spec's; `AE-EXP-006..010` are the result of the XSD audit of the
//! exporter (spec 5.2.7 `AE-EXP-00N`): `006` and `007` are the XSD-required children no official
//! rule, placeholder or omission covers (recorded with every other required child in
//! `export::XSD_AUDIT`), `008` to `010` the dates and times the official XPath casts accept and
//! the XML Schema 1.0 datatypes reject.

use crate::decimal::Cents;
use crate::doc::{Dec, Doc, DocKind, text};
use crate::export::writer::is_forbidden_xml_char;
use crate::pb;
use crate::rule::{Rule, Sink, fill};

/// Prefix of the out-of-scope self-billing specification (CI rule 9).
pub const SELFBILLING_SPECIFICATION_PREFIX: &str = "urn:peppol:pint:selfbilling-1@ae-1";

pub static RULES: &[Rule] = &[
    Rule {
        id: "AE-FMT-001",
        check: ae_fmt_001,
    },
    Rule {
        id: "AE-SCOPE-001",
        check: ae_scope_001,
    },
    Rule {
        id: "AE-EXP-001",
        check: ae_exp_001,
    },
    Rule {
        id: "AE-EXP-002",
        check: ae_exp_002,
    },
    Rule {
        id: "AE-EXP-003",
        check: ae_exp_003,
    },
    Rule {
        id: "AE-EXP-004",
        check: ae_exp_004,
    },
    Rule {
        id: "AE-EXP-005",
        check: ae_exp_005,
    },
    Rule {
        id: "AE-EXP-006",
        check: ae_exp_006,
    },
    Rule {
        id: "AE-EXP-007",
        check: ae_exp_007,
    },
    Rule {
        id: "AE-EXP-008",
        check: ae_exp_008,
    },
    Rule {
        id: "AE-EXP-009",
        check: ae_exp_009,
    },
    Rule {
        id: "AE-EXP-010",
        check: ae_exp_010,
    },
];

/// `AE-FMT-001` (error, fix `llm`): a decimal field breaks the contract rule 10 grammar
/// (spec 5.2.2). One finding per non-empty field of [`Doc::DECIMAL_FIELDS`] that fails
/// `decimal::parse`, at that field's path, with that field's business term as both the issue's
/// term and `message_args.term` (F7). Every other rule sees such a field as absent.
fn ae_fmt_001(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    doc.each_decimal(|field, idx, term, dec| {
        if dec.is_invalid() {
            sink.fail_at(fill(field.path, idx))
                .term(term)
                .arg("term", term);
        }
    });
}

/// `AE-SCOPE-001` (error, fix `none`): `process.specification_identifier`, trimmed and with the
/// exporter's default, starts with `urn:peppol:pint:selfbilling-1@ae-1`. Self-billing is out of
/// scope (CI rule 9) although the official `aligned-ibrp-001-ae` accepts it.
fn ae_scope_001(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc
        .specification_identifier()
        .starts_with(SELFBILLING_SPECIFICATION_PREFIX)
    {
        sink.fail(&[]);
    }
}

/// `AE-EXP-001` (warning, fix `none`): a credit note (`381`/`81`) with IBT-009 and no
/// `payment_instructions`. The CreditNote XSD has no root `DueDate`; IBT-009 is written as
/// `PaymentMeans/PaymentDueDate` of `payment_instructions[0]` (CI IBT-009 row), so without an
/// instruction the due date is left out of the XML.
fn ae_exp_001(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    if doc.kind == DocKind::CreditNote
        && text(&inv.payment_due_date).is_some()
        && inv.payment_instructions.is_empty()
    {
        sink.fail(&[]);
    }
}

/// `AE-EXP-002` (error, fix `value`): `vat_amount` (IBT-110) empty while `tax_breakdown` is not.
/// The document-currency `TaxTotal` (which carries the breakdown) is written only with IBT-110,
/// and `TaxTotal/TaxAmount` is XSD-mandatory (contract 0.2.1). The suggestion, when every
/// breakdown IBT-117 parses, is the one IBT-110 `ibr-co-14` accepts:
/// `xpath_round2(sum(tax_breakdown[#].tax_amount))`, exact in [`Cents`] as `ibr-co-14` is. An
/// invalid IBT-110 is `AE-FMT-001`'s.
fn ae_exp_002(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if !doc.vat_amount.raw.is_empty() || doc.inv.tax_breakdown.is_empty() {
        return;
    }
    let mut total = Some(Cents::ZERO);
    for t in &doc.tax_breakdown {
        total = match (total, t.tax_amount.value.and_then(Cents::of)) {
            (Some(acc), Some(v)) => acc.checked_add(v),
            _ => None,
        };
    }
    let f = sink.fail(&[]);
    if let Some(total) = total
        .and_then(Cents::round_half_up)
        .and_then(Cents::to_decimal)
    {
        f.suggest(total);
    }
}

/// `AE-EXP-003` (error, fix `none`): BTAE-04 (written as `TaxExchangeRate` when it parses) without
/// IBT-006; `TaxExchangeRate/TargetCurrencyCode` is XSD-mandatory.
fn ae_exp_003(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc.exchange_rate.exists() && text(&doc.inv.tax_currency).is_none() {
        sink.fail(&[]);
    }
}

/// `AE-EXP-004` (error, fix `none`): a payment card that is written (it has a holder name or a
/// network id) without IBT-087; `CardAccount/PrimaryAccountNumberID` is XSD-mandatory and no
/// official rule requires it (XSD audit). `NetworkID` gets the `NA` placeholder instead.
fn ae_exp_004(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (i, pi) in doc.inv.payment_instructions.iter().enumerate() {
        if let Some(c) = &pi.card
            && text(&c.primary_account_number).is_none()
            && (text(&c.holder_name).is_some() || text(&c.network_id).is_some())
        {
            sink.fail(&[i]);
        }
    }
}

/// `AE-EXP-005` (error, fix `none`): a string field the exporter writes holds, after the
/// `doc::text` trim, a character XML 1.0 forbids (U+0000-U+0008, U+000B, U+000C,
/// U+000E-U+001F, U+FFFE, U+FFFF). One finding per field, at its path (F7); such a document
/// cannot be serialised (`export::ExportError::ForbiddenChar`), so this rule is what keeps
/// `export` from ever failing on a run without errors.
fn ae_exp_005(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    each_exported_string(doc.inv, &mut |template, idx, value| {
        if text(value).is_some_and(|t| t.chars().any(is_forbidden_xml_char)) {
            sink.fail_at(fill(template, idx));
        }
    });
}

/// `AE-EXP-006` (error, fix `none`): BTAE-05 (written as
/// `ContractDocumentReference/DocumentDescription` when it parses) without IBT-012;
/// `ContractDocumentReference/ID` is XSD-mandatory and no official rule requires IBT-012
/// (`ibr-094` only counts it). Found by the XSD audit.
fn ae_exp_006(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let reference = doc
        .inv
        .references
        .as_ref()
        .and_then(|r| text(&r.contract_reference));
    if doc.contract_value.exists() && reference.is_none() {
        sink.fail(&[]);
    }
}

/// `AE-EXP-007` (error, fix `none`): no `LegalMonetaryTotal` amount parses (IBT-106 to IBT-109,
/// IBT-112 to IBT-115), so the XSD-mandatory `LegalMonetaryTotal` cannot be written. Found by
/// the XSD audit: `ibr-012` to `ibr-015` need the element to exist, and `ibr-co-15` passes when
/// IBT-200 is true.
fn ae_exp_007(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let t = &doc.totals;
    let amounts: [&Dec<'_>; 8] = [
        &t.line_extension_amount,
        &t.tax_exclusive_amount,
        &doc.total_amount,
        &t.allowance_total_amount,
        &t.charge_total_amount,
        &t.paid_amount,
        &t.rounding_amount,
        &t.payable_amount,
    ];
    if !amounts.iter().any(|d| d.exists()) {
        sink.fail(&[]);
    }
}

/// `AE-EXP-008` (error, fix `llm`): an `xsd:date` the exporter writes outside the `ibr-073`
/// context (`cbc:IssueDate | cbc:DueDate | cbc:TaxPointDate | cbc:StartDate | cbc:EndDate |
/// cbc:ActualDeliveryDate`) that is not [`is_xsd_date`]: a credit note's IBT-009
/// (`PaymentMeans/cbc:PaymentDueDate`, written when there are payment instructions) and every
/// IBT-177 (`PaymentTerms/cbc:InstallmentDueDate`). Path and term per field (F7).
fn ae_exp_008(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    if doc.kind == DocKind::CreditNote
        && !inv.payment_instructions.is_empty()
        && text(&inv.payment_due_date).is_some_and(|d| !is_xsd_date(d))
    {
        sink.fail_at("payment_due_date")
            .term("IBT-009")
            .arg("term", "IBT-009");
    }
    for (i, pt) in inv.payment_terms.iter().enumerate() {
        if text(&pt.installment_due_date).is_some_and(|d| !is_xsd_date(d)) {
            sink.fail_at(fill("payment_terms[#].installment_due_date", &[i]))
                .term("IBT-177")
                .arg("term", "IBT-177");
        }
    }
}

/// `AE-EXP-009` (error, fix `llm`): a date in the `ibr-073` context with the year 0000.
/// `ibr-073` (`string-length(text()) = 10 and (string(.) castable as xs:date)`) accepts it,
/// because XPath 2.0 casts with XML Schema 1.1 semantics, while the UBL 2.1 XSD is validated as
/// XML Schema 1.0, which has no year 0000. Found by the XSD audit. Path and term per field (F7):
/// IBT-002, IBT-007, IBT-009 (invoices: `cbc:DueDate`), IBT-026, IBT-072, IBT-073, IBT-074,
/// IBT-134, IBT-135.
fn ae_exp_009(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let inv = doc.inv;
    let mut check = |template: &'static str, idx: &[usize], term: &'static str, value: &str| {
        if text(value).is_some_and(is_year_zero_date) {
            sink.fail_at(fill(template, idx))
                .term(term)
                .arg("term", term);
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

/// `AE-EXP-010` (error, fix `llm`): IBT-168 (`cbc:IssueTime`) is a time `ibr-119`
/// (`string(.) castable as xs:time`) accepts but the UBL 2.1 XSD rejects: an otherwise valid
/// time whose offset is beyond ±14:00 (±14:01 to ±14:59). Saxon allows these minutes, XML Schema
/// 1.0 bounds the offset at ±14:00. Found by the XSD audit (5,040 measured combinations; this is
/// the whole difference). Every other invalid time is `ibr-119`'s.
fn ae_exp_010(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if text(&doc.inv.issue_time).is_some_and(is_offset_beyond_14_hours) {
        sink.fail(&[]);
    }
}

/// `hh:mm:ss(.s+)?` followed by an offset `±14:01` to `±14:59`, where the time is a valid
/// `xs:time` (hour 00-23, or `24:00:00` with a zero fraction; minute and second 00-59).
fn is_offset_beyond_14_hours(s: &str) -> bool {
    let Some(split) = s.len().checked_sub(6) else {
        return false;
    };
    let (Some(time), Some(offset)) = (s.get(..split), s.get(split..)) else {
        return false;
    };
    let o = offset.as_bytes();
    let offset_ok = matches!(o[0], b'+' | b'-')
        && &o[1..4] == b"14:"
        && o[4..].iter().all(u8::is_ascii_digit)
        && o[4..] != *b"00"
        && o[4] < b'6';
    offset_ok && is_xsd_time_without_offset(time)
}

fn is_xsd_time_without_offset(s: &str) -> bool {
    let b = s.as_bytes();
    if b.len() < 8 || b[2] != b':' || b[5] != b':' {
        return false;
    }
    let two = |i: usize| -> Option<u32> {
        (b[i].is_ascii_digit() && b[i + 1].is_ascii_digit())
            .then(|| u32::from(b[i] - b'0') * 10 + u32::from(b[i + 1] - b'0'))
    };
    let (Some(h), Some(m), Some(sec)) = (two(0), two(3), two(6)) else {
        return false;
    };
    let fraction = &b[8..];
    let fraction_ok = fraction.is_empty()
        || (fraction[0] == b'.'
            && fraction.len() > 1
            && fraction[1..].iter().all(u8::is_ascii_digit));
    if !fraction_ok {
        return false;
    }
    if h == 24 {
        return m == 0 && sec == 0 && fraction.iter().skip(1).all(|c| *c == b'0');
    }
    h < 24 && m < 60 && sec < 60
}

/// `YYYY-MM-DD` in ASCII digits.
fn ymd(s: &str) -> Option<(u32, u32, u32)> {
    let b = s.as_bytes();
    if b.len() != 10 || b[4] != b'-' || b[7] != b'-' {
        return None;
    }
    let num = |r: std::ops::Range<usize>| -> Option<u32> {
        b[r].iter().try_fold(0u32, |acc, c| {
            c.is_ascii_digit().then(|| acc * 10 + u32::from(c - b'0'))
        })
    };
    Some((num(0..4)?, num(5..7)?, num(8..10)?))
}

/// A real day of the proleptic Gregorian calendar (year 0 is a leap year).
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

/// `YYYY-MM-DD` (CI rule 6) that every `xsd:date` validator accepts: ASCII digits, a year from
/// 0001 to 9999, a real day. Stricter than `xsd:date`, which also allows a time zone and longer
/// years.
pub fn is_xsd_date(s: &str) -> bool {
    ymd(s).is_some_and(|(y, m, d)| y >= 1 && real_day(y, m, d))
}

/// A date `ibr-073` accepts but XML Schema 1.0 rejects: the year 0000 with a real day.
fn is_year_zero_date(s: &str) -> bool {
    ymd(s).is_some_and(|(y, m, d)| y == 0 && real_day(y, m, d))
}

/// Calls `f` for a non-empty string.
macro_rules! s {
    ($f:expr, $idx:expr, $path:expr, $v:expr) => {
        if !$v.is_empty() {
            $f($path, $idx, $v);
        }
    };
}

macro_rules! ident {
    ($f:expr, $idx:expr, $o:expr, $p:expr) => {
        if let Some(i) = $o {
            s!($f, $idx, concat!($p, ".id"), &i.id);
            s!($f, $idx, concat!($p, ".scheme_id"), &i.scheme_id);
        }
    };
}

macro_rules! address {
    ($f:expr, $idx:expr, $o:expr, $p:expr) => {
        if let Some(a) = $o {
            s!($f, $idx, concat!($p, ".line1"), &a.line1);
            s!($f, $idx, concat!($p, ".line2"), &a.line2);
            s!($f, $idx, concat!($p, ".line3"), &a.line3);
            s!($f, $idx, concat!($p, ".city"), &a.city);
            s!($f, $idx, concat!($p, ".post_code"), &a.post_code);
            s!(
                $f,
                $idx,
                concat!($p, ".country_subdivision"),
                &a.country_subdivision
            );
            s!($f, $idx, concat!($p, ".country_code"), &a.country_code);
        }
    };
}

/// A tax category without its rate (a decimal).
macro_rules! tax_category {
    ($f:expr, $idx:expr, $o:expr, $p:expr) => {
        if let Some(c) = $o {
            s!($f, $idx, concat!($p, ".code"), &c.code);
            s!($f, $idx, concat!($p, ".tax_scheme"), &c.tax_scheme);
            s!(
                $f,
                $idx,
                concat!($p, ".exemption_reason_code"),
                &c.exemption_reason_code
            );
            s!(
                $f,
                $idx,
                concat!($p, ".exemption_reason_text"),
                &c.exemption_reason_text
            );
        }
    };
}

macro_rules! party {
    ($f:expr, $o:expr, $p:literal) => {
        if let Some(p) = $o {
            let n: &[usize] = &[];
            s!($f, n, concat!($p, ".name"), &p.name);
            s!($f, n, concat!($p, ".trading_name"), &p.trading_name);
            for (i, id) in p.identifiers.iter().enumerate() {
                ident!($f, &[i], Some(id), concat!($p, ".identifiers[#]"));
            }
            if let Some(lr) = &p.legal_registration {
                s!($f, n, concat!($p, ".legal_registration.id"), &lr.id);
                s!(
                    $f,
                    n,
                    concat!($p, ".legal_registration.scheme_id"),
                    &lr.scheme_id
                );
                s!($f, n, concat!($p, ".legal_registration.type"), &lr.r#type);
                s!(
                    $f,
                    n,
                    concat!($p, ".legal_registration.authority_name"),
                    &lr.authority_name
                );
                s!(
                    $f,
                    n,
                    concat!($p, ".legal_registration.passport_issuing_country"),
                    &lr.passport_issuing_country
                );
            }
            s!(
                $f,
                n,
                concat!($p, ".tax_registration_identifier"),
                &p.tax_registration_identifier
            );
            s!(
                $f,
                n,
                concat!($p, ".additional_legal_information"),
                &p.additional_legal_information
            );
            ident!(
                $f,
                n,
                p.electronic_address.as_ref(),
                concat!($p, ".electronic_address")
            );
            address!(
                $f,
                n,
                p.postal_address.as_ref(),
                concat!($p, ".postal_address")
            );
            if let Some(c) = &p.contact {
                s!($f, n, concat!($p, ".contact.name"), &c.name);
                s!($f, n, concat!($p, ".contact.telephone"), &c.telephone);
                s!($f, n, concat!($p, ".contact.email"), &c.email);
            }
        }
    };
}

/// Visits every non-empty string field the exporter can write, as text or attribute, in proto
/// order: `f(path template, indices, raw value)`, allocating nothing. That is every string field
/// of the canonical model except the decimals of [`Doc::DECIMAL_FIELDS`] (`AE-FMT-001`'s; written
/// only when they parse), `supporting_documents[#].attachment.*` (not exported, spec 2),
/// `payment_instructions[#].direct_debit.creditor_identifier` (no binding) and the tax category
/// of a line-level allowance or charge (document level only). A field the exporter skips in a
/// particular document (an unselected qualifier, a scheme without its id) is still visited.
pub fn each_exported_string(inv: &pb::Invoice, f: &mut dyn FnMut(&'static str, &[usize], &str)) {
    let n: &[usize] = &[];
    s!(f, n, "invoice_number", &inv.invoice_number);
    s!(f, n, "issue_date", &inv.issue_date);
    s!(f, n, "seller_trn", &inv.seller_trn);
    s!(f, n, "buyer_trn", &inv.buyer_trn);
    s!(f, n, "currency", &inv.currency);
    s!(f, n, "uuid", &inv.uuid);
    s!(f, n, "issue_time", &inv.issue_time);
    s!(f, n, "invoice_type_code", &inv.invoice_type_code);
    s!(f, n, "transaction_type_code", &inv.transaction_type_code);
    s!(f, n, "tax_currency", &inv.tax_currency);
    s!(f, n, "tax_point_date", &inv.tax_point_date);
    s!(f, n, "payment_due_date", &inv.payment_due_date);
    s!(f, n, "note", &inv.note);
    s!(
        f,
        n,
        "credit_note_reason_code",
        &inv.credit_note_reason_code
    );
    if let Some(p) = &inv.process {
        s!(
            f,
            n,
            "process.business_process_type",
            &p.business_process_type
        );
        s!(
            f,
            n,
            "process.specification_identifier",
            &p.specification_identifier
        );
    }
    if let Some(r) = &inv.references {
        s!(f, n, "references.buyer_reference", &r.buyer_reference);
        s!(f, n, "references.project_reference", &r.project_reference);
        s!(f, n, "references.contract_reference", &r.contract_reference);
        s!(
            f,
            n,
            "references.purchase_order_reference",
            &r.purchase_order_reference
        );
        s!(
            f,
            n,
            "references.sales_order_reference",
            &r.sales_order_reference
        );
        s!(
            f,
            n,
            "references.receiving_advice_reference",
            &r.receiving_advice_reference
        );
        s!(
            f,
            n,
            "references.despatch_advice_reference",
            &r.despatch_advice_reference
        );
        s!(
            f,
            n,
            "references.tender_or_lot_reference",
            &r.tender_or_lot_reference
        );
        ident!(
            f,
            n,
            r.invoiced_object.as_ref(),
            "references.invoiced_object"
        );
        s!(
            f,
            n,
            "references.buyer_accounting_reference",
            &r.buyer_accounting_reference
        );
        s!(f, n, "references.customs_reference", &r.customs_reference);
    }
    for (i, p) in inv.preceding_invoices.iter().enumerate() {
        s!(f, &[i], "preceding_invoices[#].id", &p.id);
        s!(f, &[i], "preceding_invoices[#].issue_date", &p.issue_date);
    }
    party!(f, inv.seller.as_ref(), "seller");
    party!(f, inv.buyer.as_ref(), "buyer");
    s!(f, n, "principal_id", &inv.principal_id);
    s!(f, n, "beneficiary_id", &inv.beneficiary_id);
    if let Some(p) = &inv.payee {
        s!(f, n, "payee.name", &p.name);
        ident!(f, n, p.identifier.as_ref(), "payee.identifier");
        ident!(
            f,
            n,
            p.legal_registration.as_ref(),
            "payee.legal_registration"
        );
    }
    if let Some(r) = &inv.tax_representative {
        s!(f, n, "tax_representative.name", &r.name);
        s!(f, n, "tax_representative.vat_identifier", &r.vat_identifier);
        address!(
            f,
            n,
            r.postal_address.as_ref(),
            "tax_representative.postal_address"
        );
    }
    if let Some(d) = &inv.delivery {
        s!(f, n, "delivery.party_name", &d.party_name);
        s!(f, n, "delivery.incoterms", &d.incoterms);
        ident!(f, n, d.location.as_ref(), "delivery.location");
        s!(
            f,
            n,
            "delivery.actual_delivery_date",
            &d.actual_delivery_date
        );
        address!(f, n, d.address.as_ref(), "delivery.address");
    }
    if let Some(p) = &inv.invoicing_period {
        s!(f, n, "invoicing_period.start_date", &p.start_date);
        s!(f, n, "invoicing_period.end_date", &p.end_date);
    }
    s!(f, n, "billing_frequency", &inv.billing_frequency);
    for (i, pi) in inv.payment_instructions.iter().enumerate() {
        let ix: &[usize] = &[i];
        s!(f, ix, "payment_instructions[#].id", &pi.id);
        s!(f, ix, "payment_instructions[#].means_code", &pi.means_code);
        s!(f, ix, "payment_instructions[#].means_text", &pi.means_text);
        for (j, r) in pi.remittance_information.iter().enumerate() {
            ident!(
                f,
                &[i, j],
                Some(r),
                "payment_instructions[#].remittance_information[#]"
            );
        }
        if let Some(ct) = &pi.credit_transfer {
            ident!(
                f,
                ix,
                ct.account.as_ref(),
                "payment_instructions[#].credit_transfer.account"
            );
            s!(
                f,
                ix,
                "payment_instructions[#].credit_transfer.account_name",
                &ct.account_name
            );
            s!(
                f,
                ix,
                "payment_instructions[#].credit_transfer.service_provider_id",
                &ct.service_provider_id
            );
            address!(
                f,
                ix,
                ct.institution_address.as_ref(),
                "payment_instructions[#].credit_transfer.institution_address"
            );
        }
        if let Some(c) = &pi.card {
            s!(
                f,
                ix,
                "payment_instructions[#].card.primary_account_number",
                &c.primary_account_number
            );
            s!(
                f,
                ix,
                "payment_instructions[#].card.holder_name",
                &c.holder_name
            );
            s!(
                f,
                ix,
                "payment_instructions[#].card.network_id",
                &c.network_id
            );
        }
        if let Some(dd) = &pi.direct_debit {
            s!(
                f,
                ix,
                "payment_instructions[#].direct_debit.mandate_reference",
                &dd.mandate_reference
            );
            s!(
                f,
                ix,
                "payment_instructions[#].direct_debit.debited_account",
                &dd.debited_account
            );
        }
    }
    for (i, pt) in inv.payment_terms.iter().enumerate() {
        s!(
            f,
            &[i],
            "payment_terms[#].instructions_id",
            &pt.instructions_id
        );
        s!(f, &[i], "payment_terms[#].note", &pt.note);
        s!(
            f,
            &[i],
            "payment_terms[#].installment_due_date",
            &pt.installment_due_date
        );
    }
    for (i, a) in inv.allowances_charges.iter().enumerate() {
        s!(f, &[i], "allowances_charges[#].reason", &a.reason);
        s!(f, &[i], "allowances_charges[#].reason_code", &a.reason_code);
        tax_category!(
            f,
            &[i],
            a.tax_category.as_ref(),
            "allowances_charges[#].tax_category"
        );
    }
    for (i, t) in inv.tax_breakdown.iter().enumerate() {
        tax_category!(f, &[i], t.category.as_ref(), "tax_breakdown[#].category");
    }
    for (i, d) in inv.supporting_documents.iter().enumerate() {
        s!(f, &[i], "supporting_documents[#].reference", &d.reference);
        s!(
            f,
            &[i],
            "supporting_documents[#].description",
            &d.description
        );
        s!(
            f,
            &[i],
            "supporting_documents[#].external_uri",
            &d.external_uri
        );
    }
    for (i, l) in inv.lines.iter().enumerate() {
        let ix: &[usize] = &[i];
        s!(f, ix, "lines[#].id", &l.id);
        s!(f, ix, "lines[#].note", &l.note);
        ident!(
            f,
            ix,
            l.object_identifier.as_ref(),
            "lines[#].object_identifier"
        );
        s!(f, ix, "lines[#].unit_code", &l.unit_code);
        s!(f, ix, "lines[#].order_reference", &l.order_reference);
        s!(
            f,
            ix,
            "lines[#].order_line_reference",
            &l.order_line_reference
        );
        s!(
            f,
            ix,
            "lines[#].despatch_advice_reference",
            &l.despatch_advice_reference
        );
        s!(
            f,
            ix,
            "lines[#].accounting_reference",
            &l.accounting_reference
        );
        s!(f, ix, "lines[#].batch_number", &l.batch_number);
        if let Some(p) = &l.period {
            s!(f, ix, "lines[#].period.start_date", &p.start_date);
            s!(f, ix, "lines[#].period.end_date", &p.end_date);
        }
        for (j, a) in l.allowances_charges.iter().enumerate() {
            s!(
                f,
                &[i, j],
                "lines[#].allowances_charges[#].reason",
                &a.reason
            );
            s!(
                f,
                &[i, j],
                "lines[#].allowances_charges[#].reason_code",
                &a.reason_code
            );
        }
        if let Some(p) = &l.price {
            s!(
                f,
                ix,
                "lines[#].price.base_quantity_unit_code",
                &p.base_quantity_unit_code
            );
        }
        tax_category!(f, ix, l.tax.as_ref(), "lines[#].tax");
        if let Some(it) = &l.item {
            s!(f, ix, "lines[#].item.name", &it.name);
            s!(f, ix, "lines[#].item.description", &it.description);
            s!(f, ix, "lines[#].item.item_type", &it.item_type);
            s!(
                f,
                ix,
                "lines[#].item.goods_service_type",
                &it.goods_service_type
            );
            s!(f, ix, "lines[#].item.seller_item_id", &it.seller_item_id);
            s!(f, ix, "lines[#].item.buyer_item_id", &it.buyer_item_id);
            ident!(f, ix, it.standard_id.as_ref(), "lines[#].item.standard_id");
            for (j, c) in it.classifications.iter().enumerate() {
                s!(f, &[i, j], "lines[#].item.classifications[#].code", &c.code);
                s!(
                    f,
                    &[i, j],
                    "lines[#].item.classifications[#].scheme_id",
                    &c.scheme_id
                );
                s!(
                    f,
                    &[i, j],
                    "lines[#].item.classifications[#].scheme_version",
                    &c.scheme_version
                );
            }
            for (j, c) in it.service_accounting_codes.iter().enumerate() {
                s!(
                    f,
                    &[i, j],
                    "lines[#].item.service_accounting_codes[#].code",
                    &c.code
                );
                s!(
                    f,
                    &[i, j],
                    "lines[#].item.service_accounting_codes[#].scheme_id",
                    &c.scheme_id
                );
                s!(
                    f,
                    &[i, j],
                    "lines[#].item.service_accounting_codes[#].scheme_version",
                    &c.scheme_version
                );
            }
            s!(f, ix, "lines[#].item.origin_country", &it.origin_country);
            for (j, a) in it.attributes.iter().enumerate() {
                s!(f, &[i, j], "lines[#].item.attributes[#].name", &a.name);
                s!(f, &[i, j], "lines[#].item.attributes[#].value", &a.value);
            }
        }
    }
}

/// Corpus examples whose imported BTAE-05 is not the decimal string the contract requires, so
/// `AE-FMT-001` fires on `references.contract_value`. Empty: the importer strips the currency
/// prefix of the official text (`AED200000`, `AED 1000000`), so every example passes.
#[cfg(test)]
pub(crate) const KNOWN_INVALID_CONTRACT_VALUE: &[&str] = &[];

#[cfg(test)]
mod tests {
    use super::*;
    use crate::conformance::{apply_patch, examples};
    use crate::pb;
    use crate::rule::Finding;
    use serde_json::{Map, Value};

    fn run(rule_id: &str, inv: &pb::Invoice) -> Vec<Finding> {
        let rule = RULES.iter().find(|r| r.id == rule_id).unwrap();
        let template = crate::catalog::pint_ae_1_0_4()
            .get(rule_id)
            .map(|e| e.path)
            .unwrap();
        let doc = Doc::new(inv);
        let mut sink = Sink::new(template);
        (rule.check)(&doc, &mut sink);
        sink.into_findings()
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

    fn example(slug: &str) -> pb::Invoice {
        examples().into_iter().find(|(s, _)| s == slug).unwrap().1
    }

    fn patched(base: &pb::Invoice, set: Value, remove: &[&str]) -> pb::Invoice {
        let mut inv = base.clone();
        let set: Map<String, Value> = serde_json::from_value(set).unwrap();
        let remove: Vec<String> = remove.iter().map(|s| s.to_string()).collect();
        apply_patch(&mut inv, &set, &remove).unwrap();
        inv
    }

    fn paths(rule_id: &str, inv: &pb::Invoice) -> Vec<String> {
        run(rule_id, inv).into_iter().map(|f| f.path).collect()
    }

    #[test]
    fn ae_exp_001_warns_about_a_credit_note_due_date_without_payment_instructions() {
        let cn = example("standard-tax-credit-note");
        assert!(cn.payment_instructions.is_empty());
        let due = patched(
            &cn,
            serde_json::json!({"payment_due_date": "2025-03-01"}),
            &[],
        );
        assert_eq!(paths("AE-EXP-001", &due), ["payment_due_date"]);
        let f = &run("AE-EXP-001", &due)[0];
        assert!(f.args.is_empty() && f.suggested_value.is_none() && f.business_term.is_none());
        // With an instruction the date is written into PaymentMeans; an invoice writes DueDate.
        let with_pi = patched(
            &due,
            serde_json::json!({"payment_instructions[0].means_code": "10"}),
            &[],
        );
        assert_eq!(paths("AE-EXP-001", &with_pi), Vec::<String>::new());
        let inv = patched(&due, serde_json::json!({"invoice_type_code": "380"}), &[]);
        assert_eq!(paths("AE-EXP-001", &inv), Vec::<String>::new());
        let blank = patched(&cn, serde_json::json!({"payment_due_date": " "}), &[]);
        assert_eq!(paths("AE-EXP-001", &blank), Vec::<String>::new());
        let cn81 = patched(&due, serde_json::json!({"invoice_type_code": "81"}), &[]);
        assert_eq!(paths("AE-EXP-001", &cn81), ["payment_due_date"]);
    }

    #[test]
    fn ae_exp_002_requires_ibt_110_with_a_breakdown_and_suggests_the_rounded_sum() {
        let base = example("standard-tax-invoice");
        let inv = patched(&base, serde_json::json!({"vat_amount": ""}), &[]);
        let f = run("AE-EXP-002", &inv);
        assert_eq!(f.len(), 1);
        assert_eq!(f[0].path, "vat_amount");
        // One breakdown entry of 532.1645: ibr-co-14 wants round(sum * 100) div 100.
        assert_eq!(f[0].suggested_value.as_deref(), Some("532.16"));
        let two = patched(
            &inv,
            serde_json::json!({"tax_breakdown[1].tax_amount": "0.005", "tax_breakdown[1].taxable_amount": "1"}),
            &[],
        );
        assert_eq!(
            run("AE-EXP-002", &two)[0].suggested_value.as_deref(),
            Some("532.17")
        );
        // Exact like ibr-co-14 (decimal::Cents): 1e25 + 0.0049999999999999999999999 rounds to
        // .00, where rust_decimal's sum would round to ...0.005 first and give .01.
        let wide = patched(
            &inv,
            serde_json::json!({
                "tax_breakdown[0].tax_amount": "10000000000000000000000000",
                "tax_breakdown[1].tax_amount": "0.0049999999999999999999999",
                "tax_breakdown[1].taxable_amount": "1",
            }),
            &[],
        );
        assert_eq!(
            run("AE-EXP-002", &wide)[0].suggested_value.as_deref(),
            Some("10000000000000000000000000.00")
        );
        // No suggestion when a breakdown amount is absent or invalid.
        for bad in ["", "1,0"] {
            let inv = patched(
                &two,
                serde_json::json!({"tax_breakdown[1].tax_amount": bad}),
                &[],
            );
            let f = run("AE-EXP-002", &inv);
            assert_eq!(f.len(), 1, "{bad:?}");
            assert_eq!(f[0].suggested_value, None, "{bad:?}");
        }
        // No breakdown, or IBT-110 present (an invalid one is AE-FMT-001's): nothing.
        let none = patched(&inv, serde_json::json!({}), &["tax_breakdown"]);
        assert_eq!(paths("AE-EXP-002", &none), Vec::<String>::new());
        assert_eq!(paths("AE-EXP-002", &base), Vec::<String>::new());
        let invalid = patched(&base, serde_json::json!({"vat_amount": "x"}), &[]);
        assert_eq!(paths("AE-EXP-002", &invalid), Vec::<String>::new());
    }

    #[test]
    fn ae_exp_003_requires_ibt_006_with_an_exchange_rate() {
        let base = example("standard-tax-invoice");
        let inv = patched(&base, serde_json::json!({"exchange_rate": "3.6725"}), &[]);
        assert_eq!(paths("AE-EXP-003", &inv), ["tax_currency"]);
        let ok = patched(&inv, serde_json::json!({"tax_currency": "AED"}), &[]);
        assert_eq!(paths("AE-EXP-003", &ok), Vec::<String>::new());
        // An invalid rate is not written (AE-FMT-001), so no target currency is needed.
        let invalid = patched(&base, serde_json::json!({"exchange_rate": "3,67"}), &[]);
        assert_eq!(paths("AE-EXP-003", &invalid), Vec::<String>::new());
        let exports = patched(
            &example("exports"),
            serde_json::json!({"tax_currency": " "}),
            &[],
        );
        assert_eq!(paths("AE-EXP-003", &exports), ["tax_currency"]);
    }

    #[test]
    fn ae_exp_004_requires_the_pan_of_a_written_card() {
        let base = example("standard-invoice-extensive");
        let inv = patched(
            &base,
            serde_json::json!({"payment_instructions[0].card.primary_account_number": ""}),
            &[],
        );
        assert_eq!(
            paths("AE-EXP-004", &inv),
            ["payment_instructions[0].card.primary_account_number"]
        );
        let second = patched(
            &base,
            serde_json::json!({"payment_instructions[1].means_code": "54", "payment_instructions[1].card.network_id": "VISA"}),
            &[],
        );
        assert_eq!(
            paths("AE-EXP-004", &second),
            ["payment_instructions[1].card.primary_account_number"]
        );
        // A card with no content is not written at all.
        let empty = patched(
            &base,
            serde_json::json!({
                "payment_instructions[0].card.primary_account_number": "",
                "payment_instructions[0].card.holder_name": "",
                "payment_instructions[0].card.network_id": "",
            }),
            &[],
        );
        assert_eq!(paths("AE-EXP-004", &empty), Vec::<String>::new());
        assert_eq!(paths("AE-EXP-004", &base), Vec::<String>::new());
    }

    #[test]
    fn ae_exp_005_reports_each_field_with_a_forbidden_character() {
        let base = example("standard-tax-invoice");
        let inv = patched(
            &base,
            serde_json::json!({
                "note": "Tax\u{1}invoice",
                "lines[0].item.name": "x\u{FFFE}",
                "seller.electronic_address.scheme_id": "02\u{B}35",
                "buyer.contact.email": "\u{C}a@b.ae",
            }),
            &[],
        );
        // U+000C at the start is trimmed (White_Space), so it is never written.
        assert_eq!(
            paths("AE-EXP-005", &inv),
            [
                "note",
                "seller.electronic_address.scheme_id",
                "lines[0].item.name"
            ]
        );
        assert!(crate::export::to_xml(&Doc::new(&inv)).is_err());
        // Never exported: attachments, IBT-090; decimals are AE-FMT-001's.
        let silent = patched(
            &base,
            serde_json::json!({
                "supporting_documents[0].attachment.filename": "a\u{1}",
                "payment_instructions[0].direct_debit.creditor_identifier": "\u{2}x",
                "total_amount": "1\u{1}",
            }),
            &[],
        );
        assert_eq!(paths("AE-EXP-005", &silent), Vec::<String>::new());
        assert!(crate::export::to_xml(&Doc::new(&silent)).is_ok());
    }

    /// Every string field of the model is visited by AE-EXP-005 with its own value, except the
    /// decimals (AE-FMT-001) and the fields the exporter never writes; and every field the
    /// walker visits really fails the export when it holds a forbidden character.
    #[test]
    fn ae_exp_005_covers_every_exported_string_field() {
        use crate::export::testing::{every_field, maximal};
        for code in ["380", "381"] {
            let inv = maximal(code);
            let mut seen = Vec::new();
            each_exported_string(&inv, &mut |template, idx, value| {
                seen.push((fill(template, idx), value.to_string()));
            });
            let mut want: Vec<(String, String)> = every_field(code)
                .into_iter()
                .filter_map(|(p, v)| v.as_str().map(|v| (p, v.to_string())))
                .filter(|(p, _)| !not_exported_or_decimal(p))
                .filter(|(p, _)| code == "381" || p != "credit_note_reason_code")
                .collect();
            want.sort();
            seen.sort();
            assert_eq!(seen, want, "{code}");
        }
    }

    fn not_exported_or_decimal(path: &str) -> bool {
        let template: String = {
            let mut out = String::new();
            let mut in_idx = false;
            for c in path.chars() {
                match c {
                    '[' => {
                        in_idx = true;
                        out.push_str("[#");
                    }
                    ']' => {
                        in_idx = false;
                        out.push(']');
                    }
                    _ if in_idx => {}
                    c => out.push(c),
                }
            }
            out
        };
        Doc::DECIMAL_FIELDS.iter().any(|f| f.path == template)
            || template.contains(".attachment.")
            || template.ends_with("creditor_identifier")
            || template.starts_with("lines[#].allowances_charges[#].tax_category.")
    }

    #[test]
    fn ae_exp_005_and_the_exporter_agree_on_every_exported_field() {
        use crate::export::testing::maximal;
        for code in ["380", "381"] {
            let base = maximal(code);
            let mut fields = Vec::new();
            each_exported_string(&base, &mut |t, idx, _| fields.push(fill(t, idx)));
            for path in fields {
                let inv = patched(&base, serde_json::json!({ path.clone(): "a\u{1}b" }), &[]);
                let reported = paths("AE-EXP-005", &inv) == [path.clone()];
                let exportable = crate::export::to_xml(&Doc::new(&inv)).is_ok();
                assert!(reported, "{code} {path}: not reported");
                // Reported fields that the exporter skips (an unselected qualifier, a scheme
                // without its id) are allowed; a written one must fail the export.
                if exportable {
                    assert!(
                        SKIPPED_WHEN_SET_ALONE.iter().any(|s| path.ends_with(s)),
                        "{code} {path}: exported although it holds a forbidden character"
                    );
                }
            }
        }
    }

    /// Fields AE-EXP-005 checks although the exporter does not write them in a maximal document
    /// (conservative: the field is reported, the export is blocked): the `legal_registration`
    /// qualifier a `TL` type does not select.
    const SKIPPED_WHEN_SET_ALONE: &[&str] = &["legal_registration.passport_issuing_country"];

    #[test]
    fn ae_exp_006_requires_ibt_012_with_a_contract_value() {
        let base = example("standard-invoice-extensive");
        let inv = patched(
            &base,
            serde_json::json!({"references.contract_reference": ""}),
            &[],
        );
        assert_eq!(paths("AE-EXP-006", &inv), ["references.contract_reference"]);
        assert_eq!(paths("AE-EXP-006", &base), Vec::<String>::new());
        let invalid = patched(
            &inv,
            serde_json::json!({"references.contract_value": "AED 5"}),
            &[],
        );
        assert_eq!(paths("AE-EXP-006", &invalid), Vec::<String>::new());
    }

    #[test]
    fn ae_exp_007_requires_some_document_total() {
        let base = example("standard-tax-invoice");
        let cleared = patched(&base, serde_json::json!({"total_amount": ""}), &["totals"]);
        assert_eq!(paths("AE-EXP-007", &cleared), ["totals.payable_amount"]);
        // IBT-111, BTAE-20 and IBT-200 are not written in LegalMonetaryTotal.
        let other = patched(
            &cleared,
            serde_json::json!({"totals.tax_amount_accounting_currency": "1", "totals.total_with_tax_aed": "2", "totals.tax_inclusive_pricing": true}),
            &[],
        );
        assert_eq!(paths("AE-EXP-007", &other), ["totals.payable_amount"]);
        for field in [
            "total_amount",
            "totals.line_extension_amount",
            "totals.allowance_total_amount",
            "totals.charge_total_amount",
            "totals.tax_exclusive_amount",
            "totals.paid_amount",
            "totals.rounding_amount",
            "totals.payable_amount",
        ] {
            let one = patched(&cleared, serde_json::json!({ field: "0" }), &[]);
            assert_eq!(paths("AE-EXP-007", &one), Vec::<String>::new(), "{field}");
            let bad = patched(&cleared, serde_json::json!({ field: "x" }), &[]);
            assert_eq!(
                paths("AE-EXP-007", &bad),
                ["totals.payable_amount"],
                "{field}"
            );
        }
        assert_eq!(paths("AE-EXP-007", &base), Vec::<String>::new());
    }

    #[test]
    fn xsd_dates_are_yyyy_mm_dd_with_a_real_day_and_a_year_from_1() {
        for ok in [
            "2025-01-31",
            "2024-02-29",
            "2000-02-29",
            "0001-01-01",
            "9999-12-31",
        ] {
            assert!(is_xsd_date(ok), "{ok}");
        }
        for bad in [
            "2025-02-29",
            "1900-02-29",
            "2025-13-01",
            "2025-00-10",
            "2025-04-31",
            "2025-01-00",
            "0000-01-01",
            "2025-1-01",
            "25-01-01",
            "2025/01/01",
            "2025-01-01Z",
            " 2025-01-01",
            "٢٠٢٥-٠١-٠١",
            "",
            "10000-01-01",
        ] {
            assert!(!is_xsd_date(bad), "{bad}");
        }
    }

    #[test]
    fn ae_exp_008_checks_the_dates_outside_ibr_073() {
        let inv = patched(
            &example("standard-tax-invoice"),
            serde_json::json!({
                "payment_terms[0].installment_due_date": "2025-13-01",
                "payment_terms[1].installment_due_date": "2025-02-28",
                "payment_terms[2].installment_due_date": "0000-02-28",
                "payment_due_date": "2025-02-30",
            }),
            &[],
        );
        let got: Vec<_> = run("AE-EXP-008", &inv)
            .into_iter()
            .map(|f| (f.path, f.business_term, f.args))
            .collect();
        let want =
            |p: &str, t: &'static str| (p.to_string(), Some(t), vec![("term", t.to_string())]);
        // An invoice's IBT-009 is cbc:DueDate, which ibr-073 checks.
        assert_eq!(
            got,
            [
                want("payment_terms[0].installment_due_date", "IBT-177"),
                want("payment_terms[2].installment_due_date", "IBT-177"),
            ]
        );
        let cn = patched(
            &example("standard-tax-credit-note"),
            serde_json::json!({"payment_due_date": "2025-02-30", "payment_instructions[0].means_code": "10"}),
            &[],
        );
        let got: Vec<_> = run("AE-EXP-008", &cn)
            .into_iter()
            .map(|f| (f.path, f.business_term))
            .collect();
        assert_eq!(got, [("payment_due_date".to_string(), Some("IBT-009"))]);
        // Without payment instructions a credit note's IBT-009 is not written (AE-EXP-001).
        let lost = patched(
            &example("standard-tax-credit-note"),
            serde_json::json!({"payment_due_date": "x"}),
            &[],
        );
        assert_eq!(paths("AE-EXP-008", &lost), Vec::<String>::new());
    }

    #[test]
    fn ae_exp_009_reports_year_0000_in_the_ibr_073_dates() {
        let inv = patched(
            &example("standard-invoice-extensive"),
            serde_json::json!({
                "issue_date": "0000-01-30",
                "tax_point_date": "0000-02-29",
                "payment_due_date": "0000-02-30",
                "preceding_invoices[0].issue_date": "0000-01-01",
                "delivery.actual_delivery_date": "0000-12-31",
                "invoicing_period.start_date": "0000-01-01",
                "invoicing_period.end_date": "2025-01-30",
                "lines[1].period.start_date": "0000-01-01",
                "lines[1].period.end_date": "0000-01-02",
                "payment_terms[0].installment_due_date": "0000-01-01",
            }),
            &[],
        );
        let got: Vec<_> = run("AE-EXP-009", &inv)
            .into_iter()
            .map(|f| (f.path, f.business_term.unwrap()))
            .collect();
        // 0000-02-30 is not a date at all: ibr-073 reports it. IBT-177 is AE-EXP-008's.
        let want: Vec<(String, &str)> = [
            ("issue_date", "IBT-002"),
            ("tax_point_date", "IBT-007"),
            ("preceding_invoices[0].issue_date", "IBT-026"),
            ("delivery.actual_delivery_date", "IBT-072"),
            ("invoicing_period.start_date", "IBT-073"),
            ("lines[1].period.start_date", "IBT-134"),
            ("lines[1].period.end_date", "IBT-135"),
        ]
        .into_iter()
        .map(|(p, t)| (p.to_string(), t))
        .collect();
        assert_eq!(got, want);
        let due = patched(
            &example("standard-tax-invoice"),
            serde_json::json!({"payment_due_date": "0000-03-01"}),
            &[],
        );
        assert_eq!(paths("AE-EXP-009", &due), ["payment_due_date"]);
        // A credit note's IBT-009 is not in ibr-073's context (AE-EXP-008 covers it).
        let cn = patched(
            &example("standard-tax-credit-note"),
            serde_json::json!({"payment_due_date": "0000-03-01", "payment_instructions[0].means_code": "10"}),
            &[],
        );
        assert_eq!(paths("AE-EXP-009", &cn), Vec::<String>::new());
        assert_eq!(paths("AE-EXP-008", &cn), ["payment_due_date"]);
    }

    /// The exact difference between Saxon's `castable as xs:time` (`ibr-119`) and the XML Schema
    /// 1.0 `xs:time` of lxml, measured on 5,040 combinations of hour, minute, second, fraction and
    /// offset: an otherwise valid time whose offset is beyond ±14:00 (±14:01 to ±14:59).
    #[test]
    fn ae_exp_010_reports_an_issue_time_offset_beyond_14_hours() {
        let base = example("standard-tax-invoice");
        for bad in [
            "07:54:00+14:01",
            "07:54:00-14:59",
            "24:00:00+14:30",
            "00:00:00.5-14:01",
            " 23:59:59.123+14:10 ",
        ] {
            let inv = patched(&base, serde_json::json!({ "issue_time": bad }), &[]);
            let f = run("AE-EXP-010", &inv);
            assert_eq!(f.len(), 1, "{bad:?}");
            assert_eq!(f[0].path, "issue_time");
            assert!(f[0].args.is_empty() && f[0].suggested_value.is_none());
        }
        // Valid for both, or invalid for both (ibr-119 reports those): nothing.
        for ok in [
            "07:54:00+04:00",
            "07:54:00+14:00",
            "07:54:00-14:00",
            "07:54:00Z",
            "07:54:00",
            "24:00:00",
            "07:54:00+15:00",
            "07:54:00+14:60",
            "25:00:00+14:30",
            "24:00:01+14:30",
            "07:60:00+14:30",
            "07:54:60+14:30",
            "07:54:00.+14:30",
            "7:54:00+14:30",
            "07:54+14:30",
            "07:54:00+14",
            "٠٧:54:00+14:30",
            "",
        ] {
            let inv = patched(&base, serde_json::json!({ "issue_time": ok }), &[]);
            assert_eq!(paths("AE-EXP-010", &inv), Vec::<String>::new(), "{ok:?}");
        }
    }

    #[test]
    fn the_official_examples_pass_every_platform_rule() {
        assert_eq!(KNOWN_INVALID_CONTRACT_VALUE, &[] as &[&str]);
        for (slug, inv) in examples() {
            for rule in RULES {
                let paths: Vec<String> = run(rule.id, &inv).into_iter().map(|f| f.path).collect();
                assert_eq!(paths, Vec::<String>::new(), "{} on {slug}", rule.id);
            }
        }
    }

    /// The two examples that write BTAE-05 with a currency prefix import as bare decimals.
    #[test]
    fn the_examples_contract_values_are_decimal_strings() {
        let ex = examples();
        for (slug, want) in [
            ("continuous-supplies", "1000000"),
            ("standard-invoice-extensive", "200000"),
        ] {
            let (_, inv) = ex.iter().find(|(s, _)| s == slug).unwrap();
            let got = &inv.references.as_ref().unwrap().contract_value;
            assert_eq!(got, want, "{slug}");
        }
    }

    #[test]
    fn ae_fmt_001_reports_each_invalid_field_with_its_term() {
        let inv = invoice(&[
            ("total_amount", "1,050.00"),
            ("vat_amount", " 50.00 "),
            ("exchange_rate", "3.6725"),
            ("tax_breakdown[0].tax_amount", "٥٠"),
            ("tax_breakdown[0].taxable_amount", "1000"),
            ("lines[0].net_amount", "1e3"),
            ("lines[0].price.net_price", " "),
            ("lines[0].allowances_charges[0].amount", "AED 5"),
        ]);
        let mut inv = inv;
        inv.lines[0].allowances_charges[0].is_charge = true;
        let got: Vec<_> = run("AE-FMT-001", &inv)
            .into_iter()
            .map(|f| (f.path, f.business_term, f.args))
            .collect();
        let want = |path: &str, term: &'static str| {
            (
                path.to_string(),
                Some(term),
                vec![("term", term.to_string())],
            )
        };
        assert_eq!(
            got,
            [
                want("total_amount", "IBT-112"),
                want("tax_breakdown[0].tax_amount", "IBT-117"),
                want("lines[0].net_amount", "IBT-131"),
                want("lines[0].allowances_charges[0].amount", "IBT-141"),
            ]
        );
    }

    #[test]
    fn ae_fmt_001_covers_every_decimal_field() {
        let mut set = Vec::new();
        for f in Doc::DECIMAL_FIELDS {
            let combos: Vec<Vec<usize>> = match f.path.matches('#').count() {
                0 => vec![vec![]],
                1 => vec![vec![0], vec![1]],
                _ => vec![vec![0, 0], vec![0, 1], vec![1, 0], vec![1, 1]],
            };
            for (n, idx) in combos.iter().enumerate() {
                let bad = if n % 2 == 0 { "1.0.0" } else { "+-1" };
                set.push((fill(f.path, idx), bad.to_string()));
            }
        }
        set.sort();
        let pairs: Vec<(&str, &str)> = set.iter().map(|(k, v)| (k.as_str(), v.as_str())).collect();
        let inv = invoice(&pairs);
        let mut got: Vec<String> = run("AE-FMT-001", &inv)
            .into_iter()
            .map(|f| f.path)
            .collect();
        got.sort();
        let want: Vec<String> = set.into_iter().map(|(k, _)| k).collect();
        assert_eq!(got, want);
    }

    #[test]
    fn ae_fmt_001_accepts_valid_and_absent_values() {
        let inv = invoice(&[
            ("total_amount", " 1050.00 "),
            ("vat_amount", "-0.5"),
            ("exchange_rate", "+3.672500"),
            ("totals.paid_amount", "\t"),
        ]);
        assert_eq!(run("AE-FMT-001", &inv), []);
        assert_eq!(run("AE-FMT-001", &pb::Invoice::default()), []);
    }

    #[test]
    fn ae_scope_001_rejects_self_billing() {
        for spec in [
            "urn:peppol:pint:selfbilling-1@ae-1",
            " urn:peppol:pint:selfbilling-1@ae-1 ",
            "urn:peppol:pint:selfbilling-1@ae-1#conformant#urn:x",
        ] {
            let inv = invoice(&[("process.specification_identifier", spec)]);
            let got = run("AE-SCOPE-001", &inv);
            assert_eq!(got.len(), 1, "{spec:?}");
            assert_eq!(got[0].path, "process.specification_identifier");
            assert_eq!(got[0].business_term, None);
            assert!(got[0].args.is_empty() && got[0].suggested_value.is_none());
        }
        for spec in [
            "",
            "urn:peppol:pint:billing-1@ae-1",
            "URN:PEPPOL:PINT:SELFBILLING-1@AE-1",
            "x urn:peppol:pint:selfbilling-1@ae-1",
            "urn:peppol:pint:selfbilling-1",
        ] {
            let inv = invoice(&[("process.specification_identifier", spec)]);
            assert_eq!(run("AE-SCOPE-001", &inv), [], "{spec:?}");
        }
        assert_eq!(run("AE-SCOPE-001", &pb::Invoice::default()), []);
    }

    /// The failing fixtures of `mutations/platform.jsonl` (spec 5.2.8). `expect` is the complete
    /// multiset the full RuleSet must report; the generic harness (`tests/family_fixtures.rs`)
    /// checks its registered part exactly, and this test pins the platform part, which the
    /// official families cannot change.
    #[test]
    fn platform_mutation_fixtures_fail_as_expected() {
        let path = crate::conformance::corpus::ruleset_dir().join("mutations/platform.jsonl");
        let text = std::fs::read_to_string(&path).unwrap();
        let bases = examples();
        let mut fired = std::collections::BTreeSet::new();
        let mut count = 0;
        for line in text.lines() {
            let m: Value = serde_json::from_str(line).unwrap();
            let id = m["id"].as_str().unwrap();
            let base = m["base"].as_str().unwrap();
            let set = m["set"].as_object().unwrap();
            let remove: Vec<String> = m["remove"]
                .as_array()
                .unwrap()
                .iter()
                .map(|v| v.as_str().unwrap().to_string())
                .collect();
            let mut expect: Vec<&str> = m["expect"]
                .as_array()
                .unwrap()
                .iter()
                .map(|v| v.as_str().unwrap())
                .filter(|r| r.starts_with("AE-"))
                .collect();
            assert!(
                m["note"].as_str().is_some_and(|n| !n.is_empty()),
                "{id}: note"
            );
            let (_, inv) = bases
                .iter()
                .find(|(slug, _)| slug == base)
                .unwrap_or_else(|| panic!("{id}: unknown base {base}"));
            let mut inv = inv.clone();
            apply_patch(&mut inv, set, &remove).unwrap();
            let run = crate::ruleset::default_ruleset().validate(&inv);
            let mut got: Vec<&str> = run
                .issues
                .iter()
                .map(|i| i.rule_id.as_str())
                .filter(|r| r.starts_with("AE-"))
                .collect();
            got.sort_unstable();
            expect.sort_unstable();
            assert_eq!(got, expect, "{id}");
            fired.extend(got.iter().map(|r| r.to_string()));
            count += 1;
        }
        assert!(count >= 2, "platform.jsonl has {count} fixtures");
        for rule in RULES {
            assert!(
                fired.contains(rule.id),
                "{} has no failing fixture",
                rule.id
            );
        }
    }
}
