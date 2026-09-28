from ai.gen.compliance.v1 import invoice_pb2 as _invoice_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Severity(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SEVERITY_UNSPECIFIED: _ClassVar[Severity]
    SEVERITY_ERROR: _ClassVar[Severity]
    SEVERITY_WARNING: _ClassVar[Severity]
SEVERITY_UNSPECIFIED: Severity
SEVERITY_ERROR: Severity
SEVERITY_WARNING: Severity

class ValidateRequest(_message.Message):
    __slots__ = ("invoice",)
    INVOICE_FIELD_NUMBER: _ClassVar[int]
    invoice: _invoice_pb2.Invoice
    def __init__(self, invoice: _Optional[_Union[_invoice_pb2.Invoice, _Mapping]] = ...) -> None: ...

class ValidateResponse(_message.Message):
    __slots__ = ("run",)
    RUN_FIELD_NUMBER: _ClassVar[int]
    run: ValidationRun
    def __init__(self, run: _Optional[_Union[ValidationRun, _Mapping]] = ...) -> None: ...

class ValidationIssue(_message.Message):
    __slots__ = ("rule_id", "severity", "path", "message")
    RULE_ID_FIELD_NUMBER: _ClassVar[int]
    SEVERITY_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    rule_id: str
    severity: Severity
    path: str
    message: str
    def __init__(self, rule_id: _Optional[str] = ..., severity: _Optional[_Union[Severity, str]] = ..., path: _Optional[str] = ..., message: _Optional[str] = ...) -> None: ...

class ValidationRun(_message.Message):
    __slots__ = ("ruleset_version", "issues")
    RULESET_VERSION_FIELD_NUMBER: _ClassVar[int]
    ISSUES_FIELD_NUMBER: _ClassVar[int]
    ruleset_version: str
    issues: _containers.RepeatedCompositeFieldContainer[ValidationIssue]
    def __init__(self, ruleset_version: _Optional[str] = ..., issues: _Optional[_Iterable[_Union[ValidationIssue, _Mapping]]] = ...) -> None: ...
