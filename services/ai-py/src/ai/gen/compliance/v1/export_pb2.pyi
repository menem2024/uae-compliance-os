from ai.gen.compliance.v1 import invoice_pb2 as _invoice_pb2
from ai.gen.compliance.v1 import validator_pb2 as _validator_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ExportRequest(_message.Message):
    __slots__ = ("invoice", "ruleset_version")
    INVOICE_FIELD_NUMBER: _ClassVar[int]
    RULESET_VERSION_FIELD_NUMBER: _ClassVar[int]
    invoice: _invoice_pb2.Invoice
    ruleset_version: str
    def __init__(self, invoice: _Optional[_Union[_invoice_pb2.Invoice, _Mapping]] = ..., ruleset_version: _Optional[str] = ...) -> None: ...

class ExportResponse(_message.Message):
    __slots__ = ("xml", "sha256", "format", "run", "exported", "document_kind")
    XML_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    RUN_FIELD_NUMBER: _ClassVar[int]
    EXPORTED_FIELD_NUMBER: _ClassVar[int]
    DOCUMENT_KIND_FIELD_NUMBER: _ClassVar[int]
    xml: bytes
    sha256: str
    format: str
    run: _validator_pb2.ValidationRun
    exported: bool
    document_kind: str
    def __init__(self, xml: _Optional[bytes] = ..., sha256: _Optional[str] = ..., format: _Optional[str] = ..., run: _Optional[_Union[_validator_pb2.ValidationRun, _Mapping]] = ..., exported: _Optional[bool] = ..., document_kind: _Optional[str] = ...) -> None: ...
