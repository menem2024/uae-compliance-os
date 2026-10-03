from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Invoice(_message.Message):
    __slots__ = ("invoice_number", "issue_date", "seller_trn", "buyer_trn", "currency", "total_amount", "vat_amount", "uuid", "issue_time", "invoice_type_code", "transaction_type_code", "tax_currency", "exchange_rate", "tax_point_date", "payment_due_date", "note", "credit_note_reason_code", "process", "references", "preceding_invoices", "seller", "buyer", "principal_id", "beneficiary_id", "payee", "tax_representative", "delivery", "invoicing_period", "billing_frequency", "payment_instructions", "payment_terms", "allowances_charges", "totals", "tax_breakdown", "supporting_documents", "lines")
    INVOICE_NUMBER_FIELD_NUMBER: _ClassVar[int]
    ISSUE_DATE_FIELD_NUMBER: _ClassVar[int]
    SELLER_TRN_FIELD_NUMBER: _ClassVar[int]
    BUYER_TRN_FIELD_NUMBER: _ClassVar[int]
    CURRENCY_FIELD_NUMBER: _ClassVar[int]
    TOTAL_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    VAT_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    UUID_FIELD_NUMBER: _ClassVar[int]
    ISSUE_TIME_FIELD_NUMBER: _ClassVar[int]
    INVOICE_TYPE_CODE_FIELD_NUMBER: _ClassVar[int]
    TRANSACTION_TYPE_CODE_FIELD_NUMBER: _ClassVar[int]
    TAX_CURRENCY_FIELD_NUMBER: _ClassVar[int]
    EXCHANGE_RATE_FIELD_NUMBER: _ClassVar[int]
    TAX_POINT_DATE_FIELD_NUMBER: _ClassVar[int]
    PAYMENT_DUE_DATE_FIELD_NUMBER: _ClassVar[int]
    NOTE_FIELD_NUMBER: _ClassVar[int]
    CREDIT_NOTE_REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    PROCESS_FIELD_NUMBER: _ClassVar[int]
    REFERENCES_FIELD_NUMBER: _ClassVar[int]
    PRECEDING_INVOICES_FIELD_NUMBER: _ClassVar[int]
    SELLER_FIELD_NUMBER: _ClassVar[int]
    BUYER_FIELD_NUMBER: _ClassVar[int]
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    BENEFICIARY_ID_FIELD_NUMBER: _ClassVar[int]
    PAYEE_FIELD_NUMBER: _ClassVar[int]
    TAX_REPRESENTATIVE_FIELD_NUMBER: _ClassVar[int]
    DELIVERY_FIELD_NUMBER: _ClassVar[int]
    INVOICING_PERIOD_FIELD_NUMBER: _ClassVar[int]
    BILLING_FREQUENCY_FIELD_NUMBER: _ClassVar[int]
    PAYMENT_INSTRUCTIONS_FIELD_NUMBER: _ClassVar[int]
    PAYMENT_TERMS_FIELD_NUMBER: _ClassVar[int]
    ALLOWANCES_CHARGES_FIELD_NUMBER: _ClassVar[int]
    TOTALS_FIELD_NUMBER: _ClassVar[int]
    TAX_BREAKDOWN_FIELD_NUMBER: _ClassVar[int]
    SUPPORTING_DOCUMENTS_FIELD_NUMBER: _ClassVar[int]
    LINES_FIELD_NUMBER: _ClassVar[int]
    invoice_number: str
    issue_date: str
    seller_trn: str
    buyer_trn: str
    currency: str
    total_amount: str
    vat_amount: str
    uuid: str
    issue_time: str
    invoice_type_code: str
    transaction_type_code: str
    tax_currency: str
    exchange_rate: str
    tax_point_date: str
    payment_due_date: str
    note: str
    credit_note_reason_code: str
    process: ProcessControl
    references: DocumentReferences
    preceding_invoices: _containers.RepeatedCompositeFieldContainer[PrecedingInvoiceReference]
    seller: Party
    buyer: Party
    principal_id: str
    beneficiary_id: str
    payee: Payee
    tax_representative: TaxRepresentative
    delivery: Delivery
    invoicing_period: Period
    billing_frequency: str
    payment_instructions: _containers.RepeatedCompositeFieldContainer[PaymentInstructions]
    payment_terms: _containers.RepeatedCompositeFieldContainer[PaymentTerms]
    allowances_charges: _containers.RepeatedCompositeFieldContainer[AllowanceCharge]
    totals: DocumentTotals
    tax_breakdown: _containers.RepeatedCompositeFieldContainer[TaxSubtotal]
    supporting_documents: _containers.RepeatedCompositeFieldContainer[SupportingDocument]
    lines: _containers.RepeatedCompositeFieldContainer[InvoiceLine]
    def __init__(self, invoice_number: _Optional[str] = ..., issue_date: _Optional[str] = ..., seller_trn: _Optional[str] = ..., buyer_trn: _Optional[str] = ..., currency: _Optional[str] = ..., total_amount: _Optional[str] = ..., vat_amount: _Optional[str] = ..., uuid: _Optional[str] = ..., issue_time: _Optional[str] = ..., invoice_type_code: _Optional[str] = ..., transaction_type_code: _Optional[str] = ..., tax_currency: _Optional[str] = ..., exchange_rate: _Optional[str] = ..., tax_point_date: _Optional[str] = ..., payment_due_date: _Optional[str] = ..., note: _Optional[str] = ..., credit_note_reason_code: _Optional[str] = ..., process: _Optional[_Union[ProcessControl, _Mapping]] = ..., references: _Optional[_Union[DocumentReferences, _Mapping]] = ..., preceding_invoices: _Optional[_Iterable[_Union[PrecedingInvoiceReference, _Mapping]]] = ..., seller: _Optional[_Union[Party, _Mapping]] = ..., buyer: _Optional[_Union[Party, _Mapping]] = ..., principal_id: _Optional[str] = ..., beneficiary_id: _Optional[str] = ..., payee: _Optional[_Union[Payee, _Mapping]] = ..., tax_representative: _Optional[_Union[TaxRepresentative, _Mapping]] = ..., delivery: _Optional[_Union[Delivery, _Mapping]] = ..., invoicing_period: _Optional[_Union[Period, _Mapping]] = ..., billing_frequency: _Optional[str] = ..., payment_instructions: _Optional[_Iterable[_Union[PaymentInstructions, _Mapping]]] = ..., payment_terms: _Optional[_Iterable[_Union[PaymentTerms, _Mapping]]] = ..., allowances_charges: _Optional[_Iterable[_Union[AllowanceCharge, _Mapping]]] = ..., totals: _Optional[_Union[DocumentTotals, _Mapping]] = ..., tax_breakdown: _Optional[_Iterable[_Union[TaxSubtotal, _Mapping]]] = ..., supporting_documents: _Optional[_Iterable[_Union[SupportingDocument, _Mapping]]] = ..., lines: _Optional[_Iterable[_Union[InvoiceLine, _Mapping]]] = ...) -> None: ...

