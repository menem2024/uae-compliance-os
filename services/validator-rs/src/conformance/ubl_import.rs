//! UBL 2.1 XML to canonical `pb::Invoice` importer, used by the differential conformance
//! tests to turn the official examples into corpus fixtures and to round-trip the exporter.
//!
//! The mapping is the inverse of the "CI mapping table" of `canonical-invoice.md` v0.2.1 (the
//! exporter's bindings). Nothing is dropped silently: every element and attribute the importer
//! does not consume is listed in [`ImportReport::ignored`] as a slash path
//! (`CreditNote/cac:CreditNoteLine/cac:DiscrepancyResponse`). Deliberate non-modelled content
//! that is consumed without being stored:
//!
//! * `currencyID` attributes and `TaxExchangeRate/{Source,Target}CurrencyCode` (derived from
//!   IBT-005 / IBT-006 by the exporter), `DespatchLineReference/LineID` (always `NA`);
//! * the base64 text of `EmbeddedDocumentBinaryObject` (the v2 exporter does not embed
//!   attachments; `mimeCode` and `filename` are kept);
//! * the `NA` placeholders the exporter writes where UBL requires a value that the business
//!   term does not have (`OrderReference/ID` with only IBT-014, `OrderLineReference/LineID`
//!   with only IBT-183, `CardAccount/NetworkID`), which import back as empty fields;
//! * the currency prefix of the BTAE-05 text (`AED200000`): the canonical contract value is a
//!   bare decimal string. A prefix equal to IBT-005 is redundant; any other currency has no
//!   canonical home and is reported as `.../cbc:DocumentDescription#currency`.
//!
//! The only content of the 30 official examples that is not modelled is the line-level
//! `DiscrepancyResponse` of `Volume-discount-credit-note.xml` (upstream defect 3).

use std::cell::RefCell;
use std::collections::HashSet;

use roxmltree::{Document, Node, NodeId};

use crate::pb;

const NS_CAC: &str = "urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2";
const NS_CBC: &str = "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2";
const NS_INVOICE: &str = "urn:oasis:names:specification:ubl:schema:xsd:Invoice-2";
const NS_CREDIT_NOTE: &str = "urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2";
const NS_XSI: &str = "http://www.w3.org/2001/XMLSchema-instance";

/// What the importer left out of the canonical invoice.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct ImportReport {
    /// One entry per unconsumed element (`Root/cac:A/cbc:B`), attribute (`.../cbc:B@attr`) or
    /// dropped part of an element's text (`.../cbc:B#part`).
    pub ignored: Vec<String>,
}

/// The XML is not well-formed or not a UBL 2.1 Invoice / CreditNote.
#[derive(Debug, PartialEq, Eq)]
pub struct ImportError(pub String);

impl std::fmt::Display for ImportError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "ubl import: {}", self.0)
    }
}

impl std::error::Error for ImportError {}

type N<'a, 'i> = Node<'a, 'i>;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Ns {
    Cac,
    Cbc,
}

impl Ns {
    fn uri(self) -> &'static str {
        match self {
            Ns::Cac => NS_CAC,
            Ns::Cbc => NS_CBC,
        }
    }
}

struct Imp {
    visited: RefCell<HashSet<NodeId>>,
    attrs: RefCell<HashSet<(NodeId, String)>>,
    /// Elements whose text was consumed only in part, with the name of the dropped part.
    dropped: RefCell<Vec<(NodeId, &'static str)>>,
    credit_note: bool,
}

fn nz<T: Default + PartialEq>(t: T) -> Option<T> {
    if t == T::default() { None } else { Some(t) }
}

/// Splits BTAE-05 text of the form `<ISO 4217 code>[spaces]<decimal>` into the code and the
/// decimal. The official examples write the contract value with a currency prefix
/// (`AED200000`, `AED 1000000`), while the canonical field is a bare decimal string (CI rule 10
/// and the BTAE-05 mapping row). Any other text is `None` and is imported verbatim, so
/// `AE-FMT-001` reports it instead of the importer guessing.
fn split_currency_prefix(text: &str) -> Option<(&str, &str)> {
    let (code, rest) = text.split_at_checked(3)?;
    if !code.bytes().all(|b| b.is_ascii_uppercase()) {
        return None;
    }
    let amount = rest.trim_start();
    crate::decimal::parse(amount).ok()?;
    Some((code, amount))
}

impl Imp {
    fn visit<'a, 'i>(&self, n: N<'a, 'i>) -> N<'a, 'i> {
        self.visited.borrow_mut().insert(n.id());
        n
    }

    fn matches(n: &N, ns: Ns, name: &str) -> bool {
        n.is_element() && n.tag_name().namespace() == Some(ns.uri()) && n.tag_name().name() == name
    }

    /// All matching children; each is marked as consumed.
    fn kids<'a, 'i>(&self, p: N<'a, 'i>, ns: Ns, name: &str) -> Vec<N<'a, 'i>> {
        p.children()
            .filter(|c| Self::matches(c, ns, name))
            .map(|c| self.visit(c))
            .collect()
    }

    /// All matching children, not marked; the caller marks the ones it consumes.
    fn kids_raw<'a, 'i>(&self, p: N<'a, 'i>, ns: Ns, name: &str) -> Vec<N<'a, 'i>> {
        p.children()
            .filter(|c| Self::matches(c, ns, name))
            .collect()
    }

    /// The first matching child (only it is marked, so a duplicate is reported as ignored).
    fn kid<'a, 'i>(&self, p: N<'a, 'i>, ns: Ns, name: &str) -> Option<N<'a, 'i>> {
        p.children()
            .find(|c| Self::matches(c, ns, name))
            .map(|c| self.visit(c))
    }

    fn text(&self, n: N) -> String {
        n.text().map(str::trim).unwrap_or_default().to_string()
    }

