//! PINT-AE 1.0.4 UBL 2.1 exporter (spec 5.3.1): one ordered emitter per document kind
//! ([`invoice`], [`credit_note`]), the builders they share below, and a deterministic writer
//! ([`writer`]).
//!
//! Bindings, element order, placeholders, omissions and defaults are exactly the contract's (CI
//! mapping table and "Invoice vs CreditNote serialisation", v0.2.1). The exporter reads only the
//! [`Doc`], so the official schematron evaluated on its output sees what the Rust rules see
//! (CI rule 13). What a rule author must know about the output:
//!
//! * **Text** is the `doc::text` trim of the field; an absent (empty or blank) field is not
//!   written. **Decimals** are written verbatim (`Dec::raw`) only when they parse
//!   (`Dec::exists`); an invalid one (`AE-FMT-001`) is not written, exactly as every other rule
//!   treats it as absent.
//! * **No empty element** is ever written (CI rule 4, `ibr-079`): an aggregate is written only
//!   when it has a written child. Constant children (`ChargeIndicator`, `TaxScheme/ID`,
//!   `DocumentTypeCode`, the `NA` placeholders) are added only to an aggregate that has content
//!   of its own, except the document- and line-level `AllowanceCharge`, whose `ChargeIndicator`
//!   comes from the `is_charge` field and is therefore always written: every allowance or
//!   charge entry of the model is an element. A line, payment instruction, payment terms entry,
//!   breakdown entry, preceding invoice or supporting document whose fields are all empty is
//!   not written.
//! * **Defaults** (CI "Defaults"): IBT-023, IBT-024 and the tax scheme `VAT`
//!   (`Doc::business_process_type`, `Doc::specification_identifier`, `doc::tax_scheme`).
//! * **Placeholders** `NA`: `OrderReference/ID` (IBT-014 without IBT-013),
//!   `OrderLineReference/LineID` (IBT-183 without IBT-132), `DespatchLineReference/LineID`
//!   (always), `CardAccount/NetworkID` (no `network_id`).
//! * **Omissions** (CI "XSD-mandatory children"): an `Identifier` with an empty `id` (its scheme
//!   is dropped), a `Classification` with an empty `code`, an `ItemAttribute` with an empty
//!   `name`, a `PartyName` with an empty name; `ItemPriceExtension` without BTAE-10 (BTAE-08 is
//!   dropped with it); `TaxExchangeRate` without BTAE-04; the document-currency `TaxTotal`
//!   without IBT-110 (IBT-200 and the VAT breakdown are dropped with it, `AE-EXP-002`); the
//!   accounting-currency `TaxTotal` without IBT-111; a tax category whose fields are all empty
//!   (its default scheme alone is not content).
//! * **currencyID**: IBT-005 on every amount, `AED` on the `ItemPriceExtension` amounts (BTAE-10,
//!   BTAE-08), IBT-006 on the accounting-currency `TaxTotal/TaxAmount` (IBT-111). The attribute
//!   is XSD-required, so it is written even when the currency is empty.
//! * **Not exported**: `supporting_documents[].attachment` (spec 2, non-goals);
//!   `payment_instructions[].direct_debit.creditor_identifier` (IBT-090 has no binding in the CI
//!   mapping table); IBT-009 of a credit note without payment instructions (`AE-EXP-001`); of the
//!   two `legal_registration` qualifiers that share `@schemeAgencyName`, only the one the type
//!   selects (`passport_issuing_country` for `PAS`, `authority_name` otherwise).
//! * **Layout choices** where UBL allows several: BTAE-13, BTAE-09 and the first written
//!   classification share one `CommodityClassification` (as in every official example); each
//!   further classification gets its own. The seller's VAT `PartyTaxScheme` precedes the TIN one
//!   (`Seller-TIN-identifier.xml`). Credit-note IBT-009 goes into the `PaymentMeans` of
//!   `payment_instructions[0]`.
//!
//! Every XSD-required child of every element written here is either always written, a
//! placeholder, omitted with its aggregate, or required by an official or platform rule
//! (spec 5.2.7, `AE-EXP-00N`).

pub mod credit_note;
pub mod invoice;
pub mod writer;

use std::borrow::Cow;

use crate::doc::{self, AllowanceChargeDec, Dec, Doc, DocKind, LineDec, text};
use crate::pb;
use crate::ruleset::RuleSet;
use writer::El;

/// `ExportResponse.format`.
pub const FORMAT: &str = "pint-ae-billing-1.0.4/ubl-2.1";

pub const NS_INVOICE: &str = "urn:oasis:names:specification:ubl:schema:xsd:Invoice-2";
pub const NS_CREDIT_NOTE: &str = "urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2";
pub const NS_CAC: &str = "urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2";
pub const NS_CBC: &str = "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2";

/// The Peppol placeholder for an XSD-required value the business term does not have.
pub const PLACEHOLDER: &str = "NA";
/// Currency of the `ItemPriceExtension` amounts (BTAE-10, BTAE-08) and of BTAE-20.
pub const AED: &str = "AED";
/// `AdditionalDocumentReference/DocumentTypeCode` of the invoiced object (IBT-018) and of the
/// line object identifier (IBT-128).
pub const DOC_TYPE_INVOICED_OBJECT: &str = "130";
/// `AdditionalDocumentReference/DocumentTypeCode` of a credit note's project reference (IBT-011).
pub const DOC_TYPE_PROJECT: &str = "50";
/// `AdditionalDocumentReference/DocumentTypeCode` of BTAE-20.
pub const DOC_TYPE_AED_TOTAL: &str = "aedtotal-incl-vat";

/// The exporter cannot write this document.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ExportError {
    /// A character XML 1.0 forbids; `AE-EXP-005` reports it before export.
    ForbiddenChar { element: &'static str, ch: char },
}

impl std::fmt::Display for ExportError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ExportError::ForbiddenChar { element, ch } => write!(
                f,
                "character U+{:04X} in <{element}> cannot be written in XML 1.0 (AE-EXP-005)",
                u32::from(*ch)
            ),
        }
    }
}

impl std::error::Error for ExportError {}

/// `ExportResponse.document_kind`.
pub fn document_kind(kind: DocKind) -> &'static str {
    match kind {
        DocKind::Invoice => "invoice",
        DocKind::CreditNote => "credit_note",
    }
}

/// Writes UBL 2.1 for the document kind of `doc` without checking validity (the conformance
/// CLI exports invalid fixtures on purpose). Only [`export`] is reachable from gRPC.
pub fn to_xml(doc: &Doc<'_>) -> Result<Vec<u8>, ExportError> {
    let root = match doc.kind {
        DocKind::Invoice => invoice::build(doc),
        DocKind::CreditNote => credit_note::build(doc),
    };
    writer::serialize(&root)
}

/// The result of [`export`]: the validation run the decision is based on, and the XML when the
/// run has no error issue.
#[derive(Debug, Clone, PartialEq)]
pub struct ExportOutcome {
    pub run: pb::ValidationRun,
    pub xml: Option<Vec<u8>>,
}

/// Validates `inv` with `rs` and exports it only when the run has no `error` issue
/// (spec 5.3.1). `AE-EXP-005` covers the only way [`to_xml`] can fail, so a run without errors
/// always yields XML; should that ever not hold, no XML is returned rather than invalid bytes.
pub fn export(inv: &pb::Invoice, rs: &RuleSet) -> ExportOutcome {
    let run = rs.validate(inv);
    let has_error = run
        .issues
        .iter()
        .any(|i| i.severity == pb::Severity::Error as i32);
    let xml = if has_error {
        None
    } else {
        match to_xml(&Doc::new(inv)) {
            Ok(xml) => Some(xml),
            Err(e) => {
                tracing::error!(error = %e, "a run without errors could not be exported");
                None
            }
        }
    };
    ExportOutcome { run, xml }
}

// ---------------------------------------------------------------------------------------------
// Shared builders. `'a` is the lifetime of the canonical invoice: every written string borrows
// from it, except the constants and BTAE-20's `AED ` prefix.