class ProcessControl(_message.Message):
    __slots__ = ("business_process_type", "specification_identifier")
    BUSINESS_PROCESS_TYPE_FIELD_NUMBER: _ClassVar[int]
    SPECIFICATION_IDENTIFIER_FIELD_NUMBER: _ClassVar[int]
    business_process_type: str
    specification_identifier: str
    def __init__(self, business_process_type: _Optional[str] = ..., specification_identifier: _Optional[str] = ...) -> None: ...

class Identifier(_message.Message):
    __slots__ = ("id", "scheme_id")
    ID_FIELD_NUMBER: _ClassVar[int]
    SCHEME_ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    scheme_id: str
    def __init__(self, id: _Optional[str] = ..., scheme_id: _Optional[str] = ...) -> None: ...

class DocumentReferences(_message.Message):
    __slots__ = ("buyer_reference", "project_reference", "contract_reference", "contract_value", "purchase_order_reference", "sales_order_reference", "receiving_advice_reference", "despatch_advice_reference", "tender_or_lot_reference", "invoiced_object", "buyer_accounting_reference", "customs_reference")
    BUYER_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    PROJECT_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    CONTRACT_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    CONTRACT_VALUE_FIELD_NUMBER: _ClassVar[int]
    PURCHASE_ORDER_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    SALES_ORDER_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    RECEIVING_ADVICE_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    DESPATCH_ADVICE_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    TENDER_OR_LOT_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    INVOICED_OBJECT_FIELD_NUMBER: _ClassVar[int]
    BUYER_ACCOUNTING_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    CUSTOMS_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    buyer_reference: str
    project_reference: str
    contract_reference: str
    contract_value: str
    purchase_order_reference: str
    sales_order_reference: str
    receiving_advice_reference: str
    despatch_advice_reference: str
    tender_or_lot_reference: str
    invoiced_object: Identifier
    buyer_accounting_reference: str
    customs_reference: str
    def __init__(self, buyer_reference: _Optional[str] = ..., project_reference: _Optional[str] = ..., contract_reference: _Optional[str] = ..., contract_value: _Optional[str] = ..., purchase_order_reference: _Optional[str] = ..., sales_order_reference: _Optional[str] = ..., receiving_advice_reference: _Optional[str] = ..., despatch_advice_reference: _Optional[str] = ..., tender_or_lot_reference: _Optional[str] = ..., invoiced_object: _Optional[_Union[Identifier, _Mapping]] = ..., buyer_accounting_reference: _Optional[str] = ..., customs_reference: _Optional[str] = ...) -> None: ...