    /// Text of the first `cbc:<name>` child, empty when absent.
    fn tx(&self, p: N, name: &str) -> String {
        self.kid(p, Ns::Cbc, name)
            .map(|c| self.text(c))
            .unwrap_or_default()
    }

    /// Records that part of `n`'s text (`what`) was consumed but not stored.
    fn drop_text(&self, n: N, what: &'static str) {
        self.dropped.borrow_mut().push((n.id(), what));
    }

    fn attr(&self, n: N, name: &str) -> String {
        match n.attribute(name) {
            Some(v) => {
                self.attrs.borrow_mut().insert((n.id(), name.to_string()));
                v.trim().to_string()
            }
            None => String::new(),
        }
    }

    /// Marks `currencyID` as consumed (the exporter derives it) and returns the amount text.
    fn amount(&self, p: N, name: &str) -> String {
        match self.kid(p, Ns::Cbc, name) {
            Some(c) => {
                self.attr(c, "currencyID");
                self.text(c)
            }
            None => String::new(),
        }
    }

    /// `cbc:<name>` with a `schemeID`.
    fn ident(&self, p: N, name: &str) -> Option<pb::Identifier> {
        let c = self.kid(p, Ns::Cbc, name)?;
        nz(pb::Identifier {
            id: self.text(c),
            scheme_id: self.attr(c, "schemeID"),
        })
    }

    fn ident_node(&self, c: N) -> Option<pb::Identifier> {
        nz(pb::Identifier {
            id: self.text(c),
            scheme_id: self.attr(c, "schemeID"),
        })
    }

    // ------------------------------------------------------------------ parties

    fn address(&self, a: N) -> Option<pb::PostalAddress> {
        let line3 = self
            .kid(a, Ns::Cac, "AddressLine")
            .map(|l| self.tx(l, "Line"))
            .unwrap_or_default();
        let country = self
            .kid(a, Ns::Cac, "Country")
            .map(|c| self.tx(c, "IdentificationCode"))
            .unwrap_or_default();
        nz(pb::PostalAddress {
            line1: self.tx(a, "StreetName"),
            line2: self.tx(a, "AdditionalStreetName"),
            line3,
            city: self.tx(a, "CityName"),
            post_code: self.tx(a, "PostalZone"),
            country_subdivision: self.tx(a, "CountrySubentity"),
            country_code: country,
        })
    }

    fn tax_category(&self, c: N) -> Option<pb::TaxCategory> {
        let scheme = self
            .kid(c, Ns::Cac, "TaxScheme")
            .map(|s| self.tx(s, "ID"))
            .unwrap_or_default();
        nz(pb::TaxCategory {
            code: self.tx(c, "ID"),
            rate: self.tx(c, "Percent"),
            tax_scheme: scheme,
            exemption_reason_code: self.tx(c, "TaxExemptionReasonCode"),
            exemption_reason_text: self.tx(c, "TaxExemptionReason"),
        })
    }

    /// `cac:Party` of a seller or buyer. Returns the party and its VAT identifier.
    fn party(&self, p: N) -> (Option<pb::Party>, String) {
        let mut party = pb::Party::default();
        let mut vat = String::new();

        if let Some(e) = self.kid(p, Ns::Cbc, "EndpointID") {
            party.electronic_address = self.ident_node(e);
        }
        if let Some(pn) = self.kid(p, Ns::Cac, "PartyName") {
            party.trading_name = self.tx(pn, "Name");
        }
        for pi in self.kids(p, Ns::Cac, "PartyIdentification") {
            if let Some(i) = self.ident(pi, "ID") {
                party.identifiers.push(i);
            }
        }
        if let Some(a) = self.kid(p, Ns::Cac, "PostalAddress") {
            party.postal_address = self.address(a);
        }
        for pts in self.kids_raw(p, Ns::Cac, "PartyTaxScheme") {
            let scheme = pts
                .children()
                .find(|c| Self::matches(c, Ns::Cac, "TaxScheme"))
                .and_then(|s| {
                    s.children()
                        .find(|c| Self::matches(c, Ns::Cbc, "ID"))
                        .map(|i| self.text(i))
                })
                .unwrap_or_default();
            let target = match scheme.as_str() {
                "VAT" => &mut vat,
                "TIN" => &mut party.tax_registration_identifier,
                _ => continue, // not consumed: reported as ignored
            };
            if !target.is_empty() {
                continue; // a second VAT/TIN entry is reported as ignored
            }
            self.visit(pts);
            *target = self.tx(pts, "CompanyID");
            if let Some(s) = self.kid(pts, Ns::Cac, "TaxScheme") {
                self.tx(s, "ID");
            }
        }
        if let Some(le) = self.kid(p, Ns::Cac, "PartyLegalEntity") {
            party.name = self.tx(le, "RegistrationName");
            party.additional_legal_information = self.tx(le, "CompanyLegalForm");
            if let Some(c) = self.kid(le, Ns::Cbc, "CompanyID") {
                let kind = self.attr(c, "schemeAgencyID");
                let agency_name = self.attr(c, "schemeAgencyName");
                let mut lr = pb::LegalRegistration {
                    id: self.text(c),
                    scheme_id: self.attr(c, "schemeID"),
                    r#type: kind.clone(),
                    ..Default::default()
                };
                if kind == "PAS" {
                    lr.passport_issuing_country = agency_name;
                } else {
                    lr.authority_name = agency_name;
                }
                party.legal_registration = nz(lr);
            }
        }
        if let Some(c) = self.kid(p, Ns::Cac, "Contact") {
            party.contact = nz(pb::Contact {
                name: self.tx(c, "Name"),
                telephone: self.tx(c, "Telephone"),
                email: self.tx(c, "ElectronicMail"),
            });
        }
        (nz(party), vat)
    }

