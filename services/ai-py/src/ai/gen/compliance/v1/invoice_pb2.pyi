from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class Invoice(_message.Message):
    __slots__ = ("invoice_number", "issue_date", "seller_trn", "buyer_trn", "currency", "total_amount", "vat_amount")
    INVOICE_NUMBER_FIELD_NUMBER: _ClassVar[int]
    ISSUE_DATE_FIELD_NUMBER: _ClassVar[int]
    SELLER_TRN_FIELD_NUMBER: _ClassVar[int]
    BUYER_TRN_FIELD_NUMBER: _ClassVar[int]
    CURRENCY_FIELD_NUMBER: _ClassVar[int]
    TOTAL_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    VAT_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    invoice_number: str
    issue_date: str
    seller_trn: str
    buyer_trn: str
    currency: str
    total_amount: str
    vat_amount: str
    def __init__(self, invoice_number: _Optional[str] = ..., issue_date: _Optional[str] = ..., seller_trn: _Optional[str] = ..., buyer_trn: _Optional[str] = ..., currency: _Optional[str] = ..., total_amount: _Optional[str] = ..., vat_amount: _Optional[str] = ...) -> None: ...