class PrecedingInvoiceReference(_message.Message):
    __slots__ = ("id", "issue_date")
    ID_FIELD_NUMBER: _ClassVar[int]
    ISSUE_DATE_FIELD_NUMBER: _ClassVar[int]
    id: str
    issue_date: str
    def __init__(self, id: _Optional[str] = ..., issue_date: _Optional[str] = ...) -> None: ...

class Party(_message.Message):
    __slots__ = ("name", "trading_name", "identifiers", "legal_registration", "tax_registration_identifier", "additional_legal_information", "electronic_address", "postal_address", "contact")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TRADING_NAME_FIELD_NUMBER: _ClassVar[int]
    IDENTIFIERS_FIELD_NUMBER: _ClassVar[int]
    LEGAL_REGISTRATION_FIELD_NUMBER: _ClassVar[int]
    TAX_REGISTRATION_IDENTIFIER_FIELD_NUMBER: _ClassVar[int]
    ADDITIONAL_LEGAL_INFORMATION_FIELD_NUMBER: _ClassVar[int]
    ELECTRONIC_ADDRESS_FIELD_NUMBER: _ClassVar[int]
    POSTAL_ADDRESS_FIELD_NUMBER: _ClassVar[int]
    CONTACT_FIELD_NUMBER: _ClassVar[int]
    name: str
    trading_name: str
    identifiers: _containers.RepeatedCompositeFieldContainer[Identifier]
    legal_registration: LegalRegistration
    tax_registration_identifier: str
    additional_legal_information: str
    electronic_address: Identifier
    postal_address: PostalAddress
    contact: Contact
    def __init__(self, name: _Optional[str] = ..., trading_name: _Optional[str] = ..., identifiers: _Optional[_Iterable[_Union[Identifier, _Mapping]]] = ..., legal_registration: _Optional[_Union[LegalRegistration, _Mapping]] = ..., tax_registration_identifier: _Optional[str] = ..., additional_legal_information: _Optional[str] = ..., electronic_address: _Optional[_Union[Identifier, _Mapping]] = ..., postal_address: _Optional[_Union[PostalAddress, _Mapping]] = ..., contact: _Optional[_Union[Contact, _Mapping]] = ...) -> None: ...