/// Builder state for one document.
pub(crate) struct B<'d, 'a> {
    pub doc: &'d Doc<'a>,
    pub inv: &'a pb::Invoice,
    /// IBT-005, the `currencyID` of every amount except the AED ones.
    pub cur: &'a str,
}

/// `<name>text</name>` when the trimmed text is present.
pub(crate) fn t<'a>(name: &'static str, s: &'a str) -> Option<El<'a>> {
    text(s).map(|v| El::leaf(name, v))
}

/// A decimal without a unit (quantity, percent, rate, multiplier) when it parses.
pub(crate) fn num<'a>(name: &'static str, d: &Dec<'a>) -> Option<El<'a>> {
    d.exists().then(|| El::leaf(name, d.raw))
}

/// An amount with its `currencyID` when it parses.
pub(crate) fn amount_in<'a>(name: &'static str, d: &Dec<'a>, cur: &'a str) -> Option<El<'a>> {
    num(name, d).map(|el| el.attr("currencyID", cur))
}

/// `<name schemeID="...">id</name>` when the identifier's `id` is present.
pub(crate) fn ident<'a>(name: &'static str, i: Option<&'a pb::Identifier>) -> Option<El<'a>> {
    let i = i?;
    t(name, &i.id).map(|el| el.attr_opt("schemeID", text(&i.scheme_id)))
}

/// `<agg><cbc:ID schemeID="...">id</cbc:ID></agg>`.
pub(crate) fn id_agg<'a>(agg: &'static str, i: Option<&'a pb::Identifier>) -> Option<El<'a>> {
    ident("cbc:ID", i).map(|id| El::new(agg).with(id))
}

/// `<agg><cbc:ID>s</cbc:ID></agg>`.
pub(crate) fn id_text_agg<'a>(agg: &'static str, s: &'a str) -> Option<El<'a>> {
    t("cbc:ID", s).map(|id| El::new(agg).with(id))
}

/// `<agg><cbc:Name>s</cbc:Name></agg>` (`PartyName`).
pub(crate) fn party_name<'a>(s: &'a str) -> Option<El<'a>> {
    t("cbc:Name", s).map(|n| El::new("cac:PartyName").with(n))
}

/// An `AddressType` aggregate (IBG-05, -08, -12, -15, -34).
pub(crate) fn address<'a>(name: &'static str, a: Option<&'a pb::PostalAddress>) -> Option<El<'a>> {
    let a = a?;
    El::new(name)
        .with(t("cbc:StreetName", &a.line1))
        .with(t("cbc:AdditionalStreetName", &a.line2))
        .with(t("cbc:CityName", &a.city))
        .with(t("cbc:PostalZone", &a.post_code))
        .with(t("cbc:CountrySubentity", &a.country_subdivision))
        .with(t("cbc:Line", &a.line3).map(|l| El::new("cac:AddressLine").with(l)))
        .with(t("cbc:IdentificationCode", &a.country_code).map(|c| El::new("cac:Country").with(c)))
        .non_empty()
}

/// `<cac:PartyTaxScheme><cbc:CompanyID>id</cbc:CompanyID><cac:TaxScheme><cbc:ID>scheme`.
pub(crate) fn party_tax_scheme<'a>(company_id: &'a str, scheme: &'static str) -> Option<El<'a>> {
    t("cbc:CompanyID", company_id).map(|c| {
        El::new("cac:PartyTaxScheme")
            .with(c)
            .with(El::new("cac:TaxScheme").with(El::leaf("cbc:ID", scheme)))
    })
}

/// A `TaxCategoryType` (`cac:TaxCategory` or `cac:ClassifiedTaxCategory`), written when any of
/// its fields is present; the scheme defaults to `VAT`.
pub(crate) fn tax_category<'a>(
    name: &'static str,
    c: Option<&'a pb::TaxCategory>,
    rate: &Dec<'a>,
) -> Option<El<'a>> {
    let c = c?;
    let el = El::new(name)
        .with(t("cbc:ID", &c.code))
        .with(num("cbc:Percent", rate))
        .with(t("cbc:TaxExemptionReasonCode", &c.exemption_reason_code))
        .with(t("cbc:TaxExemptionReason", &c.exemption_reason_text));
    if el.is_empty() && text(&c.tax_scheme).is_none() {
        return None;
    }
    Some(el.with(El::new("cac:TaxScheme").with(El::leaf("cbc:ID", doc::tax_scheme(c)))))
}

fn indicator(b: bool) -> &'static str {
    if b { "true" } else { "false" }
}

impl<'d, 'a> B<'d, 'a> {
    pub(crate) fn new(doc: &'d Doc<'a>) -> Self {
        B {
            doc,
            inv: doc.inv,
            cur: text(&doc.inv.currency).unwrap_or(""),
        }
    }