    /// `cac:PartyIdentification/cbc:ID` of `BuyerCustomerParty` / `SellerSupplierParty`.
    fn party_id_only(&self, wrapper: N) -> String {
        let Some(p) = self.kid(wrapper, Ns::Cac, "Party") else {
            return String::new();
        };
        let Some(pi) = self.kid(p, Ns::Cac, "PartyIdentification") else {
            return String::new();
        };
        self.tx(pi, "ID")
    }

    fn allowance_charge(&self, ac: N, with_tax: bool) -> pb::AllowanceCharge {
        let mut out = pb::AllowanceCharge {
            is_charge: self.tx(ac, "ChargeIndicator") == "true",
            amount: self.amount(ac, "Amount"),
            base_amount: self.amount(ac, "BaseAmount"),
            percentage: self.tx(ac, "MultiplierFactorNumeric"),
            reason: self.tx(ac, "AllowanceChargeReason"),
            reason_code: self.tx(ac, "AllowanceChargeReasonCode"),
            tax_category: None,
        };
        if with_tax && let Some(tc) = self.kid(ac, Ns::Cac, "TaxCategory") {
            out.tax_category = self.tax_category(tc);
        }
        out
    }

    // ------------------------------------------------------------------ lines

    fn classification(&self, code: N) -> pb::Classification {
        pb::Classification {
            code: self.text(code),
            scheme_id: self.attr(code, "listID"),
            scheme_version: self.attr(code, "listVersionID"),
        }
    }

    fn item(&self, it: N) -> Option<pb::Item> {
        let mut item = pb::Item {
            name: self.tx(it, "Name"),
            description: self.tx(it, "Description"),
            ..Default::default()
        };
        if let Some(b) = self.kid(it, Ns::Cac, "BuyersItemIdentification") {
            item.buyer_item_id = self.tx(b, "ID");
        }
        if let Some(s) = self.kid(it, Ns::Cac, "SellersItemIdentification") {
            item.seller_item_id = self.tx(s, "ID");
        }
        if let Some(s) = self.kid(it, Ns::Cac, "StandardItemIdentification") {
            item.standard_id = self.ident(s, "ID");
        }
        if let Some(o) = self.kid(it, Ns::Cac, "OriginCountry") {
            item.origin_country = self.tx(o, "IdentificationCode");
        }
        for cc in self.kids(it, Ns::Cac, "CommodityClassification") {
            let ty = self.tx(cc, "CommodityCode");
            if item.item_type.is_empty() {
                item.item_type = ty;
            }
            let nature = self.tx(cc, "NatureCode");
            if item.goods_service_type.is_empty() {
                item.goods_service_type = nature;
            }
            for code in self.kids(cc, Ns::Cbc, "ItemClassificationCode") {
                item.classifications.push(self.classification(code));
            }
        }
        for ai in self.kids_raw(it, Ns::Cac, "AdditionalItemIdentification") {
            let Some(id) = ai.children().find(|c| Self::matches(c, Ns::Cbc, "ID")) else {
                continue;
            };
            if id.attribute("schemeID") != Some("SAC") {
                continue; // not consumed: reported as ignored
            }
            self.visit(ai);
            self.visit(id);
            item.service_accounting_codes.push(pb::Classification {
                code: self.text(id),
                scheme_id: self.attr(id, "schemeID"),
                scheme_version: self.attr(id, "schemeVersionID"),
            });
        }
        for ap in self.kids(it, Ns::Cac, "AdditionalItemProperty") {
            let prop = pb::ItemAttribute {
                name: self.tx(ap, "Name"),
                value: self.tx(ap, "Value"),
            };
            if !prop.name.is_empty() {
                item.attributes.push(prop);
            }
        }
        nz(item)
    }

    /// BTAE-24, `cac:Item/cac:ItemInstance/cac:LotIdentification/cbc:LotNumberID`.
    fn batch_number(&self, it: N) -> String {
        let Some(inst) = self.kid(it, Ns::Cac, "ItemInstance") else {
            return String::new();
        };
        let Some(lot) = self.kid(inst, Ns::Cac, "LotIdentification") else {
            return String::new();
        };
        self.tx(lot, "LotNumberID")
    }

    fn period(&self, p: N) -> Option<pb::Period> {
        nz(pb::Period {
            start_date: self.tx(p, "StartDate"),
            end_date: self.tx(p, "EndDate"),
        })
    }