class LegalRegistration(_message.Message):
    __slots__ = ("id", "scheme_id", "type", "authority_name", "passport_issuing_country")
    ID_FIELD_NUMBER: _ClassVar[int]
    SCHEME_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    AUTHORITY_NAME_FIELD_NUMBER: _ClassVar[int]
    PASSPORT_ISSUING_COUNTRY_FIELD_NUMBER: _ClassVar[int]
    id: str
    scheme_id: str
    type: str
    authority_name: str
    passport_issuing_country: str
    def __init__(self, id: _Optional[str] = ..., scheme_id: _Optional[str] = ..., type: _Optional[str] = ..., authority_name: _Optional[str] = ..., passport_issuing_country: _Optional[str] = ...) -> None: ...

class PostalAddress(_message.Message):
    __slots__ = ("line1", "line2", "line3", "city", "post_code", "country_subdivision", "country_code")
    LINE1_FIELD_NUMBER: _ClassVar[int]
    LINE2_FIELD_NUMBER: _ClassVar[int]
    LINE3_FIELD_NUMBER: _ClassVar[int]
    CITY_FIELD_NUMBER: _ClassVar[int]
    POST_CODE_FIELD_NUMBER: _ClassVar[int]
    COUNTRY_SUBDIVISION_FIELD_NUMBER: _ClassVar[int]
    COUNTRY_CODE_FIELD_NUMBER: _ClassVar[int]
    line1: str
    line2: str
    line3: str
    city: str
    post_code: str
    country_subdivision: str
    country_code: str
    def __init__(self, line1: _Optional[str] = ..., line2: _Optional[str] = ..., line3: _Optional[str] = ..., city: _Optional[str] = ..., post_code: _Optional[str] = ..., country_subdivision: _Optional[str] = ..., country_code: _Optional[str] = ...) -> None: ...

class Contact(_message.Message):
    __slots__ = ("name", "telephone", "email")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TELEPHONE_FIELD_NUMBER: _ClassVar[int]
    EMAIL_FIELD_NUMBER: _ClassVar[int]
    name: str
    telephone: str
    email: str
    def __init__(self, name: _Optional[str] = ..., telephone: _Optional[str] = ..., email: _Optional[str] = ...) -> None: ...

class Payee(_message.Message):
    __slots__ = ("name", "identifier", "legal_registration")
    NAME_FIELD_NUMBER: _ClassVar[int]
    IDENTIFIER_FIELD_NUMBER: _ClassVar[int]
    LEGAL_REGISTRATION_FIELD_NUMBER: _ClassVar[int]
    name: str
    identifier: Identifier
    legal_registration: Identifier
    def __init__(self, name: _Optional[str] = ..., identifier: _Optional[_Union[Identifier, _Mapping]] = ..., legal_registration: _Optional[_Union[Identifier, _Mapping]] = ...) -> None: ...

class TaxRepresentative(_message.Message):
    __slots__ = ("name", "vat_identifier", "postal_address")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VAT_IDENTIFIER_FIELD_NUMBER: _ClassVar[int]
    POSTAL_ADDRESS_FIELD_NUMBER: _ClassVar[int]
    name: str
    vat_identifier: str
    postal_address: PostalAddress
    def __init__(self, name: _Optional[str] = ..., vat_identifier: _Optional[str] = ..., postal_address: _Optional[_Union[PostalAddress, _Mapping]] = ...) -> None: ...

class Delivery(_message.Message):
    __slots__ = ("party_name", "incoterms", "location", "actual_delivery_date", "address")
    PARTY_NAME_FIELD_NUMBER: _ClassVar[int]
    INCOTERMS_FIELD_NUMBER: _ClassVar[int]
    LOCATION_FIELD_NUMBER: _ClassVar[int]
    ACTUAL_DELIVERY_DATE_FIELD_NUMBER: _ClassVar[int]
    ADDRESS_FIELD_NUMBER: _ClassVar[int]
    party_name: str
    incoterms: str
    location: Identifier
    actual_delivery_date: str
    address: PostalAddress
    def __init__(self, party_name: _Optional[str] = ..., incoterms: _Optional[str] = ..., location: _Optional[_Union[Identifier, _Mapping]] = ..., actual_delivery_date: _Optional[str] = ..., address: _Optional[_Union[PostalAddress, _Mapping]] = ...) -> None: ...

