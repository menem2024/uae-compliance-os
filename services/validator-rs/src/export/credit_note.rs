//! The `CreditNote` root (type codes `381` and `81`, CI rule 8) in `UBL-CreditNote-2.1.xsd`
//! order (CI "Invoice vs CreditNote serialisation"):
//!
//! `CustomizationID, ProfileID, ProfileExecutionID, ID, UUID, IssueDate, IssueTime,
//! TaxPointDate, CreditNoteTypeCode, Note, DocumentCurrencyCode, TaxCurrencyCode,
//! AccountingCost, BuyerReference, InvoicePeriod, DiscrepancyResponse, OrderReference,
//! BillingReference*, DespatchDocumentReference, ReceiptDocumentReference,
//! ContractDocumentReference, AdditionalDocumentReference*, StatementDocumentReference,
//! OriginatorDocumentReference, AccountingSupplierParty, AccountingCustomerParty, PayeeParty,
//! BuyerCustomerParty, SellerSupplierParty, TaxRepresentativeParty, Delivery, PaymentMeans*,
//! PaymentTerms*, TaxExchangeRate, AllowanceCharge*, TaxTotal*, LegalMonetaryTotal,
//! CreditNoteLine+`.
//!
//! Differences from the invoice: no root `DueDate` (IBT-009 is `PaymentMeans/PaymentDueDate`),
//! no `ProjectReference` (IBT-011 is `AdditionalDocumentReference` type `50`), BTAE-03 in
//! `DiscrepancyResponse/ResponseCode`, `TaxPointDate` before the type code,
//! `StatementDocumentReference` and `OriginatorDocumentReference` after the additional
//! references, `AllowanceCharge` after `TaxExchangeRate`, `CreditNoteLine`/`CreditedQuantity`.

use super::writer::El;
use super::{B, NS_CAC, NS_CBC, NS_CREDIT_NOTE, t};
use crate::doc::Doc;

/// The element tree of a credit note.
pub fn build<'a>(doc: &Doc<'a>) -> El<'a> {
    let b = B::new(doc);
    let inv = doc.inv;
    let mut root = El::new("CreditNote")
        .attr("xmlns", NS_CREDIT_NOTE)
        .attr("xmlns:cac", NS_CAC)
        .attr("xmlns:cbc", NS_CBC);
    b.head(&mut root);
    root.push(t("cbc:TaxPointDate", &inv.tax_point_date));
    root.push(t("cbc:CreditNoteTypeCode", &inv.invoice_type_code));
    root.push(t("cbc:Note", &inv.note));
    b.currencies_to_period(&mut root);
    root.push(
        t("cbc:ResponseCode", &inv.credit_note_reason_code)
            .map(|c| El::new("cac:DiscrepancyResponse").with(c)),
    );
    b.order_and_billing(&mut root);
    root.push(b.despatch_ref());
    root.push(b.receipt_ref());
    root.push(b.contract_ref());
    b.additional_refs(&mut root);
    root.push(b.statement_ref());
    root.push(b.originator_ref());
    root.push(b.supplier());
    root.push(b.customer());
    b.other_parties(&mut root);
    root.push(b.delivery());
    b.payment_means(&mut root);
    b.payment_terms(&mut root);
    root.push(b.tax_exchange_rate());
    b.allowances_charges(&mut root);
    b.tax_totals(&mut root);
    root.push(b.legal_monetary_total());
    b.lines(&mut root);
    root
}