    fn line(&self, l: N) -> pb::InvoiceLine {
        let qty_name = if self.credit_note {
            "CreditedQuantity"
        } else {
            "InvoicedQuantity"
        };
        let mut out = pb::InvoiceLine {
            id: self.tx(l, "ID"),
            note: self.tx(l, "Note"),
            net_amount: self.amount(l, "LineExtensionAmount"),
            accounting_reference: self.tx(l, "AccountingCost"),
            ..Default::default()
        };
        if let Some(q) = self.kid(l, Ns::Cbc, qty_name) {
            out.quantity = self.text(q);
            out.unit_code = self.attr(q, "unitCode");
        }
        if let Some(p) = self.kid(l, Ns::Cac, "InvoicePeriod") {
            out.period = self.period(p);
        }
        if let Some(olr) = self.kid(l, Ns::Cac, "OrderLineReference") {
            out.order_line_reference = self.tx(olr, "LineID");
            if let Some(or) = self.kid(olr, Ns::Cac, "OrderReference") {
                out.order_reference = self.tx(or, "ID");
            }
            if out.order_line_reference == "NA" && !out.order_reference.is_empty() {
                out.order_line_reference.clear();
            }
        }
        if let Some(d) = self.kid(l, Ns::Cac, "DespatchLineReference") {
            self.tx(d, "LineID"); // always the placeholder `NA`
            if let Some(dr) = self.kid(d, Ns::Cac, "DocumentReference") {
                out.despatch_advice_reference = self.tx(dr, "ID");
            }
        }
        for dr in self.kids_raw(l, Ns::Cac, "DocumentReference") {
            let is_130 = dr
                .children()
                .find(|c| Self::matches(c, Ns::Cbc, "DocumentTypeCode"))
                .map(|c| self.text(c) == "130")
                .unwrap_or(false);
            if !is_130 || out.object_identifier.is_some() {
                continue; // not consumed: reported as ignored
            }
            self.visit(dr);
            self.tx(dr, "DocumentTypeCode");
            out.object_identifier = self.ident(dr, "ID");
        }
        for ac in self.kids(l, Ns::Cac, "AllowanceCharge") {
            out.allowances_charges
                .push(self.allowance_charge(ac, false));
        }
        if let Some(it) = self.kid(l, Ns::Cac, "Item") {
            out.batch_number = self.batch_number(it);
            if let Some(tc) = self.kid(it, Ns::Cac, "ClassifiedTaxCategory") {
                out.tax = self.tax_category(tc);
            }
            out.item = self.item(it);
        }
        if let Some(p) = self.kid(l, Ns::Cac, "Price") {
            let mut price = pb::Price {
                net_price: self.amount(p, "PriceAmount"),
                ..Default::default()
            };
            if let Some(bq) = self.kid(p, Ns::Cbc, "BaseQuantity") {
                price.base_quantity = self.text(bq);
                price.base_quantity_unit_code = self.attr(bq, "unitCode");
            }
            if let Some(ac) = self.kid(p, Ns::Cac, "AllowanceCharge") {
                self.tx(ac, "ChargeIndicator");
                price.discount = self.amount(ac, "Amount");
                price.gross_price = self.amount(ac, "BaseAmount");
            }
            out.price = nz(price);
        }
        if let Some(pe) = self.kid(l, Ns::Cac, "ItemPriceExtension") {
            out.amount_aed = self.amount(pe, "Amount");
            if let Some(tt) = self.kid(pe, Ns::Cac, "TaxTotal") {
                out.vat_amount_aed = self.amount(tt, "TaxAmount");
            }
        }
        out
    }

    // ------------------------------------------------------------------ document

