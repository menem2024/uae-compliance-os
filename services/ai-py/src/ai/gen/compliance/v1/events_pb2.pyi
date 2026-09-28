from ai.gen.compliance.v1 import invoice_pb2 as _invoice_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class InvoiceSubmitted(_message.Message):
    __slots__ = ("invoice_id", "firm_id", "invoice")
    INVOICE_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    INVOICE_FIELD_NUMBER: _ClassVar[int]
    invoice_id: str
    firm_id: str
    invoice: _invoice_pb2.Invoice
    def __init__(self, invoice_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., invoice: _Optional[_Union[_invoice_pb2.Invoice, _Mapping]] = ...) -> None: ...

class InvoiceExtracted(_message.Message):
    __slots__ = ("invoice_id", "firm_id", "invoice", "confidence")
    INVOICE_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    INVOICE_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    invoice_id: str
    firm_id: str
    invoice: _invoice_pb2.Invoice
    confidence: float
    def __init__(self, invoice_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., invoice: _Optional[_Union[_invoice_pb2.Invoice, _Mapping]] = ..., confidence: _Optional[float] = ...) -> None: ...
