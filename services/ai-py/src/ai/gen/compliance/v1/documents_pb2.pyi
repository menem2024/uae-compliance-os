from ai.gen.compliance.v1 import agents_pb2 as _agents_pb2
from ai.gen.compliance.v1 import invoice_pb2 as _invoice_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ClientCompanyRef(_message.Message):
    __slots__ = ("client_company_id", "name", "name_ar", "trn")
    CLIENT_COMPANY_ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    NAME_AR_FIELD_NUMBER: _ClassVar[int]
    TRN_FIELD_NUMBER: _ClassVar[int]
    client_company_id: str
    name: str
    name_ar: str
    trn: str
    def __init__(self, client_company_id: _Optional[str] = ..., name: _Optional[str] = ..., name_ar: _Optional[str] = ..., trn: _Optional[str] = ...) -> None: ...

class DocumentUploaded(_message.Message):
    __slots__ = ("document_id", "firm_id", "client_company_id", "sha256", "object_key", "content_type", "size_bytes", "filename", "candidates", "reprocess_nonce")
    DOCUMENT_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_COMPANY_ID_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    OBJECT_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    FILENAME_FIELD_NUMBER: _ClassVar[int]
    CANDIDATES_FIELD_NUMBER: _ClassVar[int]
    REPROCESS_NONCE_FIELD_NUMBER: _ClassVar[int]
    document_id: str
    firm_id: str
    client_company_id: str
    sha256: str
    object_key: str
    content_type: str
    size_bytes: int
    filename: str
    candidates: _containers.RepeatedCompositeFieldContainer[ClientCompanyRef]
    reprocess_nonce: str
    def __init__(self, document_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., client_company_id: _Optional[str] = ..., sha256: _Optional[str] = ..., object_key: _Optional[str] = ..., content_type: _Optional[str] = ..., size_bytes: _Optional[int] = ..., filename: _Optional[str] = ..., candidates: _Optional[_Iterable[_Union[ClientCompanyRef, _Mapping]]] = ..., reprocess_nonce: _Optional[str] = ...) -> None: ...

class FieldConfidence(_message.Message):
    __slots__ = ("path", "confidence")
    PATH_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    path: str
    confidence: float
    def __init__(self, path: _Optional[str] = ..., confidence: _Optional[float] = ...) -> None: ...

class ExtractedInvoice(_message.Message):
    __slots__ = ("source_ordinal", "source_ref", "invoice", "confidence", "fields", "verdict")
    SOURCE_ORDINAL_FIELD_NUMBER: _ClassVar[int]
    SOURCE_REF_FIELD_NUMBER: _ClassVar[int]
    INVOICE_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    FIELDS_FIELD_NUMBER: _ClassVar[int]
    VERDICT_FIELD_NUMBER: _ClassVar[int]
    source_ordinal: int
    source_ref: str
    invoice: _invoice_pb2.Invoice
    confidence: float
    fields: _containers.RepeatedCompositeFieldContainer[FieldConfidence]
    verdict: _agents_pb2.VerifierVerdict
    def __init__(self, source_ordinal: _Optional[int] = ..., source_ref: _Optional[str] = ..., invoice: _Optional[_Union[_invoice_pb2.Invoice, _Mapping]] = ..., confidence: _Optional[float] = ..., fields: _Optional[_Iterable[_Union[FieldConfidence, _Mapping]]] = ..., verdict: _Optional[_Union[_agents_pb2.VerifierVerdict, _Mapping]] = ...) -> None: ...

class DocumentExtracted(_message.Message):
    __slots__ = ("document_id", "firm_id", "client_company_id", "run_id", "document_kind", "direction", "extraction_method", "language", "invoices", "needs_review", "review_reasons")
    DOCUMENT_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_COMPANY_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    DOCUMENT_KIND_FIELD_NUMBER: _ClassVar[int]
    DIRECTION_FIELD_NUMBER: _ClassVar[int]
    EXTRACTION_METHOD_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    INVOICES_FIELD_NUMBER: _ClassVar[int]
    NEEDS_REVIEW_FIELD_NUMBER: _ClassVar[int]
    REVIEW_REASONS_FIELD_NUMBER: _ClassVar[int]
    document_id: str
    firm_id: str
    client_company_id: str
    run_id: str
    document_kind: str
    direction: str
    extraction_method: str
    language: str
    invoices: _containers.RepeatedCompositeFieldContainer[ExtractedInvoice]
    needs_review: bool
    review_reasons: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, document_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., client_company_id: _Optional[str] = ..., run_id: _Optional[str] = ..., document_kind: _Optional[str] = ..., direction: _Optional[str] = ..., extraction_method: _Optional[str] = ..., language: _Optional[str] = ..., invoices: _Optional[_Iterable[_Union[ExtractedInvoice, _Mapping]]] = ..., needs_review: _Optional[bool] = ..., review_reasons: _Optional[_Iterable[str]] = ...) -> None: ...

class DocumentFailed(_message.Message):
    __slots__ = ("document_id", "firm_id", "run_id", "reason_code", "detail")
    DOCUMENT_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    DETAIL_FIELD_NUMBER: _ClassVar[int]
    document_id: str
    firm_id: str
    run_id: str
    reason_code: str
    detail: str
    def __init__(self, document_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., run_id: _Optional[str] = ..., reason_code: _Optional[str] = ..., detail: _Optional[str] = ...) -> None: ...
