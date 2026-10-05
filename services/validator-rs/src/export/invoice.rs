//! The `Invoice` root (type codes other than `381` and `81`) in `UBL-Invoice-2.1.xsd` order
//! (CI "Invoice vs CreditNote serialisation"):
//!
//! `CustomizationID, ProfileID, ProfileExecutionID, ID, UUID, IssueDate, IssueTime, DueDate,
//! InvoiceTypeCode, Note, TaxPointDate, DocumentCurrencyCode, TaxCurrencyCode, AccountingCost,
//! BuyerReference, InvoicePeriod, OrderReference, BillingReference*, DespatchDocumentReference,
//! ReceiptDocumentReference, StatementDocumentReference, OriginatorDocumentReference,
//! ContractDocumentReference, AdditionalDocumentReference*, ProjectReference,
//! AccountingSupplierParty, AccountingCustomerParty, PayeeParty, BuyerCustomerParty,
//! SellerSupplierParty, TaxRepresentativeParty, Delivery, PaymentMeans*, PaymentTerms*,
//! AllowanceCharge*, TaxExchangeRate, TaxTotal*, LegalMonetaryTotal, InvoiceLine+`.

use super::writer::El;
use super::{B, NS_CAC, NS_CBC, NS_INVOICE, id_text_agg, t};
use crate::doc::Doc;

/// The element tree of an invoice.
pub fn build<'a>(doc: &Doc<'a>) -> El<'a> {
    let b = B::new(doc);
    let inv = doc.inv;
    let mut root = El::new("Invoice")
        .attr("xmlns", NS_INVOICE)
        .attr("xmlns:cac", NS_CAC)
        .attr("xmlns:cbc", NS_CBC);
    b.head(&mut root);
    root.push(t("cbc:DueDate", &inv.payment_due_date));
    root.push(t("cbc:InvoiceTypeCode", &inv.invoice_type_code));
    root.push(t("cbc:Note", &inv.note));
    root.push(t("cbc:TaxPointDate", &inv.tax_point_date));
    b.currencies_to_period(&mut root);
    b.order_and_billing(&mut root);
    root.push(b.despatch_ref());
    root.push(b.receipt_ref());
    root.push(b.statement_ref());
    root.push(b.originator_ref());
    root.push(b.contract_ref());
    b.additional_refs(&mut root);
    root.push(id_text_agg(
        "cac:ProjectReference",
        b.r(|r| &r.project_reference),
    ));
    root.push(b.supplier());
    root.push(b.customer());
    b.other_parties(&mut root);
    root.push(b.delivery());
    b.payment_means(&mut root);
    b.payment_terms(&mut root);
    b.allowances_charges(&mut root);
    root.push(b.tax_exchange_rate());
    b.tax_totals(&mut root);
    root.push(b.legal_monetary_total());
    b.lines(&mut root);
    root
}