class Period(_message.Message):
    __slots__ = ("start_date", "end_date")
    START_DATE_FIELD_NUMBER: _ClassVar[int]
    END_DATE_FIELD_NUMBER: _ClassVar[int]
    start_date: str
    end_date: str
    def __init__(self, start_date: _Optional[str] = ..., end_date: _Optional[str] = ...) -> None: ...

class PaymentInstructions(_message.Message):
    __slots__ = ("id", "means_code", "means_text", "remittance_information", "credit_transfer", "card", "direct_debit")
    ID_FIELD_NUMBER: _ClassVar[int]
    MEANS_CODE_FIELD_NUMBER: _ClassVar[int]
    MEANS_TEXT_FIELD_NUMBER: _ClassVar[int]
    REMITTANCE_INFORMATION_FIELD_NUMBER: _ClassVar[int]
    CREDIT_TRANSFER_FIELD_NUMBER: _ClassVar[int]
    CARD_FIELD_NUMBER: _ClassVar[int]
    DIRECT_DEBIT_FIELD_NUMBER: _ClassVar[int]
    id: str
    means_code: str
    means_text: str
    remittance_information: _containers.RepeatedCompositeFieldContainer[Identifier]
    credit_transfer: CreditTransfer
    card: PaymentCard
    direct_debit: DirectDebit
    def __init__(self, id: _Optional[str] = ..., means_code: _Optional[str] = ..., means_text: _Optional[str] = ..., remittance_information: _Optional[_Iterable[_Union[Identifier, _Mapping]]] = ..., credit_transfer: _Optional[_Union[CreditTransfer, _Mapping]] = ..., card: _Optional[_Union[PaymentCard, _Mapping]] = ..., direct_debit: _Optional[_Union[DirectDebit, _Mapping]] = ...) -> None: ...

class CreditTransfer(_message.Message):
    __slots__ = ("account", "account_name", "service_provider_id", "institution_address")
    ACCOUNT_FIELD_NUMBER: _ClassVar[int]
    ACCOUNT_NAME_FIELD_NUMBER: _ClassVar[int]
    SERVICE_PROVIDER_ID_FIELD_NUMBER: _ClassVar[int]
    INSTITUTION_ADDRESS_FIELD_NUMBER: _ClassVar[int]
    account: Identifier
    account_name: str
    service_provider_id: str
    institution_address: PostalAddress
    def __init__(self, account: _Optional[_Union[Identifier, _Mapping]] = ..., account_name: _Optional[str] = ..., service_provider_id: _Optional[str] = ..., institution_address: _Optional[_Union[PostalAddress, _Mapping]] = ...) -> None: ...

class PaymentCard(_message.Message):
    __slots__ = ("primary_account_number", "holder_name", "network_id")
    PRIMARY_ACCOUNT_NUMBER_FIELD_NUMBER: _ClassVar[int]
    HOLDER_NAME_FIELD_NUMBER: _ClassVar[int]
    NETWORK_ID_FIELD_NUMBER: _ClassVar[int]
    primary_account_number: str
    holder_name: str
    network_id: str
    def __init__(self, primary_account_number: _Optional[str] = ..., holder_name: _Optional[str] = ..., network_id: _Optional[str] = ...) -> None: ...

class DirectDebit(_message.Message):
    __slots__ = ("mandate_reference", "creditor_identifier", "debited_account")
    MANDATE_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    CREDITOR_IDENTIFIER_FIELD_NUMBER: _ClassVar[int]
    DEBITED_ACCOUNT_FIELD_NUMBER: _ClassVar[int]
    mandate_reference: str
    creditor_identifier: str
    debited_account: str
    def __init__(self, mandate_reference: _Optional[str] = ..., creditor_identifier: _Optional[str] = ..., debited_account: _Optional[str] = ...) -> None: ...