    pub(crate) fn amount(&self, name: &'static str, d: &Dec<'a>) -> Option<El<'a>> {
        amount_in(name, d, self.cur)
    }

    fn refs(&self) -> Option<&'a pb::DocumentReferences> {
        self.inv.references.as_ref()
    }

    /// A field of `references`, or `""`.
    pub(crate) fn r(&self, f: impl Fn(&'a pb::DocumentReferences) -> &'a str) -> &'a str {
        self.refs().map_or("", f)
    }

    // ----------------------------------------------------------------------- header

    /// `CustomizationID`, `ProfileID`, `ProfileExecutionID`, `ID`, `UUID`, `IssueDate`,
    /// `IssueTime` (identical in both root sequences).
    pub(crate) fn head(&self, root: &mut El<'a>) {
        let inv = self.inv;
        root.push(El::leaf(
            "cbc:CustomizationID",
            self.doc.specification_identifier(),
        ));
        root.push(El::leaf("cbc:ProfileID", self.doc.business_process_type()));
        root.push(t("cbc:ProfileExecutionID", &inv.transaction_type_code));
        root.push(t("cbc:ID", &inv.invoice_number));
        root.push(t("cbc:UUID", &inv.uuid));
        root.push(t("cbc:IssueDate", &inv.issue_date));
        root.push(t("cbc:IssueTime", &inv.issue_time));
    }

    /// `DocumentCurrencyCode`, `TaxCurrencyCode`, `AccountingCost`, `BuyerReference`,
    /// `InvoicePeriod` (identical in both root sequences).
    pub(crate) fn currencies_to_period(&self, root: &mut El<'a>) {
        let inv = self.inv;
        root.push(t("cbc:DocumentCurrencyCode", &inv.currency));
        root.push(t("cbc:TaxCurrencyCode", &inv.tax_currency));
        root.push(t(
            "cbc:AccountingCost",
            self.r(|r| &r.buyer_accounting_reference),
        ));
        root.push(t("cbc:BuyerReference", self.r(|r| &r.buyer_reference)));
        let period = inv.invoicing_period.as_ref();
        root.push(
            El::new("cac:InvoicePeriod")
                .with(period.and_then(|p| t("cbc:StartDate", &p.start_date)))
                .with(period.and_then(|p| t("cbc:EndDate", &p.end_date)))
                .with(t("cbc:DescriptionCode", &inv.billing_frequency)),
        );
    }

    /// `OrderReference` (IBT-013, IBT-014; `NA` when only IBT-014 is set) and the
    /// `BillingReference`s (IBG-03).
    pub(crate) fn order_and_billing(&self, root: &mut El<'a>) {
        let po = text(self.r(|r| &r.purchase_order_reference));
        let so = t("cbc:SalesOrderID", self.r(|r| &r.sales_order_reference));
        if po.is_some() || so.is_some() {
            root.push(
                El::new("cac:OrderReference")
                    .with(El::leaf("cbc:ID", po.unwrap_or(PLACEHOLDER)))
                    .with(so),
            );
        }
        for p in &self.inv.preceding_invoices {
            root.push(
                El::new("cac:BillingReference").with(
                    El::new("cac:InvoiceDocumentReference")
                        .with(t("cbc:ID", &p.id))
                        .with(t("cbc:IssueDate", &p.issue_date)),
                ),
            );
        }
    }

    pub(crate) fn despatch_ref(&self) -> Option<El<'a>> {
        id_text_agg(
            "cac:DespatchDocumentReference",
            self.r(|r| &r.despatch_advice_reference),
        )
    }

    pub(crate) fn receipt_ref(&self) -> Option<El<'a>> {
        id_text_agg(
            "cac:ReceiptDocumentReference",
            self.r(|r| &r.receiving_advice_reference),
        )
    }

    /// BTAE-21.
    pub(crate) fn statement_ref(&self) -> Option<El<'a>> {
        id_text_agg(
            "cac:StatementDocumentReference",
            self.r(|r| &r.customs_reference),
        )
    }

    /// IBT-017.
    pub(crate) fn originator_ref(&self) -> Option<El<'a>> {
        id_text_agg(
            "cac:OriginatorDocumentReference",
            self.r(|r| &r.tender_or_lot_reference),
        )
    }

    /// IBT-012 and BTAE-05 (the decimal verbatim, no currency prefix). Without IBT-012 the
    /// required `ID` is missing (`AE-EXP-006`).
    pub(crate) fn contract_ref(&self) -> Option<El<'a>> {
        El::new("cac:ContractDocumentReference")
            .with(t("cbc:ID", self.r(|r| &r.contract_reference)))
            .with(num("cbc:DocumentDescription", &self.doc.contract_value))
            .non_empty()
    }

    /// The `AdditionalDocumentReference`s in contract order: invoiced object (130), supporting
    /// documents in model order, project reference (50, credit notes only), BTAE-20.
    pub(crate) fn additional_refs(&self, root: &mut El<'a>) {
        if let Some(id) = ident(
            "cbc:ID",
            self.refs().and_then(|r| r.invoiced_object.as_ref()),
        ) {
            root.push(
                El::new("cac:AdditionalDocumentReference")
                    .with(id)
                    .with(El::leaf("cbc:DocumentTypeCode", DOC_TYPE_INVOICED_OBJECT)),
            );
        }
        for d in &self.inv.supporting_documents {
            let uri = t("cbc:URI", &d.external_uri)
                .map(|u| El::new("cac:Attachment").with(El::new("cac:ExternalReference").with(u)));
            root.push(
                El::new("cac:AdditionalDocumentReference")
                    .with(t("cbc:ID", &d.reference))
                    .with(t("cbc:DocumentDescription", &d.description))
                    .with(uri),
            );
        }
        if self.doc.kind == DocKind::CreditNote
            && let Some(id) = t("cbc:ID", self.r(|r| &r.project_reference))
        {
            root.push(
                El::new("cac:AdditionalDocumentReference")
                    .with(id)
                    .with(El::leaf("cbc:DocumentTypeCode", DOC_TYPE_PROJECT)),
            );
        }
        let aed = &self.doc.totals.total_with_tax_aed;
        if aed.exists() {
            root.push(
                El::new("cac:AdditionalDocumentReference")
                    .with(El::leaf("cbc:ID", AED))
                    .with(El::leaf("cbc:DocumentTypeCode", DOC_TYPE_AED_TOTAL))
                    .with(El::leaf(
                        "cbc:DocumentDescription",
                        Cow::Owned(format!("{AED} {}", aed.raw)),
                    )),
            );
        }
    }

    // ----------------------------------------------------------------------- parties

    /// `cac:Party` of the seller or buyer; `vat` is IBT-031 / IBT-048.
    fn party(&self, p: Option<&'a pb::Party>, vat: &'a str) -> El<'a> {
        let mut el = El::new("cac:Party");
        el.push(ident(
            "cbc:EndpointID",
            p.and_then(|p| p.electronic_address.as_ref()),
        ));
        if let Some(p) = p {
            for i in &p.identifiers {
                el.push(id_agg("cac:PartyIdentification", Some(i)));
            }
            el.push(party_name(&p.trading_name));
            el.push(address("cac:PostalAddress", p.postal_address.as_ref()));
        }
        el.push(party_tax_scheme(vat, "VAT"));
        if let Some(p) = p {
            el.push(party_tax_scheme(&p.tax_registration_identifier, "TIN"));
            let company_id = p.legal_registration.as_ref().and_then(|lr| {
                let agency_name = if text(&lr.r#type) == Some("PAS") {
                    &lr.passport_issuing_country
                } else {
                    &lr.authority_name
                };
                t("cbc:CompanyID", &lr.id).map(|c| {
                    c.attr_opt("schemeID", text(&lr.scheme_id))
                        .attr_opt("schemeAgencyID", text(&lr.r#type))
                        .attr_opt("schemeAgencyName", text(agency_name))
                })
            });
            el.push(
                El::new("cac:PartyLegalEntity")
                    .with(t("cbc:RegistrationName", &p.name))
                    .with(company_id)
                    .with(t("cbc:CompanyLegalForm", &p.additional_legal_information)),
            );
            el.push(p.contact.as_ref().map(|c| {
                El::new("cac:Contact")
                    .with(t("cbc:Name", &c.name))
                    .with(t("cbc:Telephone", &c.telephone))
                    .with(t("cbc:ElectronicMail", &c.email))
            }));
        }
        el
    }

    /// `AccountingSupplierParty` (IBG-04).
    pub(crate) fn supplier(&self) -> Option<El<'a>> {
        El::new("cac:AccountingSupplierParty")
            .with(self.party(self.inv.seller.as_ref(), &self.inv.seller_trn))
            .non_empty()
    }

    /// `AccountingCustomerParty` (IBG-07).
    pub(crate) fn customer(&self) -> Option<El<'a>> {
        El::new("cac:AccountingCustomerParty")
            .with(self.party(self.inv.buyer.as_ref(), &self.inv.buyer_trn))
            .non_empty()
    }

    /// `PayeeParty` (IBG-10), then `BuyerCustomerParty` (BTAE-01), `SellerSupplierParty`
    /// (BTAE-14) and `TaxRepresentativeParty` (IBG-11): the same order in both roots.
    pub(crate) fn other_parties(&self, root: &mut El<'a>) {
        let inv = self.inv;
        root.push(inv.payee.as_ref().map(|p| {
            El::new("cac:PayeeParty")
                .with(id_agg("cac:PartyIdentification", p.identifier.as_ref()))
                .with(party_name(&p.name))
                .with(
                    ident("cbc:CompanyID", p.legal_registration.as_ref())
                        .map(|c| El::new("cac:PartyLegalEntity").with(c)),
                )
        }));
        let id_only = |wrapper: &'static str, id: &'a str| {
            id_text_agg("cac:PartyIdentification", id)
                .map(|pi| El::new(wrapper).with(El::new("cac:Party").with(pi)))
        };
        root.push(id_only("cac:BuyerCustomerParty", &inv.beneficiary_id));
        root.push(id_only("cac:SellerSupplierParty", &inv.principal_id));
        root.push(inv.tax_representative.as_ref().map(|r| {
            El::new("cac:TaxRepresentativeParty")
                .with(party_name(&r.name))
                .with(address("cac:PostalAddress", r.postal_address.as_ref()))
                .with(party_tax_scheme(&r.vat_identifier, "VAT"))
        }));
    }

    /// `Delivery` (IBG-13): `ActualDeliveryDate`, `DeliveryLocation`, `DeliveryParty`, then
    /// `DeliveryTerms` (BTAE-22) inside it.
    pub(crate) fn delivery(&self) -> Option<El<'a>> {
        let d = self.inv.delivery.as_ref()?;
        El::new("cac:Delivery")
            .with(t("cbc:ActualDeliveryDate", &d.actual_delivery_date))
            .with(
                El::new("cac:DeliveryLocation")
                    .with(ident("cbc:ID", d.location.as_ref()))
                    .with(address("cac:Address", d.address.as_ref())),
            )
            .with(party_name(&d.party_name).map(|n| El::new("cac:DeliveryParty").with(n)))
            .with(
                t("cbc:ID", &d.incoterms)
                    .map(|id| El::new("cac:DeliveryTerms").with(id.attr("schemeID", "Incoterms"))),
            )
            .non_empty()
    }

    // ----------------------------------------------------------------------- payment

    /// The `PaymentMeans` (IBG-16). For a credit note, IBT-009 goes into the one of
    /// `payment_instructions[0]` (the CreditNote XSD has no root `DueDate`), which is then
    /// written even when that instruction is otherwise empty.
    pub(crate) fn payment_means(&self, root: &mut El<'a>) {
        let mut due = match self.doc.kind {
            DocKind::CreditNote => t("cbc:PaymentDueDate", &self.inv.payment_due_date),
            DocKind::Invoice => None,
        };
        for pi in &self.inv.payment_instructions {
            let code = t("cbc:PaymentMeansCode", &pi.means_code)
                .map(|c| c.attr_opt("name", text(&pi.means_text)));
            let mut el = El::new("cac:PaymentMeans")
                .with(t("cbc:ID", &pi.id))
                .with(code)
                .with(due.take());
            for r in self.payment_means_tail(pi) {
                el.push(r);
            }
            root.push(el);
        }
    }

    /// `PaymentID`, `CardAccount`, `PayeeFinancialAccount`, `PaymentMandate` of one
    /// instruction.
    fn payment_means_tail(&self, pi: &'a pb::PaymentInstructions) -> Vec<El<'a>> {
        let mut out = Vec::new();
        for r in &pi.remittance_information {
            out.extend(ident("cbc:PaymentID", Some(r)));
        }
        if let Some(c) = &pi.card {
            let pan = t("cbc:PrimaryAccountNumberID", &c.primary_account_number);
            let holder = t("cbc:HolderName", &c.holder_name);
            let network = text(&c.network_id);
            if pan.is_some() || holder.is_some() || network.is_some() {
                out.push(
                    El::new("cac:CardAccount")
                        .with(pan)
                        .with(El::leaf("cbc:NetworkID", network.unwrap_or(PLACEHOLDER)))
                        .with(holder),
                );
            }
        }
        if let Some(ct) = &pi.credit_transfer {
            out.extend(
                El::new("cac:PayeeFinancialAccount")
                    .with(ident("cbc:ID", ct.account.as_ref()))
                    .with(t("cbc:Name", &ct.account_name))
                    .with(
                        El::new("cac:FinancialInstitutionBranch")
                            .with(t("cbc:ID", &ct.service_provider_id))
                            .with(address("cac:Address", ct.institution_address.as_ref())),
                    )
                    .non_empty(),
            );
        }
        if let Some(dd) = &pi.direct_debit {
            out.extend(
                El::new("cac:PaymentMandate")
                    .with(t("cbc:ID", &dd.mandate_reference))
                    .with(
                        t("cbc:ID", &dd.debited_account)
                            .map(|id| El::new("cac:PayerFinancialAccount").with(id)),
                    )
                    .non_empty(),
            );
        }
        out
    }

    /// The `PaymentTerms` (IBG-33).
    pub(crate) fn payment_terms(&self, root: &mut El<'a>) {
        for (pt, d) in self.inv.payment_terms.iter().zip(&self.doc.payment_terms) {
            root.push(
                El::new("cac:PaymentTerms")
                    .with(t("cbc:PaymentMeansID", &pt.instructions_id))
                    .with(t("cbc:Note", &pt.note))
                    .with(self.amount("cbc:Amount", &d.amount))
                    .with(t("cbc:InstallmentDueDate", &pt.installment_due_date)),
            );
        }
    }

    // ----------------------------------------------------------------------- allowances, tax

    /// A document-level (`with_tax`) or line-level `AllowanceCharge`.
    fn allowance_charge(
        &self,
        a: &'a pb::AllowanceCharge,
        d: &AllowanceChargeDec<'a>,
        with_tax: bool,
    ) -> El<'a> {
        El::new("cac:AllowanceCharge")
            .with(El::leaf("cbc:ChargeIndicator", indicator(a.is_charge)))
            .with(t("cbc:AllowanceChargeReasonCode", &a.reason_code))
            .with(t("cbc:AllowanceChargeReason", &a.reason))
            .with(num("cbc:MultiplierFactorNumeric", &d.percentage))
            .with(self.amount("cbc:Amount", &d.amount))
            .with(self.amount("cbc:BaseAmount", &d.base_amount))
            .with(if with_tax {
                tax_category("cac:TaxCategory", a.tax_category.as_ref(), &d.rate)
            } else {
                None
            })
    }

    /// The document-level allowances and charges (IBG-20, IBG-21).
    pub(crate) fn allowances_charges(&self, root: &mut El<'a>) {
        for (a, d) in self
            .inv
            .allowances_charges
            .iter()
            .zip(&self.doc.allowances_charges)
        {
            root.push(self.allowance_charge(a, d, true));
        }
    }

    /// `TaxExchangeRate` when BTAE-04 is present: IBT-005 to IBT-006 (`ibr-153-ae`). Without
    /// IBT-006 the required `TargetCurrencyCode` is missing (`AE-EXP-003`).
    pub(crate) fn tax_exchange_rate(&self) -> Option<El<'a>> {
        let rate = num("cbc:CalculationRate", &self.doc.exchange_rate)?;
        Some(
            El::new("cac:TaxExchangeRate")
                .with(t("cbc:SourceCurrencyCode", &self.inv.currency))
                .with(t("cbc:TargetCurrencyCode", &self.inv.tax_currency))
                .with(rate),
        )
    }

    /// At most two `TaxTotal`s (contract 0.2.1): the document-currency one (IBT-110, IBT-200,
    /// the breakdown) only when IBT-110 is present, then the accounting-currency one (IBT-111
    /// in IBT-006) only when IBT-111 is present.
    pub(crate) fn tax_totals(&self, root: &mut El<'a>) {
        if let Some(amount) = self.amount("cbc:TaxAmount", &self.doc.vat_amount) {
            let inclusive = self
                .inv
                .totals
                .as_ref()
                .is_some_and(|t| t.tax_inclusive_pricing);
            let mut tt = El::new("cac:TaxTotal").with(amount);
            if inclusive {
                tt.push(El::leaf("cbc:TaxIncludedIndicator", indicator(true)));
            }
            for (s, d) in self.inv.tax_breakdown.iter().zip(&self.doc.tax_breakdown) {
                tt.push(
                    El::new("cac:TaxSubtotal")
                        .with(self.amount("cbc:TaxableAmount", &d.taxable_amount))
                        .with(self.amount("cbc:TaxAmount", &d.tax_amount))
                        .with(tax_category(
                            "cac:TaxCategory",
                            s.category.as_ref(),
                            &d.rate,
                        )),
                );
            }
            root.push(tt);
        }
        let accounting = &self.doc.totals.tax_amount_accounting_currency;
        if let Some(amount) = amount_in(
            "cbc:TaxAmount",
            accounting,
            text(&self.inv.tax_currency).unwrap_or(""),
        ) {
            root.push(El::new("cac:TaxTotal").with(amount));
        }
    }

    /// `LegalMonetaryTotal` (IBG-22).
    pub(crate) fn legal_monetary_total(&self) -> Option<El<'a>> {
        let tot = &self.doc.totals;
        El::new("cac:LegalMonetaryTotal")
            .with(self.amount("cbc:LineExtensionAmount", &tot.line_extension_amount))
            .with(self.amount("cbc:TaxExclusiveAmount", &tot.tax_exclusive_amount))
            .with(self.amount("cbc:TaxInclusiveAmount", &self.doc.total_amount))
            .with(self.amount("cbc:AllowanceTotalAmount", &tot.allowance_total_amount))
            .with(self.amount("cbc:ChargeTotalAmount", &tot.charge_total_amount))
            .with(self.amount("cbc:PrepaidAmount", &tot.paid_amount))
            .with(self.amount("cbc:PayableRoundingAmount", &tot.rounding_amount))
            .with(self.amount("cbc:PayableAmount", &tot.payable_amount))
            .non_empty()
    }

    // ----------------------------------------------------------------------- lines

    /// Every line (IBG-25), `cac:InvoiceLine` or `cac:CreditNoteLine`.
    pub(crate) fn lines(&self, root: &mut El<'a>) {
        for (l, d) in self.inv.lines.iter().zip(&self.doc.lines) {
            root.push(self.line(l, d));
        }
    }

    /// One line in `InvoiceLineType` / `CreditNoteLineType` order: `ID, Note, Quantity,
    /// LineExtensionAmount, AccountingCost, InvoicePeriod, OrderLineReference,
    /// DespatchLineReference, DocumentReference, AllowanceCharge*, Item, Price,
    /// ItemPriceExtension`. A credit-note line never gets a `DiscrepancyResponse` (not modelled).
    fn line(&self, l: &'a pb::InvoiceLine, d: &LineDec<'a>) -> El<'a> {
        let (name, qty) = match self.doc.kind {
            DocKind::Invoice => ("cac:InvoiceLine", "cbc:InvoicedQuantity"),
            DocKind::CreditNote => ("cac:CreditNoteLine", "cbc:CreditedQuantity"),
        };
        let mut el = El::new(name)
            .with(t("cbc:ID", &l.id))
            .with(t("cbc:Note", &l.note))
            .with(num(qty, &d.quantity).map(|q| q.attr_opt("unitCode", text(&l.unit_code))))
            .with(self.amount("cbc:LineExtensionAmount", &d.net_amount))
            .with(t("cbc:AccountingCost", &l.accounting_reference))
            .with(l.period.as_ref().map(|p| {
                El::new("cac:InvoicePeriod")
                    .with(t("cbc:StartDate", &p.start_date))
                    .with(t("cbc:EndDate", &p.end_date))
            }));
        let order = t("cbc:ID", &l.order_reference);
        let order_line = text(&l.order_line_reference);
        if order.is_some() || order_line.is_some() {
            el.push(
                El::new("cac:OrderLineReference")
                    .with(El::leaf("cbc:LineID", order_line.unwrap_or(PLACEHOLDER)))
                    .with(order.map(|o| El::new("cac:OrderReference").with(o))),
            );
        }
        el.push(
            id_text_agg("cac:DocumentReference", &l.despatch_advice_reference).map(|dr| {
                El::new("cac:DespatchLineReference")
                    .with(El::leaf("cbc:LineID", PLACEHOLDER))
                    .with(dr)
            }),
        );
        el.push(ident("cbc:ID", l.object_identifier.as_ref()).map(|id| {
            El::new("cac:DocumentReference")
                .with(id)
                .with(El::leaf("cbc:DocumentTypeCode", DOC_TYPE_INVOICED_OBJECT))
        }));
        for (a, ad) in l.allowances_charges.iter().zip(&d.allowances_charges) {
            el.push(self.allowance_charge(a, ad, false));
        }
        el.push(self.item(l, d));
        el.push(self.price(l, d));
        el.push(amount_in("cbc:Amount", &d.amount_aed, AED).map(|amount| {
            El::new("cac:ItemPriceExtension").with(amount).with(
                amount_in("cbc:TaxAmount", &d.vat_amount_aed, AED)
                    .map(|tax| El::new("cac:TaxTotal").with(tax)),
            )
        }));
        el
    }

    /// `Item` (IBG-31) in `ItemType` order.
    fn item(&self, l: &'a pb::InvoiceLine, d: &LineDec<'a>) -> El<'a> {
        let tax = tax_category("cac:ClassifiedTaxCategory", l.tax.as_ref(), &d.rate);
        let lot = t("cbc:LotNumberID", &l.batch_number)
            .map(|n| El::new("cac:ItemInstance").with(El::new("cac:LotIdentification").with(n)));
        let Some(it) = l.item.as_ref() else {
            return El::new("cac:Item").with(tax).with(lot);
        };
        let mut el = El::new("cac:Item")
            .with(t("cbc:Description", &it.description))
            .with(t("cbc:Name", &it.name))
            .with(id_text_agg(
                "cac:BuyersItemIdentification",
                &it.buyer_item_id,
            ))
            .with(id_text_agg(
                "cac:SellersItemIdentification",
                &it.seller_item_id,
            ))
            .with(id_agg(
                "cac:StandardItemIdentification",
                it.standard_id.as_ref(),
            ));
        for sac in &it.service_accounting_codes {
            el.push(t("cbc:ID", &sac.code).map(|id| {
                El::new("cac:AdditionalItemIdentification").with(
                    id.attr_opt("schemeID", text(&sac.scheme_id))
                        .attr_opt("schemeVersionID", text(&sac.scheme_version)),
                )
            }));
        }
        el.push(
            t("cbc:IdentificationCode", &it.origin_country)
                .map(|c| El::new("cac:OriginCountry").with(c)),
        );
        let mut codes = it.classifications.iter().filter_map(|c| {
            t("cbc:ItemClassificationCode", &c.code).map(|code| {
                code.attr_opt("listID", text(&c.scheme_id))
                    .attr_opt("listVersionID", text(&c.scheme_version))
            })
        });
        el.push(
            El::new("cac:CommodityClassification")
                .with(t("cbc:NatureCode", &it.goods_service_type))
                .with(t("cbc:CommodityCode", &it.item_type))
                .with(codes.next()),
        );
        for code in codes {
            el.push(El::new("cac:CommodityClassification").with(code));
        }
        el.push(tax);
        for a in &it.attributes {
            el.push(t("cbc:Name", &a.name).map(|n| {
                El::new("cac:AdditionalItemProperty")
                    .with(n)
                    .with(t("cbc:Value", &a.value))
            }));
        }
        el.with(lot)
    }

    /// `Price` (IBG-29): `PriceAmount`, `BaseQuantity`, and the price `AllowanceCharge`
    /// (`ChargeIndicator` `false`, IBT-147 as `Amount`, IBT-148 as `BaseAmount`) when IBT-147 or
    /// IBT-148 is present.
    fn price(&self, l: &'a pb::InvoiceLine, d: &LineDec<'a>) -> Option<El<'a>> {
        let p = &d.price;
        let unit = l
            .price
            .as_ref()
            .and_then(|p| text(&p.base_quantity_unit_code));
        let discount = self.amount("cbc:Amount", &p.discount);
        let gross = self.amount("cbc:BaseAmount", &p.gross_price);
        let ac = (discount.is_some() || gross.is_some()).then(|| {
            El::new("cac:AllowanceCharge")
                .with(El::leaf(
                    "cbc:ChargeIndicator",
                    indicator(doc::PRICE_CHARGE_INDICATOR),
                ))
                .with(discount)
                .with(gross)
        });
        El::new("cac:Price")
            .with(self.amount("cbc:PriceAmount", &p.net_price))
            .with(num("cbc:BaseQuantity", &p.base_quantity).map(|q| q.attr_opt("unitCode", unit)))
            .with(ac)
            .non_empty()
    }
}

