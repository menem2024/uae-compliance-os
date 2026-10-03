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
    __slots__ = ("invoice", "ruleset_version")
    INVOICE_FIELD_NUMBER: _ClassVar[int]
    RULESET_VERSION_FIELD_NUMBER: _ClassVar[int]
    invoice: _invoice_pb2.Invoice
    ruleset_version: str
    def __init__(self, invoice: _Optional[_Union[_invoice_pb2.Invoice, _Mapping]] = ..., ruleset_version: _Optional[str] = ...) -> None: ...

class ValidateResponse(_message.Message):
    __slots__ = ("run",)
    RUN_FIELD_NUMBER: _ClassVar[int]
    run: ValidationRun
    def __init__(self, run: _Optional[_Union[ValidationRun, _Mapping]] = ...) -> None: ...

class ValidationIssue(_message.Message):
    __slots__ = ("rule_id", "severity", "path", "message", "business_term", "message_args", "fixable", "message_ar", "suggested_value")
    class MessageArgsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    RULE_ID_FIELD_NUMBER: _ClassVar[int]
    SEVERITY_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    BUSINESS_TERM_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ARGS_FIELD_NUMBER: _ClassVar[int]
    FIXABLE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_AR_FIELD_NUMBER: _ClassVar[int]
    SUGGESTED_VALUE_FIELD_NUMBER: _ClassVar[int]
    rule_id: str
    severity: Severity
    path: str
    message: str
    business_term: str
    message_args: _containers.ScalarMap[str, str]
    fixable: bool
    message_ar: str
    suggested_value: str
    def __init__(self, rule_id: _Optional[str] = ..., severity: _Optional[_Union[Severity, str]] = ..., path: _Optional[str] = ..., message: _Optional[str] = ..., business_term: _Optional[str] = ..., message_args: _Optional[_Mapping[str, str]] = ..., fixable: _Optional[bool] = ..., message_ar: _Optional[str] = ..., suggested_value: _Optional[str] = ...) -> None: ...

class ValidationRun(_message.Message):
    __slots__ = ("ruleset_version", "issues", "duration_us", "rules_evaluated")
    RULESET_VERSION_FIELD_NUMBER: _ClassVar[int]
    ISSUES_FIELD_NUMBER: _ClassVar[int]
    DURATION_US_FIELD_NUMBER: _ClassVar[int]
    RULES_EVALUATED_FIELD_NUMBER: _ClassVar[int]
    ruleset_version: str
    issues: _containers.RepeatedCompositeFieldContainer[ValidationIssue]
    duration_us: int
    rules_evaluated: int
    def __init__(self, ruleset_version: _Optional[str] = ..., issues: _Optional[_Iterable[_Union[ValidationIssue, _Mapping]]] = ..., duration_us: _Optional[int] = ..., rules_evaluated: _Optional[int] = ...) -> None: ...

class FixTaskInput(_message.Message):
    __slots__ = ("invoice_id", "payload_version", "validation_run_id", "ruleset_version", "invoice", "issues", "mode")
    INVOICE_ID_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_VERSION_FIELD_NUMBER: _ClassVar[int]
    VALIDATION_RUN_ID_FIELD_NUMBER: _ClassVar[int]
    RULESET_VERSION_FIELD_NUMBER: _ClassVar[int]
    INVOICE_FIELD_NUMBER: _ClassVar[int]
    ISSUES_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    invoice_id: str
    payload_version: int
    validation_run_id: str
    ruleset_version: str
    invoice: _invoice_pb2.Invoice
    issues: _containers.RepeatedCompositeFieldContainer[ValidationIssue]
    mode: str
    def __init__(self, invoice_id: _Optional[str] = ..., payload_version: _Optional[int] = ..., validation_run_id: _Optional[str] = ..., ruleset_version: _Optional[str] = ..., invoice: _Optional[_Union[_invoice_pb2.Invoice, _Mapping]] = ..., issues: _Optional[_Iterable[_Union[ValidationIssue, _Mapping]]] = ..., mode: _Optional[str] = ...) -> None: ...

class FixChangeNote(_message.Message):
    __slots__ = ("path", "rule_ids", "source", "rationale")
    PATH_FIELD_NUMBER: _ClassVar[int]
    RULE_IDS_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    RATIONALE_FIELD_NUMBER: _ClassVar[int]
    path: str
    rule_ids: _containers.RepeatedScalarFieldContainer[str]
    source: str
    rationale: str
    def __init__(self, path: _Optional[str] = ..., rule_ids: _Optional[_Iterable[str]] = ..., source: _Optional[str] = ..., rationale: _Optional[str] = ...) -> None: ...

class FixProposalDetail(_message.Message):
    __slots__ = ("validation_run_id", "payload_version", "ruleset_version", "notes", "errors_before", "errors_after", "resolved_rule_ids")
    VALIDATION_RUN_ID_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_VERSION_FIELD_NUMBER: _ClassVar[int]
    RULESET_VERSION_FIELD_NUMBER: _ClassVar[int]
    NOTES_FIELD_NUMBER: _ClassVar[int]
    ERRORS_BEFORE_FIELD_NUMBER: _ClassVar[int]
    ERRORS_AFTER_FIELD_NUMBER: _ClassVar[int]
    RESOLVED_RULE_IDS_FIELD_NUMBER: _ClassVar[int]
    validation_run_id: str
    payload_version: int
    ruleset_version: str
    notes: _containers.RepeatedCompositeFieldContainer[FixChangeNote]
    errors_before: int
    errors_after: int
    resolved_rule_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, validation_run_id: _Optional[str] = ..., payload_version: _Optional[int] = ..., ruleset_version: _Optional[str] = ..., notes: _Optional[_Iterable[_Union[FixChangeNote, _Mapping]]] = ..., errors_before: _Optional[int] = ..., errors_after: _Optional[int] = ..., resolved_rule_ids: _Optional[_Iterable[str]] = ...) -> None: ...

class FixTaskResult(_message.Message):
    __slots__ = ("outcome", "proposal_id", "changes")
    OUTCOME_FIELD_NUMBER: _ClassVar[int]
    PROPOSAL_ID_FIELD_NUMBER: _ClassVar[int]
    CHANGES_FIELD_NUMBER: _ClassVar[int]
    outcome: str
    proposal_id: str
    changes: int
    def __init__(self, outcome: _Optional[str] = ..., proposal_id: _Optional[str] = ..., changes: _Optional[int] = ...) -> None: ...