    fn document(&self, root: N) -> pb::Invoice {
        let mut inv = pb::Invoice::default();
        let type_code = if self.credit_note {
            "CreditNoteTypeCode"
        } else {
            "InvoiceTypeCode"
        };

        let process = pb::ProcessControl {
            business_process_type: self.tx(root, "ProfileID"),
            specification_identifier: self.tx(root, "CustomizationID"),
        };
        inv.process = nz(process);
        inv.transaction_type_code = self.tx(root, "ProfileExecutionID");
        inv.invoice_number = self.tx(root, "ID");
        inv.uuid = self.tx(root, "UUID");
        inv.issue_date = self.tx(root, "IssueDate");
        inv.issue_time = self.tx(root, "IssueTime");
        if !self.credit_note {
            inv.payment_due_date = self.tx(root, "DueDate");
        }
        inv.invoice_type_code = self.tx(root, type_code);
        inv.note = self.tx(root, "Note");
        inv.tax_point_date = self.tx(root, "TaxPointDate");
        inv.currency = self.tx(root, "DocumentCurrencyCode");
        inv.tax_currency = self.tx(root, "TaxCurrencyCode");

        let mut refs = pb::DocumentReferences {
            buyer_accounting_reference: self.tx(root, "AccountingCost"),
            buyer_reference: self.tx(root, "BuyerReference"),
            ..Default::default()
        };

        if let Some(p) = self.kid(root, Ns::Cac, "InvoicePeriod") {
            let desc = self.tx(p, "DescriptionCode");
            inv.invoicing_period = self.period(p);
            inv.billing_frequency = desc;
        }
        if self.credit_note
            && let Some(dr) = self.kid(root, Ns::Cac, "DiscrepancyResponse")
        {
            inv.credit_note_reason_code = self.tx(dr, "ResponseCode");
        }
        if let Some(or) = self.kid(root, Ns::Cac, "OrderReference") {
            refs.purchase_order_reference = self.tx(or, "ID");
            refs.sales_order_reference = self.tx(or, "SalesOrderID");
            if refs.purchase_order_reference == "NA" && !refs.sales_order_reference.is_empty() {
                refs.purchase_order_reference.clear();
            }
        }
        for br in self.kids(root, Ns::Cac, "BillingReference") {
            if let Some(idr) = self.kid(br, Ns::Cac, "InvoiceDocumentReference") {
                let r = pb::PrecedingInvoiceReference {
                    id: self.tx(idr, "ID"),
                    issue_date: self.tx(idr, "IssueDate"),
                };
                if r != Default::default() {
                    inv.preceding_invoices.push(r);
                }
            }
        }
        for (name, slot) in [
            ("DespatchDocumentReference", 0),
            ("ReceiptDocumentReference", 1),
            ("StatementDocumentReference", 2),
            ("OriginatorDocumentReference", 3),
        ] {
            if let Some(d) = self.kid(root, Ns::Cac, name) {
                let id = self.tx(d, "ID");
                match slot {
                    0 => refs.despatch_advice_reference = id,
                    1 => refs.receiving_advice_reference = id,
                    2 => refs.customs_reference = id,
                    _ => refs.tender_or_lot_reference = id,
                }
            }
        }
        if let Some(c) = self.kid(root, Ns::Cac, "ContractDocumentReference") {
            refs.contract_reference = self.tx(c, "ID");
            if let Some(d) = self.kid(c, Ns::Cbc, "DocumentDescription") {
                let text = self.text(d);
                refs.contract_value = match split_currency_prefix(&text) {
                    Some((code, amount)) => {
                        if code != inv.currency {
                            self.drop_text(d, "currency");
                        }
                        amount.to_string()
                    }
                    None => text,
                };
            }
        }
        if !self.credit_note
            && let Some(p) = self.kid(root, Ns::Cac, "ProjectReference")
        {
            refs.project_reference = self.tx(p, "ID");
        }

        let mut total_with_tax_aed = String::new();
        for adr in self.kids_raw(root, Ns::Cac, "AdditionalDocumentReference") {
            let type_text = adr
                .children()
                .find(|c| Self::matches(c, Ns::Cbc, "DocumentTypeCode"))
                .map(|c| self.text(c))
                .unwrap_or_default();
            match type_text.as_str() {
                "130" if refs.invoiced_object.is_none() => {
                    self.visit(adr);
                    self.tx(adr, "DocumentTypeCode");
                    refs.invoiced_object = self.ident(adr, "ID");
                }
                "50" if self.credit_note && refs.project_reference.is_empty() => {
                    self.visit(adr);
                    self.tx(adr, "DocumentTypeCode");
                    refs.project_reference = self.tx(adr, "ID");
                }
                "aedtotal-incl-vat" if total_with_tax_aed.is_empty() => {
                    self.visit(adr);
                    self.tx(adr, "DocumentTypeCode");
                    self.tx(adr, "ID"); // the literal `AED`
                    let d = self.tx(adr, "DocumentDescription");
                    total_with_tax_aed = d.strip_prefix("AED").unwrap_or(&d).trim().to_string();
                }
                "" => {
                    self.visit(adr);
                    let mut sd = pb::SupportingDocument {
                        reference: self.tx(adr, "ID"),
                        description: self.tx(adr, "DocumentDescription"),
                        ..Default::default()
                    };
                    if let Some(att) = self.kid(adr, Ns::Cac, "Attachment") {
                        if let Some(er) = self.kid(att, Ns::Cac, "ExternalReference") {
                            sd.external_uri = self.tx(er, "URI");
                        }
                        if let Some(b) = self.kid(att, Ns::Cbc, "EmbeddedDocumentBinaryObject") {
                            sd.attachment = nz(pb::Attachment {
                                object_key: String::new(),
                                mime_code: self.attr(b, "mimeCode"),
                                filename: self.attr(b, "filename"),
                            });
                        }
                    }
                    inv.supporting_documents.push(sd);
                }
                _ => {} // another document type: reported as ignored
            }
        }
        inv.references = nz(refs);

        let mut seller_vat = String::new();
        if let Some(w) = self.kid(root, Ns::Cac, "AccountingSupplierParty")
            && let Some(p) = self.kid(w, Ns::Cac, "Party")
        {
            let (party, vat) = self.party(p);
            inv.seller = party;
            seller_vat = vat;
        }
        inv.seller_trn = seller_vat;
        if let Some(w) = self.kid(root, Ns::Cac, "AccountingCustomerParty")
            && let Some(p) = self.kid(w, Ns::Cac, "Party")
        {
            let (party, vat) = self.party(p);
            inv.buyer = party;
            inv.buyer_trn = vat;
        }
        if let Some(w) = self.kid(root, Ns::Cac, "PayeeParty") {
            let mut payee = pb::Payee::default();
            if let Some(pn) = self.kid(w, Ns::Cac, "PartyName") {
                payee.name = self.tx(pn, "Name");
            }
            if let Some(pi) = self.kid(w, Ns::Cac, "PartyIdentification") {
                payee.identifier = self.ident(pi, "ID");
            }
            if let Some(le) = self.kid(w, Ns::Cac, "PartyLegalEntity")
                && let Some(c) = self.kid(le, Ns::Cbc, "CompanyID")
            {
                payee.legal_registration = self.ident_node(c);
            }
            inv.payee = nz(payee);
        }
        if let Some(w) = self.kid(root, Ns::Cac, "BuyerCustomerParty") {
            inv.beneficiary_id = self.party_id_only(w);
        }
        if let Some(w) = self.kid(root, Ns::Cac, "SellerSupplierParty") {
            inv.principal_id = self.party_id_only(w);
        }
        if let Some(w) = self.kid(root, Ns::Cac, "TaxRepresentativeParty") {
            let mut tr = pb::TaxRepresentative::default();
            if let Some(pn) = self.kid(w, Ns::Cac, "PartyName") {
                tr.name = self.tx(pn, "Name");
            }
            if let Some(a) = self.kid(w, Ns::Cac, "PostalAddress") {
                tr.postal_address = self.address(a);
            }
            if let Some(pts) = self.kid(w, Ns::Cac, "PartyTaxScheme") {
                tr.vat_identifier = self.tx(pts, "CompanyID");
                if let Some(s) = self.kid(pts, Ns::Cac, "TaxScheme") {
                    self.tx(s, "ID");
                }
            }
            inv.tax_representative = nz(tr);
        }
        if let Some(d) = self.kid(root, Ns::Cac, "Delivery") {
            let mut del = pb::Delivery {
                actual_delivery_date: self.tx(d, "ActualDeliveryDate"),
                ..Default::default()
            };
            if let Some(loc) = self.kid(d, Ns::Cac, "DeliveryLocation") {
                del.location = self.ident(loc, "ID");
                if let Some(a) = self.kid(loc, Ns::Cac, "Address") {
                    del.address = self.address(a);
                }
            }
            if let Some(dp) = self.kid(d, Ns::Cac, "DeliveryParty")
                && let Some(pn) = self.kid(dp, Ns::Cac, "PartyName")
            {
                del.party_name = self.tx(pn, "Name");
            }
            if let Some(dt) = self.kid(d, Ns::Cac, "DeliveryTerms")
                && let Some(id) = self.kid(dt, Ns::Cbc, "ID")
            {
                del.incoterms = self.text(id);
                self.attr(id, "schemeID");
            }
            inv.delivery = nz(del);
        }

        for (idx, pm) in self
            .kids(root, Ns::Cac, "PaymentMeans")
            .into_iter()
            .enumerate()
        {
            if self.credit_note && idx == 0 {
                inv.payment_due_date = self.tx(pm, "PaymentDueDate");
            }
            inv.payment_instructions.push(self.payment_means(pm));
        }
        for pt in self.kids(root, Ns::Cac, "PaymentTerms") {
            inv.payment_terms.push(pb::PaymentTerms {
                instructions_id: self.tx(pt, "PaymentMeansID"),
                note: self.tx(pt, "Note"),
                amount: self.amount(pt, "Amount"),
                installment_due_date: self.tx(pt, "InstallmentDueDate"),
            });
        }
        for ac in self.kids(root, Ns::Cac, "AllowanceCharge") {
            inv.allowances_charges.push(self.allowance_charge(ac, true));
        }
        if let Some(x) = self.kid(root, Ns::Cac, "TaxExchangeRate") {
            inv.exchange_rate = self.tx(x, "CalculationRate");
            self.tx(x, "SourceCurrencyCode");
            self.tx(x, "TargetCurrencyCode");
        }

        let mut totals = pb::DocumentTotals {
            total_with_tax_aed,
            ..Default::default()
        };
        for tt in self.kids(root, Ns::Cac, "TaxTotal") {
            let cur = self
                .kid(tt, Ns::Cbc, "TaxAmount")
                .map(|a| (self.attr(a, "currencyID"), self.text(a)))
                .unwrap_or_default();
            let is_doc = cur.0 == inv.currency || cur.0.is_empty();
            if is_doc {
                inv.vat_amount = cur.1;
                if self.tx(tt, "TaxIncludedIndicator") == "true" {
                    totals.tax_inclusive_pricing = true;
                }
                for st in self.kids(tt, Ns::Cac, "TaxSubtotal") {
                    let mut sub = pb::TaxSubtotal {
                        taxable_amount: self.amount(st, "TaxableAmount"),
                        tax_amount: self.amount(st, "TaxAmount"),
                        category: None,
                    };
                    if let Some(tc) = self.kid(st, Ns::Cac, "TaxCategory") {
                        sub.category = self.tax_category(tc);
                    }
                    inv.tax_breakdown.push(sub);
                }
            } else {
                totals.tax_amount_accounting_currency = cur.1;
            }
        }
        if let Some(m) = self.kid(root, Ns::Cac, "LegalMonetaryTotal") {
            totals.line_extension_amount = self.amount(m, "LineExtensionAmount");
            totals.allowance_total_amount = self.amount(m, "AllowanceTotalAmount");
            totals.charge_total_amount = self.amount(m, "ChargeTotalAmount");
            totals.tax_exclusive_amount = self.amount(m, "TaxExclusiveAmount");
            inv.total_amount = self.amount(m, "TaxInclusiveAmount");
            totals.paid_amount = self.amount(m, "PrepaidAmount");
            totals.rounding_amount = self.amount(m, "PayableRoundingAmount");
            totals.payable_amount = self.amount(m, "PayableAmount");
        }
        inv.totals = nz(totals);

        let line_name = if self.credit_note {
            "CreditNoteLine"
        } else {
            "InvoiceLine"
        };
        for l in self.kids(root, Ns::Cac, line_name) {
            inv.lines.push(self.line(l));
        }
        inv
    }