/// Test support shared by the exporter, importer and platform-rule tests: the XSD content
/// models of the elements the exporter writes, read from the vendored UBL 2.1 XSDs
/// (`scripts/fetch-upstream.sh`), and "maximal" documents that set every field.
#[cfg(test)]
pub(crate) mod testing {
    use std::collections::HashMap;
    use std::sync::LazyLock;

    use prost_reflect::{Kind, MessageDescriptor};
    use serde_json::{Map, Value};

    use crate::conformance::apply_patch;
    use crate::doc::Doc;
    use crate::pb;

    /// One child of an XSD sequence: qualified name (`cbc:ID`), minOccurs, maxOccurs (None =
    /// unbounded).
    #[derive(Debug, Clone)]
    pub struct Particle {
        pub name: String,
        pub min: u32,
        pub max: Option<u32>,
    }

    /// Element name -> content model, for the CAC aggregates and the two roots.
    pub struct Xsd {
        pub models: HashMap<String, Vec<Particle>>,
    }

    const XS: &str = "http://www.w3.org/2001/XMLSchema";

    fn read(rel: &str) -> String {
        let p = crate::conformance::corpus::ruleset_dir()
            .join("upstream/xsd")
            .join(rel);
        std::fs::read_to_string(&p)
            .unwrap_or_else(|e| panic!("{}: {e}; run scripts/fetch-upstream.sh first", p.display()))
    }