class PaymentTerms(_message.Message):
    __slots__ = ("instructions_id", "note", "amount", "installment_due_date")
    INSTRUCTIONS_ID_FIELD_NUMBER: _ClassVar[int]
    NOTE_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    INSTALLMENT_DUE_DATE_FIELD_NUMBER: _ClassVar[int]
    instructions_id: str
    note: str
    amount: str
    installment_due_date: str
    def __init__(self, instructions_id: _Optional[str] = ..., note: _Optional[str] = ..., amount: _Optional[str] = ..., installment_due_date: _Optional[str] = ...) -> None: ...

class TaxCategory(_message.Message):
    __slots__ = ("code", "rate", "tax_scheme", "exemption_reason_code", "exemption_reason_text")
    CODE_FIELD_NUMBER: _ClassVar[int]
    RATE_FIELD_NUMBER: _ClassVar[int]
    TAX_SCHEME_FIELD_NUMBER: _ClassVar[int]
    EXEMPTION_REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    EXEMPTION_REASON_TEXT_FIELD_NUMBER: _ClassVar[int]
    code: str
    rate: str
    tax_scheme: str
    exemption_reason_code: str
    exemption_reason_text: str
    def __init__(self, code: _Optional[str] = ..., rate: _Optional[str] = ..., tax_scheme: _Optional[str] = ..., exemption_reason_code: _Optional[str] = ..., exemption_reason_text: _Optional[str] = ...) -> None: ...

class AllowanceCharge(_message.Message):
    __slots__ = ("is_charge", "amount", "base_amount", "percentage", "reason", "reason_code", "tax_category")
    IS_CHARGE_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    BASE_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    PERCENTAGE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    TAX_CATEGORY_FIELD_NUMBER: _ClassVar[int]
    is_charge: bool
    amount: str
    base_amount: str
    percentage: str
    reason: str
    reason_code: str
    tax_category: TaxCategory
    def __init__(self, is_charge: _Optional[bool] = ..., amount: _Optional[str] = ..., base_amount: _Optional[str] = ..., percentage: _Optional[str] = ..., reason: _Optional[str] = ..., reason_code: _Optional[str] = ..., tax_category: _Optional[_Union[TaxCategory, _Mapping]] = ...) -> None: ...

class DocumentTotals(_message.Message):
    __slots__ = ("line_extension_amount", "allowance_total_amount", "charge_total_amount", "tax_exclusive_amount", "paid_amount", "rounding_amount", "payable_amount", "tax_inclusive_pricing", "tax_amount_accounting_currency", "total_with_tax_aed")
    LINE_EXTENSION_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    ALLOWANCE_TOTAL_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    CHARGE_TOTAL_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    TAX_EXCLUSIVE_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    PAID_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    ROUNDING_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    PAYABLE_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    TAX_INCLUSIVE_PRICING_FIELD_NUMBER: _ClassVar[int]
    TAX_AMOUNT_ACCOUNTING_CURRENCY_FIELD_NUMBER: _ClassVar[int]
    TOTAL_WITH_TAX_AED_FIELD_NUMBER: _ClassVar[int]
    line_extension_amount: str
    allowance_total_amount: str
    charge_total_amount: str
    tax_exclusive_amount: str
    paid_amount: str
    rounding_amount: str
    payable_amount: str
    tax_inclusive_pricing: bool
    tax_amount_accounting_currency: str
    total_with_tax_aed: str
    def __init__(self, line_extension_amount: _Optional[str] = ..., allowance_total_amount: _Optional[str] = ..., charge_total_amount: _Optional[str] = ..., tax_exclusive_amount: _Optional[str] = ..., paid_amount: _Optional[str] = ..., rounding_amount: _Optional[str] = ..., payable_amount: _Optional[str] = ..., tax_inclusive_pricing: _Optional[bool] = ..., tax_amount_accounting_currency: _Optional[str] = ..., total_with_tax_aed: _Optional[str] = ...) -> None: ...