    fn payment_means(&self, pm: N) -> pb::PaymentInstructions {
        let mut out = pb::PaymentInstructions {
            id: self.tx(pm, "ID"),
            ..Default::default()
        };
        if let Some(c) = self.kid(pm, Ns::Cbc, "PaymentMeansCode") {
            out.means_code = self.text(c);
            out.means_text = self.attr(c, "name");
        }
        for pid in self.kids(pm, Ns::Cbc, "PaymentID") {
            if let Some(i) = self.ident_node(pid) {
                out.remittance_information.push(i);
            }
        }
        if let Some(c) = self.kid(pm, Ns::Cac, "CardAccount") {
            let mut network = self.tx(c, "NetworkID");
            if network == "NA" {
                network.clear();
            }
            out.card = nz(pb::PaymentCard {
                primary_account_number: self.tx(c, "PrimaryAccountNumberID"),
                holder_name: self.tx(c, "HolderName"),
                network_id: network,
            });
        }
        if let Some(a) = self.kid(pm, Ns::Cac, "PayeeFinancialAccount") {
            let mut ct = pb::CreditTransfer {
                account: self.ident(a, "ID"),
                account_name: self.tx(a, "Name"),
                ..Default::default()
            };
            if let Some(b) = self.kid(a, Ns::Cac, "FinancialInstitutionBranch") {
                ct.service_provider_id = self.tx(b, "ID");
                // IBG-34 (IBT-169..IBT-175), the context of `ibr-sr-59`.
                if let Some(addr) = self.kid(b, Ns::Cac, "Address") {
                    ct.institution_address = self.address(addr);
                }
            }
            out.credit_transfer = nz(ct);
        }
        if let Some(m) = self.kid(pm, Ns::Cac, "PaymentMandate") {
            let mut dd = pb::DirectDebit {
                mandate_reference: self.tx(m, "ID"),
                ..Default::default()
            };
            if let Some(a) = self.kid(m, Ns::Cac, "PayerFinancialAccount") {
                dd.debited_account = self.tx(a, "ID");
            }
            out.direct_debit = nz(dd);
        }
        out
    }

    // ------------------------------------------------------------------ report