    fn sequences(src: &str) -> (HashMap<String, Vec<Particle>>, HashMap<String, String>) {
        let doc = roxmltree::Document::parse(src).unwrap();
        let mut types = HashMap::new();
        let mut elements = HashMap::new();
        for n in doc.root_element().children().filter(|n| n.is_element()) {
            match n.tag_name().name() {
                "complexType" if n.tag_name().namespace() == Some(XS) => {
                    let parts = n
                        .descendants()
                        .filter(|e| e.has_tag_name((XS, "element")))
                        .filter_map(|e| {
                            let r = e.attribute("ref")?;
                            Some(Particle {
                                name: r.to_string(),
                                min: e.attribute("minOccurs").map_or(1, |v| v.parse().unwrap()),
                                max: match e.attribute("maxOccurs") {
                                    None => Some(1),
                                    Some("unbounded") => None,
                                    Some(v) => Some(v.parse().unwrap()),
                                },
                            })
                        })
                        .collect();
                    types.insert(n.attribute("name").unwrap().to_string(), parts);
                }
                "element" if n.tag_name().namespace() == Some(XS) => {
                    if let (Some(name), Some(ty)) = (n.attribute("name"), n.attribute("type")) {
                        let ty = ty.rsplit(':').next().unwrap().to_string();
                        elements.insert(name.to_string(), ty);
                    }
                }
                _ => {}
            }
        }
        (types, elements)
    }