class TaxSubtotal(_message.Message):
    __slots__ = ("taxable_amount", "tax_amount", "category")
    TAXABLE_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    TAX_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    CATEGORY_FIELD_NUMBER: _ClassVar[int]
    taxable_amount: str
    tax_amount: str
    category: TaxCategory
    def __init__(self, taxable_amount: _Optional[str] = ..., tax_amount: _Optional[str] = ..., category: _Optional[_Union[TaxCategory, _Mapping]] = ...) -> None: ...

class SupportingDocument(_message.Message):
    __slots__ = ("reference", "description", "external_uri", "attachment")
    REFERENCE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    EXTERNAL_URI_FIELD_NUMBER: _ClassVar[int]
    ATTACHMENT_FIELD_NUMBER: _ClassVar[int]
    reference: str
    description: str
    external_uri: str
    attachment: Attachment
    def __init__(self, reference: _Optional[str] = ..., description: _Optional[str] = ..., external_uri: _Optional[str] = ..., attachment: _Optional[_Union[Attachment, _Mapping]] = ...) -> None: ...

class Attachment(_message.Message):
    __slots__ = ("object_key", "mime_code", "filename")
    OBJECT_KEY_FIELD_NUMBER: _ClassVar[int]
    MIME_CODE_FIELD_NUMBER: _ClassVar[int]
    FILENAME_FIELD_NUMBER: _ClassVar[int]
    object_key: str
    mime_code: str
    filename: str
    def __init__(self, object_key: _Optional[str] = ..., mime_code: _Optional[str] = ..., filename: _Optional[str] = ...) -> None: ...

class InvoiceLine(_message.Message):
    __slots__ = ("id", "note", "object_identifier", "quantity", "unit_code", "net_amount", "order_reference", "order_line_reference", "despatch_advice_reference", "accounting_reference", "batch_number", "period", "allowances_charges", "price", "tax", "amount_aed", "vat_amount_aed", "item")
    ID_FIELD_NUMBER: _ClassVar[int]
    NOTE_FIELD_NUMBER: _ClassVar[int]
    OBJECT_IDENTIFIER_FIELD_NUMBER: _ClassVar[int]
    QUANTITY_FIELD_NUMBER: _ClassVar[int]
    UNIT_CODE_FIELD_NUMBER: _ClassVar[int]
    NET_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    ORDER_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    ORDER_LINE_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    DESPATCH_ADVICE_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    ACCOUNTING_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    BATCH_NUMBER_FIELD_NUMBER: _ClassVar[int]
    PERIOD_FIELD_NUMBER: _ClassVar[int]
    ALLOWANCES_CHARGES_FIELD_NUMBER: _ClassVar[int]
    PRICE_FIELD_NUMBER: _ClassVar[int]
    TAX_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_AED_FIELD_NUMBER: _ClassVar[int]
    VAT_AMOUNT_AED_FIELD_NUMBER: _ClassVar[int]
    ITEM_FIELD_NUMBER: _ClassVar[int]
    id: str
    note: str
    object_identifier: Identifier
    quantity: str
    unit_code: str
    net_amount: str
    order_reference: str
    order_line_reference: str
    despatch_advice_reference: str
    accounting_reference: str
    batch_number: str
    period: Period
    allowances_charges: _containers.RepeatedCompositeFieldContainer[AllowanceCharge]
    price: Price
    tax: TaxCategory
    amount_aed: str
    vat_amount_aed: str
    item: Item
    def __init__(self, id: _Optional[str] = ..., note: _Optional[str] = ..., object_identifier: _Optional[_Union[Identifier, _Mapping]] = ..., quantity: _Optional[str] = ..., unit_code: _Optional[str] = ..., net_amount: _Optional[str] = ..., order_reference: _Optional[str] = ..., order_line_reference: _Optional[str] = ..., despatch_advice_reference: _Optional[str] = ..., accounting_reference: _Optional[str] = ..., batch_number: _Optional[str] = ..., period: _Optional[_Union[Period, _Mapping]] = ..., allowances_charges: _Optional[_Iterable[_Union[AllowanceCharge, _Mapping]]] = ..., price: _Optional[_Union[Price, _Mapping]] = ..., tax: _Optional[_Union[TaxCategory, _Mapping]] = ..., amount_aed: _Optional[str] = ..., vat_amount_aed: _Optional[str] = ..., item: _Optional[_Union[Item, _Mapping]] = ...) -> None: ...