    fn path(n: N) -> String {
        let mut parts: Vec<String> = n
            .ancestors()
            .filter(|a| a.is_element())
            .map(|a| match a.tag_name().namespace() {
                Some(NS_CAC) => format!("cac:{}", a.tag_name().name()),
                Some(NS_CBC) => format!("cbc:{}", a.tag_name().name()),
                _ => a.tag_name().name().to_string(),
            })
            .collect();
        parts.reverse();
        parts.join("/")
    }

    fn report(&self, root: N) -> ImportReport {
        let mut ignored = Vec::new();
        self.walk(root, &mut ignored);
        ImportReport { ignored }
    }

    /// Document order: a node's attributes, the parts of its text that were dropped, then each
    /// child (reported, or descended into).
    fn walk(&self, n: N, out: &mut Vec<String>) {
        let attrs = self.attrs.borrow();
        for a in n.attributes() {
            if a.namespace() == Some(NS_XSI) {
                continue;
            }
            if !attrs.contains(&(n.id(), a.name().to_string())) {
                out.push(format!("{}@{}", Self::path(n), a.name()));
            }
        }
        drop(attrs);
        for (_, what) in self.dropped.borrow().iter().filter(|(id, _)| *id == n.id()) {
            out.push(format!("{}#{what}", Self::path(n)));
        }
        for c in n.children().filter(|c| c.is_element()) {
            if self.visited.borrow().contains(&c.id()) {
                self.walk(c, out);
            } else {
                out.push(Self::path(c));
            }
        }
    }
}

/// Imports a UBL 2.1 Invoice or CreditNote (PINT AE shape) as a canonical invoice.
pub fn from_xml(xml: &str) -> Result<(pb::Invoice, ImportReport), ImportError> {
    let doc = Document::parse(xml).map_err(|e| ImportError(e.to_string()))?;
    let root = doc.root_element();
    let credit_note = match (root.tag_name().namespace(), root.tag_name().name()) {
        (Some(NS_INVOICE), "Invoice") => false,
        (Some(NS_CREDIT_NOTE), "CreditNote") => true,
        (ns, name) => {
            return Err(ImportError(format!(
                "root element {{{}}}{name} is not a UBL 2.1 Invoice or CreditNote",
                ns.unwrap_or("")
            )));
        }
    };
    let imp = Imp {
        visited: RefCell::new(HashSet::new()),
        attrs: RefCell::new(HashSet::new()),
        dropped: RefCell::new(Vec::new()),
        credit_note,
    };
    imp.visit(root);
    let inv = imp.document(root);
    let report = imp.report(root);
    Ok((inv, report))
}

#[cfg(test)]
mod tests {
    use super::*;

    const MIN: &str = r#"<?xml version="1.0"?>
<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"
 xmlns:cac="urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2"
 xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">
 <cbc:ID> INV-1 </cbc:ID>
 <cbc:DocumentCurrencyCode>AED</cbc:DocumentCurrencyCode>
 <cac:AccountingSupplierParty><cac:Party>
  <cac:PartyTaxScheme><cbc:CompanyID>100000000000003</cbc:CompanyID>
   <cac:TaxScheme><cbc:ID>VAT</cbc:ID></cac:TaxScheme></cac:PartyTaxScheme>
  <cac:PartyLegalEntity><cbc:RegistrationName>Acme</cbc:RegistrationName>
   <cbc:CompanyID schemeAgencyID="PAS" schemeAgencyName="IN">P1</cbc:CompanyID></cac:PartyLegalEntity>
 </cac:Party></cac:AccountingSupplierParty>
 <cac:TaxTotal><cbc:TaxAmount currencyID="AED">5.00</cbc:TaxAmount></cac:TaxTotal>
 <cac:LegalMonetaryTotal><cbc:TaxInclusiveAmount currencyID="AED">105.00</cbc:TaxInclusiveAmount></cac:LegalMonetaryTotal>
 <cac:InvoiceLine><cbc:ID>1</cbc:ID><cbc:InvoicedQuantity unitCode="H87">2</cbc:InvoicedQuantity>
  <cac:Item><cbc:Name>x</cbc:Name><cac:ClassifiedTaxCategory><cbc:ID>S</cbc:ID><cbc:Percent>5</cbc:Percent>
   <cac:TaxScheme><cbc:ID>VAT</cbc:ID></cac:TaxScheme></cac:ClassifiedTaxCategory></cac:Item>
 </cac:InvoiceLine>
</Invoice>"#;