    pub fn xsd() -> &'static Xsd {
        static X: LazyLock<Xsd> = LazyLock::new(|| {
            let mut models = HashMap::new();
            let (types, elements) =
                sequences(&read("common/UBL-CommonAggregateComponents-2.1.xsd"));
            for (el, ty) in elements {
                if let Some(seq) = types.get(&ty) {
                    models.insert(format!("cac:{el}"), seq.clone());
                }
            }
            for (file, root, ty) in [
                ("maindoc/UBL-Invoice-2.1.xsd", "Invoice", "InvoiceType"),
                (
                    "maindoc/UBL-CreditNote-2.1.xsd",
                    "CreditNote",
                    "CreditNoteType",
                ),
            ] {
                let (types, _) = sequences(&read(file));
                models.insert(root.to_string(), types[ty].clone());
            }
            Xsd { models }
        });
        &X
    }

    fn qname(n: roxmltree::Node) -> String {
        match n.tag_name().namespace() {
            Some(super::NS_CAC) => format!("cac:{}", n.tag_name().name()),
            Some(super::NS_CBC) => format!("cbc:{}", n.tag_name().name()),
            _ => n.tag_name().name().to_string(),
        }
    }

    /// Slash path of an element (`Invoice/cac:InvoiceLine/cac:Item`).
    pub fn path(n: roxmltree::Node) -> String {
        let mut parts: Vec<String> = n
            .ancestors()
            .filter(|a| a.is_element())
            .map(qname)
            .collect();
        parts.reverse();
        parts.join("/")
    }

    /// One way an element breaks its XSD content model.
    #[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
    pub enum Violation {
        /// `(parent element name, required child)`.
        Missing(String, String),
        /// `(element path, child)`: not allowed, out of order, or too many.
        Order(String, String),
        /// An element without text and children.
        Empty(String),
    }

    /// Checks every aggregate of `xml` against its XSD sequence (allowed children, order,
    /// maxOccurs, minOccurs) and that no element is empty. Datatypes are not checked (dates and
    /// times are covered by `ibr-073`, `ibr-119` and the `AE-EXP` date rules; decimals are
    /// written only when they parse).
    pub fn violations(xml: &str) -> Vec<Violation> {
        let x = xsd();
        let doc = roxmltree::Document::parse(xml).unwrap();
        let mut out = Vec::new();
        for n in doc.descendants().filter(|n| n.is_element()) {
            let kids: Vec<_> = n.children().filter(|c| c.is_element()).collect();
            if kids.is_empty() && n.text().is_none_or(|t| t.trim().is_empty()) {
                out.push(Violation::Empty(path(n)));
            }
            let name = qname(n);
            if name.starts_with("cbc:") {
                continue;
            }
            let model = x
                .models
                .get(&name)
                .unwrap_or_else(|| panic!("no XSD model for {name}"));
            let mut pos = 0usize;
            let mut count = 0u32;
            for k in &kids {
                let kn = qname(*k);
                match model[pos..].iter().position(|p| p.name == kn) {
                    Some(0) => count += 1,
                    Some(skip) => {
                        pos += skip;
                        count = 1;
                    }
                    None => {
                        out.push(Violation::Order(path(n), kn));
                        continue;
                    }
                }
                if model[pos].max.is_some_and(|m| count > m) {
                    out.push(Violation::Order(path(n), kn));
                }
            }
            for p in model.iter().filter(|p| p.min > 0) {
                if !kids.iter().any(|k| qname(*k) == p.name) {
                    out.push(Violation::Missing(name.clone(), p.name.clone()));
                }
            }
        }
        out.sort();
        out.dedup();
        out
    }

    /// Exports `inv` as a string.
    pub fn export_str(inv: &pb::Invoice) -> String {
        String::from_utf8(super::to_xml(&Doc::new(inv)).expect("exportable")).unwrap()
    }

    fn value_for(path: &str, template: &str, type_code: &str) -> Value {
        let leaf = template.rsplit('.').next().unwrap();
        let s = |v: &str| Value::String(v.to_string());
        if Doc::DECIMAL_FIELDS.iter().any(|f| f.path == template) {
            return s("1");
        }
        if leaf.ends_with("date") {
            return s("2025-01-01");
        }
        match template {
            "issue_time" => s("10:00:00+04:00"),
            "invoice_type_code" => s(type_code),
            "transaction_type_code" => s("00000000"),
            "currency" => s("USD"),
            "tax_currency" => s("AED"),
            "seller.legal_registration.type" | "buyer.legal_registration.type" => s("TL"),
            "lines[#].item.service_accounting_codes[#].scheme_id" => s("SAC"),
            _ => s(path),
        }
    }

    fn walk(
        msg: &MessageDescriptor,
        prefix: &str,
        tprefix: &str,
        type_code: &str,
        out: &mut Map<String, Value>,
    ) {
        for f in msg.fields() {
            let reps: &[Option<usize>] = if f.is_list() {
                &[Some(0), Some(1)]
            } else {
                &[None]
            };
            for r in reps {
                let (p, tp) = match r {
                    Some(i) => (
                        format!("{prefix}{}[{i}]", f.name()),
                        format!("{tprefix}{}[#]", f.name()),
                    ),
                    None => (
                        format!("{prefix}{}", f.name()),
                        format!("{tprefix}{}", f.name()),
                    ),
                };
                match f.kind() {
                    Kind::Message(m) => {
                        walk(&m, &format!("{p}."), &format!("{tp}."), type_code, out)
                    }
                    Kind::Bool => {
                        out.insert(p, Value::Bool(true));
                    }
                    Kind::String => {
                        let v = value_for(&p, &tp, type_code);
                        out.insert(p, v);
                    }
                    k => panic!("unexpected field kind {k:?} at {p}"),
                }
            }
        }
    }

    /// Every string path of the canonical invoice, as `(concrete path with indices 0 and 1,
    /// template)`, for a document of `type_code`, with a value per field.
    pub fn every_field(type_code: &str) -> Map<String, Value> {
        let pool = prost_reflect::DescriptorPool::decode(crate::FILE_DESCRIPTOR_SET).unwrap();
        let invoice = pool.get_message_by_name("compliance.v1.Invoice").unwrap();
        let mut out = Map::new();
        walk(&invoice, "", "", type_code, &mut out);
        out
    }

    /// A document of `type_code` with every field set (two elements per repeated field). The
    /// credit-note reason is set only on a credit note.
    pub fn maximal(type_code: &str) -> pb::Invoice {
        let mut set = every_field(type_code);
        if !matches!(type_code, "381" | "81") {
            set.remove("credit_note_reason_code");
        }
        let mut inv = pb::Invoice::default();
        apply_patch(&mut inv, &set, &[]).unwrap();
        inv
    }
}