class Price(_message.Message):
    __slots__ = ("net_price", "discount", "gross_price", "base_quantity", "base_quantity_unit_code")
    NET_PRICE_FIELD_NUMBER: _ClassVar[int]
    DISCOUNT_FIELD_NUMBER: _ClassVar[int]
    GROSS_PRICE_FIELD_NUMBER: _ClassVar[int]
    BASE_QUANTITY_FIELD_NUMBER: _ClassVar[int]
    BASE_QUANTITY_UNIT_CODE_FIELD_NUMBER: _ClassVar[int]
    net_price: str
    discount: str
    gross_price: str
    base_quantity: str
    base_quantity_unit_code: str
    def __init__(self, net_price: _Optional[str] = ..., discount: _Optional[str] = ..., gross_price: _Optional[str] = ..., base_quantity: _Optional[str] = ..., base_quantity_unit_code: _Optional[str] = ...) -> None: ...

class Item(_message.Message):
    __slots__ = ("name", "description", "item_type", "goods_service_type", "seller_item_id", "buyer_item_id", "standard_id", "classifications", "service_accounting_codes", "origin_country", "attributes")
    NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    ITEM_TYPE_FIELD_NUMBER: _ClassVar[int]
    GOODS_SERVICE_TYPE_FIELD_NUMBER: _ClassVar[int]
    SELLER_ITEM_ID_FIELD_NUMBER: _ClassVar[int]
    BUYER_ITEM_ID_FIELD_NUMBER: _ClassVar[int]
    STANDARD_ID_FIELD_NUMBER: _ClassVar[int]
    CLASSIFICATIONS_FIELD_NUMBER: _ClassVar[int]
    SERVICE_ACCOUNTING_CODES_FIELD_NUMBER: _ClassVar[int]
    ORIGIN_COUNTRY_FIELD_NUMBER: _ClassVar[int]
    ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    name: str
    description: str
    item_type: str
    goods_service_type: str
    seller_item_id: str
    buyer_item_id: str
    standard_id: Identifier
    classifications: _containers.RepeatedCompositeFieldContainer[Classification]
    service_accounting_codes: _containers.RepeatedCompositeFieldContainer[Classification]
    origin_country: str
    attributes: _containers.RepeatedCompositeFieldContainer[ItemAttribute]
    def __init__(self, name: _Optional[str] = ..., description: _Optional[str] = ..., item_type: _Optional[str] = ..., goods_service_type: _Optional[str] = ..., seller_item_id: _Optional[str] = ..., buyer_item_id: _Optional[str] = ..., standard_id: _Optional[_Union[Identifier, _Mapping]] = ..., classifications: _Optional[_Iterable[_Union[Classification, _Mapping]]] = ..., service_accounting_codes: _Optional[_Iterable[_Union[Classification, _Mapping]]] = ..., origin_country: _Optional[str] = ..., attributes: _Optional[_Iterable[_Union[ItemAttribute, _Mapping]]] = ...) -> None: ...

class Classification(_message.Message):
    __slots__ = ("code", "scheme_id", "scheme_version")
    CODE_FIELD_NUMBER: _ClassVar[int]
    SCHEME_ID_FIELD_NUMBER: _ClassVar[int]
    SCHEME_VERSION_FIELD_NUMBER: _ClassVar[int]
    code: str
    scheme_id: str
    scheme_version: str
    def __init__(self, code: _Optional[str] = ..., scheme_id: _Optional[str] = ..., scheme_version: _Optional[str] = ...) -> None: ...

class ItemAttribute(_message.Message):
    __slots__ = ("name", "value")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    name: str
    value: str
    def __init__(self, name: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