    #[test]
    fn imports_a_minimal_invoice() {
        let (inv, rep) = from_xml(MIN).unwrap();
        assert!(rep.ignored.is_empty(), "{:?}", rep.ignored);
        assert_eq!(inv.invoice_number, "INV-1");
        assert_eq!(inv.currency, "AED");
        assert_eq!(inv.seller_trn, "100000000000003");
        assert_eq!(inv.vat_amount, "5.00");
        assert_eq!(inv.total_amount, "105.00");
        let seller = inv.seller.unwrap();
        assert_eq!(seller.name, "Acme");
        let lr = seller.legal_registration.unwrap();
        assert_eq!(lr.r#type, "PAS");
        assert_eq!(lr.passport_issuing_country, "IN");
        assert_eq!(lr.authority_name, "");
        assert_eq!(inv.lines.len(), 1);
        assert_eq!(inv.lines[0].quantity, "2");
        assert_eq!(inv.lines[0].unit_code, "H87");
        assert_eq!(inv.lines[0].tax.as_ref().unwrap().rate, "5");
    }

    #[test]
    fn reports_unconsumed_elements_and_attributes() {
        let xml = MIN.replace(
            "<cbc:ID> INV-1 </cbc:ID>",
            "<cbc:ID foo=\"1\"> INV-1 </cbc:ID><cbc:Bogus>x</cbc:Bogus>",
        );
        let (_, rep) = from_xml(&xml).unwrap();
        assert_eq!(rep.ignored, ["Invoice/cbc:ID@foo", "Invoice/cbc:Bogus"]);
    }

    fn with_contract_value(text: &str) -> String {
        MIN.replace(
            "<cbc:DocumentCurrencyCode>",
            &format!(
                "<cac:ContractDocumentReference><cbc:ID>C-1</cbc:ID>\
                 <cbc:DocumentDescription>{text}</cbc:DocumentDescription>\
                 </cac:ContractDocumentReference><cbc:DocumentCurrencyCode>"
            ),
        )
    }

    /// BTAE-05 is a bare decimal string (CI rule 10); the official examples write a currency
    /// prefix (`AED200000`, `AED 1000000`), which the importer strips.
    #[test]
    fn contract_value_drops_a_currency_prefix_of_the_document_currency() {
        for (text, want) in [
            ("AED200000", "200000"),
            ("AED 1000000", "1000000"),
            (" AED\t1500.50 ", "1500.50"),
            ("200000", "200000"),
            ("-12.5", "-12.5"),
        ] {
            let (inv, rep) = from_xml(&with_contract_value(text)).unwrap();
            let refs = inv.references.unwrap();
            assert_eq!(refs.contract_reference, "C-1");
            assert_eq!(refs.contract_value, want, "{text:?}");
            assert!(rep.ignored.is_empty(), "{text:?}: {:?}", rep.ignored);
        }
    }

    /// A prefix naming another currency carries information the canonical model has no field
    /// for: the amount is kept, the currency is reported as dropped.
    #[test]
    fn contract_value_reports_a_foreign_currency_prefix_as_ignored() {
        let (inv, rep) = from_xml(&with_contract_value("USD 5000")).unwrap();
        assert_eq!(inv.references.unwrap().contract_value, "5000");
        assert_eq!(
            rep.ignored,
            ["Invoice/cac:ContractDocumentReference/cbc:DocumentDescription#currency"]
        );
    }

    /// Text that is not `[currency] decimal` stays verbatim, so `AE-FMT-001` reports it rather
    /// than the importer guessing.
    #[test]
    fn contract_value_without_a_decimal_amount_is_kept_verbatim() {
        for text in [
            "AED",
            "AED two hundred",
            "aed 5",
            "AED 1,000",
            "AEDX 5",
            "5 AED",
        ] {
            let (inv, rep) = from_xml(&with_contract_value(text)).unwrap();
            assert_eq!(inv.references.unwrap().contract_value, text, "{text:?}");
            assert!(rep.ignored.is_empty(), "{text:?}: {:?}", rep.ignored);
        }
    }

    /// What the exporter does not write by design: supporting-document attachments (spec 2,
    /// non-goals), IBT-090 (no binding), the `@schemeAgencyName` qualifier the legal
    /// registration type does not select, and the tax category of a line allowance (the model's
    /// comment: "document level only").
    fn exportable_part(mut x: pb::Invoice) -> pb::Invoice {
        for d in &mut x.supporting_documents {
            d.attachment = None;
        }
        for pi in &mut x.payment_instructions {
            if let Some(dd) = &mut pi.direct_debit {
                dd.creditor_identifier.clear();
            }
        }
        for p in [&mut x.seller, &mut x.buyer].into_iter().flatten() {
            if let Some(lr) = &mut p.legal_registration {
                if lr.r#type == "PAS" {
                    lr.authority_name.clear();
                } else {
                    lr.passport_issuing_country.clear();
                }
            }
        }
        for l in &mut x.lines {
            for a in &mut l.allowances_charges {
                a.tax_category = None;
            }
        }
        x
    }

    /// The deferred Task 5 round trip (spec 5.3.4): for each imported example `x`,
    /// `from_xml(to_xml(Doc::new(&x))) == x` up to what is not exported by design, and the
    /// importer consumes everything the exporter writes.
    #[test]
    fn round_trip_of_every_example() {
        let mut failures = Vec::new();
        for (slug, x) in crate::conformance::examples() {
            let xml = crate::export::testing::export_str(&x);
            let (y, rep) = from_xml(&xml).unwrap_or_else(|e| panic!("{slug}: {e}"));
            if !rep.ignored.is_empty() {
                failures.push(format!("{slug}: ignored {:?}", rep.ignored));
            }
            if y != exportable_part(x.clone()) {
                failures.push(format!("{slug}: round trip differs"));
            }
        }
        assert!(failures.is_empty(), "{}", failures.join("\n"));
    }

    /// Every field of the model set, both kinds and both legal-registration qualifiers: every
    /// binding of the exporter is read back by the importer to the same field.
    #[test]
    fn round_trip_of_maximal_documents() {
        for code in ["380", "480", "381", "81"] {
            for lr_type in ["TL", "PAS"] {
                let mut x = crate::export::testing::maximal(code);
                for p in [&mut x.seller, &mut x.buyer].into_iter().flatten() {
                    p.legal_registration.as_mut().unwrap().r#type = lr_type.into();
                }
                let xml = crate::export::testing::export_str(&x);
                let (y, rep) = from_xml(&xml).unwrap();
                assert_eq!(rep.ignored, Vec::<String>::new(), "{code} {lr_type}");
                let want = exportable_part(x);
                assert_eq!(
                    crate::canonical_json::to_canonical_json_pretty(&y),
                    crate::canonical_json::to_canonical_json_pretty(&want),
                    "{code} {lr_type}"
                );
            }
        }
    }

    #[test]
    fn rejects_non_ubl_roots_and_bad_xml() {
        assert!(from_xml("<a/>").is_err());
        assert!(from_xml("<Invoice").is_err());
        let wrong_ns = MIN.replace("Invoice-2", "Invoice-3");
        assert!(from_xml(&wrong_ns).is_err());
    }
}