#[cfg(test)]
mod tests {
    use super::testing::{Violation, export_str, maximal, violations};
    use super::*;
    use crate::conformance::{apply_patch, examples};
    use serde_json::{Map, Value, json};
    use sha2::{Digest, Sha256};

    fn example(slug: &str) -> pb::Invoice {
        examples()
            .into_iter()
            .find(|(s, _)| s == slug)
            .unwrap_or_else(|| panic!("no example {slug}"))
            .1
    }

    fn patched(base: &pb::Invoice, set: Value, remove: &[&str]) -> pb::Invoice {
        let mut inv = base.clone();
        let set: Map<String, Value> = serde_json::from_value(set).unwrap();
        let remove: Vec<String> = remove.iter().map(|s| s.to_string()).collect();
        apply_patch(&mut inv, &set, &remove).unwrap();
        inv
    }

    fn golden_path(slug: &str) -> std::path::PathBuf {
        std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("tests/golden")
            .join(format!("{slug}.xml"))
    }

    /// The two golden files pin the output format byte for byte.
    /// `UPDATE_GOLDEN=1 cargo test export::` rewrites them.
    #[test]
    fn golden_files_are_byte_identical() {
        let update = std::env::var("UPDATE_GOLDEN").is_ok_and(|v| v == "1");
        for slug in ["standard-tax-invoice", "standard-tax-credit-note"] {
            let xml = to_xml(&Doc::new(&example(slug))).unwrap();
            let path = golden_path(slug);
            if update {
                std::fs::create_dir_all(path.parent().unwrap()).unwrap();
                std::fs::write(&path, &xml).unwrap();
            }
            let golden = std::fs::read(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()));
            assert!(
                golden == xml,
                "{slug}: export differs from {}",
                path.display()
            );
        }
    }

    #[test]
    fn output_is_deterministic() {
        for (slug, inv) in examples() {
            let a = Sha256::digest(to_xml(&Doc::new(&inv)).unwrap());
            let copy = inv.clone();
            let b = Sha256::digest(to_xml(&Doc::new(&copy)).unwrap());
            assert_eq!(a, b, "{slug}");
        }
        let xml = export_str(&example("standard-tax-invoice"));
        assert!(xml.starts_with("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<Invoice xmlns=\""));
        assert!(xml.ends_with("</Invoice>\n") && !xml.contains('\r') && !xml.contains('\t'));
        assert!(xml.lines().skip(1).all(|l| {
            let indent = l.len() - l.trim_start_matches(' ').len();
            indent % 2 == 0
        }));
    }

    #[test]
    fn every_example_exports_in_xsd_order_without_empty_elements() {
        for (slug, inv) in examples() {
            let xml = export_str(&inv);
            assert_eq!(violations(&xml), [], "{slug}");
        }
    }

    /// Every field of the model set (two elements per repeated field): the full emitter order
    /// for both kinds, including aggregates no official example uses.
    #[test]
    fn maximal_documents_export_in_xsd_order() {
        for code in ["380", "381"] {
            let xml = export_str(&maximal(code));
            assert_eq!(violations(&xml), [], "{code}\n{xml}");
        }
    }

    #[test]
    fn the_root_and_line_kinds_follow_the_type_code() {
        for (code, root, line, qty) in [
            (
                "380",
                "<Invoice ",
                "<cac:InvoiceLine>",
                "<cbc:InvoicedQuantity",
            ),
            (
                "480",
                "<Invoice ",
                "<cac:InvoiceLine>",
                "<cbc:InvoicedQuantity",
            ),
            (
                "381",
                "<CreditNote ",
                "<cac:CreditNoteLine>",
                "<cbc:CreditedQuantity",
            ),
            (
                "81",
                "<CreditNote ",
                "<cac:CreditNoteLine>",
                "<cbc:CreditedQuantity",
            ),
        ] {
            let inv = patched(
                &example("standard-tax-invoice"),
                json!({"invoice_type_code": code}),
                &[],
            );
            let xml = export_str(&inv);
            assert!(
                xml.contains(root) && xml.contains(line) && xml.contains(qty),
                "{code}"
            );
            assert_eq!(
                document_kind(Doc::new(&inv).kind),
                if root == "<Invoice " {
                    "invoice"
                } else {
                    "credit_note"
                }
            );
        }
    }

    /// currencyID: IBT-005 everywhere, AED on BTAE-10/BTAE-08, IBT-006 on IBT-111 (spec 5.3.1).
    #[test]
    fn currency_ids_follow_the_bindings() {
        let inv = example("exports");
        assert_eq!(inv.currency, "USD");
        let xml = export_str(&inv);
        let doc = roxmltree::Document::parse(&xml).unwrap();
        let mut seen = 0;
        for n in doc
            .descendants()
            .filter(|n| n.attribute("currencyID").is_some())
        {
            let p = super::testing::path(n);
            // The second root-level TaxTotal is the accounting-currency one (IBT-111).
            let accounting = p == "Invoice/cac:TaxTotal/cbc:TaxAmount"
                && n.parent()
                    .unwrap()
                    .prev_sibling_element()
                    .is_some_and(|s| s.has_tag_name((NS_CAC, "TaxTotal")));
            let want = if accounting || p.contains("cac:ItemPriceExtension") {
                "AED"
            } else {
                "USD"
            };
            assert_eq!(n.attribute("currencyID"), Some(want), "{p}");
            seen += 1;
        }
        assert!(seen >= 10);
        assert!(xml.contains("<cac:ItemPriceExtension>\n      <cbc:Amount currencyID=\"AED\">"));
        // The accounting-currency TaxTotal carries IBT-006, whatever it is.
        let other = patched(&inv, json!({"tax_currency": "EUR"}), &[]);
        let xml = export_str(&other);
        assert!(
            xml.contains("<cac:TaxTotal>\n    <cbc:TaxAmount currencyID=\"EUR\">"),
            "{xml}"
        );
    }

    /// At most two TaxTotals, document currency first (contract 0.2.1, `TaxTotal*`).
    #[test]
    fn tax_totals_are_written_only_for_present_amounts() {
        let base = example("exports");
        let count = |inv: &pb::Invoice| {
            export_str(inv).matches("<cac:TaxTotal>").count()
                - export_str(inv).matches("<cac:ItemPriceExtension>").count()
        };
        assert_eq!(count(&base), 2);
        let no_111 = patched(
            &base,
            json!({"totals.tax_amount_accounting_currency": ""}),
            &[],
        );
        assert_eq!(count(&no_111), 1);
        let no_110 = patched(&base, json!({"vat_amount": ""}), &[]);
        assert_eq!(count(&no_110), 1);
        let xml = export_str(&no_110);
        assert!(
            !xml.contains("<cac:TaxSubtotal>"),
            "the breakdown is dropped with IBT-110"
        );
        assert!(!xml.contains("TaxIncludedIndicator"));
        let none = patched(
            &no_110,
            json!({"totals.tax_amount_accounting_currency": ""}),
            &[],
        );
        assert_eq!(count(&none), 0);
        // IBT-200 only when true, inside the document-currency TaxTotal.
        let incl = patched(&base, json!({"totals.tax_inclusive_pricing": true}), &[]);
        let xml = export_str(&incl);
        assert!(xml.contains("<cbc:TaxIncludedIndicator>true</cbc:TaxIncludedIndicator>"));
        assert_eq!(violations(&xml), []);
    }

    #[test]
    fn placeholders_are_written_where_the_xsd_needs_a_value() {
        let base = example("standard-invoice-extensive");
        let inv = patched(
            &base,
            json!({
                "references.purchase_order_reference": "",
                "lines[0].order_line_reference": "",
                "lines[0].despatch_advice_reference": "DA-1",
                "payment_instructions[0].card.network_id": "",
            }),
            &[],
        );
        let xml = export_str(&inv);
        assert!(xml.contains("<cac:OrderReference>\n    <cbc:ID>NA</cbc:ID>\n    <cbc:SalesOrderID>SO-001/23</cbc:SalesOrderID>"), "{xml}");
        assert!(xml.contains("<cac:OrderLineReference>\n      <cbc:LineID>NA</cbc:LineID>"));
        assert!(xml.contains("<cac:DespatchLineReference>\n      <cbc:LineID>NA</cbc:LineID>\n      <cac:DocumentReference>\n        <cbc:ID>DA-1</cbc:ID>"));
        assert!(xml.contains("<cbc:NetworkID>NA</cbc:NetworkID>"));
        assert_eq!(violations(&xml), []);
    }

    #[test]
    fn btae_20_gets_the_aed_prefix_and_btae_05_is_verbatim() {
        let xml = export_str(&example("exports"));
        assert!(xml.contains(
            "<cac:AdditionalDocumentReference>\n    <cbc:ID>AED</cbc:ID>\n    <cbc:DocumentTypeCode>aedtotal-incl-vat</cbc:DocumentTypeCode>\n    <cbc:DocumentDescription>AED 913621.44</cbc:DocumentDescription>"
        ), "{xml}");
        let xml = export_str(&example("standard-invoice-extensive"));
        assert!(xml.contains("<cbc:DocumentDescription>200000</cbc:DocumentDescription>"));
    }

    #[test]
    fn credit_note_bindings() {
        let base = example("standard-tax-credit-note");
        let inv = patched(
            &base,
            json!({
                "payment_due_date": "2025-03-01",
                "payment_instructions[0].means_code": "30",
                "references.project_reference": "PRJ-1",
            }),
            &[],
        );
        let xml = export_str(&inv);
        assert!(!xml.contains("<cbc:DueDate>") && !xml.contains("<cac:ProjectReference>"));
        assert!(xml.contains("<cac:PaymentMeans>\n    <cbc:PaymentMeansCode>30</cbc:PaymentMeansCode>\n    <cbc:PaymentDueDate>2025-03-01</cbc:PaymentDueDate>"), "{xml}");
        assert!(xml.contains(
            "<cbc:ID>PRJ-1</cbc:ID>\n    <cbc:DocumentTypeCode>50</cbc:DocumentTypeCode>"
        ));
        assert!(xml.contains(
            "<cac:DiscrepancyResponse>\n    <cbc:ResponseCode>DL8.61.1.E</cbc:ResponseCode>"
        ));
        assert_eq!(violations(&xml), []);
        // Without payment instructions the due date cannot be written (AE-EXP-001).
        let lost = patched(&base, json!({"payment_due_date": "2025-03-01"}), &[]);
        assert!(!export_str(&lost).contains("2025-03-01"));
        // An invoice writes it at the root and its project reference as ProjectReference.
        let inv = patched(&inv, json!({"invoice_type_code": "380"}), &[]);
        let xml = export_str(&inv);
        assert!(xml.contains("<cbc:DueDate>2025-03-01</cbc:DueDate>"));
        assert!(xml.contains("<cac:ProjectReference>\n    <cbc:ID>PRJ-1</cbc:ID>"));
        assert!(!xml.contains("DiscrepancyResponse") && !xml.contains("<cbc:PaymentDueDate>"));
    }

    /// Absent means not written: blank text, invalid decimals and empty aggregates.
    #[test]
    fn absent_and_invalid_values_are_not_written() {
        let base = example("standard-tax-invoice");
        let inv = patched(
            &base,
            json!({
                "note": "  \t",
                "totals.paid_amount": "1,000.00",
                "references.contract_value": "AED200000",
                "tax_representative.name": " ",
                "lines[0].item.attributes[1].value": "v",
                "lines[0].item.classifications[0].scheme_id": "HS",
                "invoice_number": " AE-01TEST ",
            }),
            &[],
        );
        let xml = export_str(&inv);
        assert!(!xml.contains("<cbc:Note>Tax invoice</cbc:Note>"));
        assert!(!xml.contains("PrepaidAmount") && !xml.contains("ContractDocumentReference"));
        assert!(
            !xml.contains("TaxRepresentativeParty") && !xml.contains("<cbc:Value>v</cbc:Value>")
        );
        assert_eq!(xml.matches("<cac:AdditionalItemProperty>").count(), 1);
        assert!(!xml.contains("CommodityClassification"));
        assert!(
            xml.contains("<cbc:ID>AE-01TEST</cbc:ID>"),
            "text is written trimmed"
        );
        assert_eq!(violations(&xml), []);
    }

    #[test]
    fn defaults_are_written_for_empty_fields() {
        let inv = patched(
            &example("standard-tax-invoice"),
            json!({
                "process.business_process_type": "",
                "process.specification_identifier": " ",
                "lines[0].tax.tax_scheme": "",
            }),
            &[],
        );
        let xml = export_str(&inv);
        assert!(xml.contains("<cbc:CustomizationID>urn:peppol:pint:billing-1@ae-1</cbc:CustomizationID>\n  <cbc:ProfileID>urn:peppol:bis:billing</cbc:ProfileID>"));
        assert!(xml.contains("<cac:ClassifiedTaxCategory>\n        <cbc:ID>S</cbc:ID>\n        <cbc:Percent>5</cbc:Percent>\n        <cac:TaxScheme>\n          <cbc:ID>VAT</cbc:ID>"), "{xml}");
    }

    #[test]
    fn a_forbidden_character_fails_the_export() {
        let inv = patched(
            &example("standard-tax-invoice"),
            json!({"note": "a\u{1}b"}),
            &[],
        );
        assert_eq!(
            to_xml(&Doc::new(&inv)),
            Err(ExportError::ForbiddenChar {
                element: "cbc:Note",
                ch: '\u{1}'
            })
        );
        // Trimmed whitespace (U+000B and U+000C are Unicode White_Space) is never written.
        let inv = patched(
            &example("standard-tax-invoice"),
            json!({"note": "\u{b}Tax\u{c}"}),
            &[],
        );
        assert!(export_str(&inv).contains("<cbc:Note>Tax</cbc:Note>"));
    }

    #[test]
    fn the_violation_checker_reports_order_missing_and_empty() {
        let ok = export_str(&example("standard-tax-invoice"));
        let swapped = ok
            .replacen(
                "<cbc:ProfileID>urn:peppol:bis:billing</cbc:ProfileID>\n",
                "",
                1,
            )
            .replacen(
                "<cbc:ID>AE-01TEST</cbc:ID>\n",
                "<cbc:ID>AE-01TEST</cbc:ID>\n  <cbc:ProfileID>x</cbc:ProfileID>\n",
                1,
            );
        assert!(violations(&swapped).iter().any(
            |v| matches!(v, Violation::Order(p, c) if p == "Invoice" && c == "cbc:ProfileID")
        ));
        let missing = ok.replacen(
            "<cbc:PayableAmount currencyID=\"AED\">11175.5</cbc:PayableAmount>",
            "",
            1,
        );
        assert!(
            violations(&missing).contains(&Violation::Missing(
                "cac:LegalMonetaryTotal".into(),
                "cbc:PayableAmount".into()
            )),
            "{:?}",
            violations(&missing)
        );
        let empty = ok.replacen(
            "<cbc:Note>Tax invoice</cbc:Note>",
            "<cbc:Note> </cbc:Note>",
            1,
        );
        assert!(violations(&empty).contains(&Violation::Empty("Invoice/cbc:Note".into())));
    }
}
