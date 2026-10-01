import datetime

from google.protobuf import any_pb2 as _any_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AgentRunStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_RUN_STATUS_UNSPECIFIED: _ClassVar[AgentRunStatus]
    AGENT_RUN_STATUS_RUNNING: _ClassVar[AgentRunStatus]
    AGENT_RUN_STATUS_SUCCEEDED: _ClassVar[AgentRunStatus]
    AGENT_RUN_STATUS_FAILED: _ClassVar[AgentRunStatus]
    AGENT_RUN_STATUS_BUDGET_EXCEEDED: _ClassVar[AgentRunStatus]
    AGENT_RUN_STATUS_CANCELLED: _ClassVar[AgentRunStatus]

class AgentStepStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_STEP_STATUS_UNSPECIFIED: _ClassVar[AgentStepStatus]
    AGENT_STEP_STATUS_STARTED: _ClassVar[AgentStepStatus]
    AGENT_STEP_STATUS_SUCCEEDED: _ClassVar[AgentStepStatus]
    AGENT_STEP_STATUS_FAILED: _ClassVar[AgentStepStatus]
    AGENT_STEP_STATUS_RETRYING: _ClassVar[AgentStepStatus]
    AGENT_STEP_STATUS_SKIPPED: _ClassVar[AgentStepStatus]

class StepKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    STEP_KIND_UNSPECIFIED: _ClassVar[StepKind]
    STEP_KIND_DETERMINISTIC: _ClassVar[StepKind]
    STEP_KIND_LLM: _ClassVar[StepKind]
    STEP_KIND_TOOL: _ClassVar[StepKind]
    STEP_KIND_ROUTER: _ClassVar[StepKind]

class Verdict(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VERDICT_UNSPECIFIED: _ClassVar[Verdict]
    VERDICT_ACCEPT: _ClassVar[Verdict]
    VERDICT_REVISE: _ClassVar[Verdict]
    VERDICT_ESCALATE: _ClassVar[Verdict]
AGENT_RUN_STATUS_UNSPECIFIED: AgentRunStatus
AGENT_RUN_STATUS_RUNNING: AgentRunStatus
AGENT_RUN_STATUS_SUCCEEDED: AgentRunStatus
AGENT_RUN_STATUS_FAILED: AgentRunStatus
AGENT_RUN_STATUS_BUDGET_EXCEEDED: AgentRunStatus
AGENT_RUN_STATUS_CANCELLED: AgentRunStatus
AGENT_STEP_STATUS_UNSPECIFIED: AgentStepStatus
AGENT_STEP_STATUS_STARTED: AgentStepStatus
AGENT_STEP_STATUS_SUCCEEDED: AgentStepStatus
AGENT_STEP_STATUS_FAILED: AgentStepStatus
AGENT_STEP_STATUS_RETRYING: AgentStepStatus
AGENT_STEP_STATUS_SKIPPED: AgentStepStatus
STEP_KIND_UNSPECIFIED: StepKind
STEP_KIND_DETERMINISTIC: StepKind
STEP_KIND_LLM: StepKind
STEP_KIND_TOOL: StepKind
STEP_KIND_ROUTER: StepKind
VERDICT_UNSPECIFIED: Verdict
VERDICT_ACCEPT: Verdict
VERDICT_REVISE: Verdict
VERDICT_ESCALATE: Verdict

class ModelUsage(_message.Message):
    __slots__ = ("model", "prompt_id", "prompt_version", "input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "cost_micro_usd", "response_cache_hit", "llm_calls")
    MODEL_FIELD_NUMBER: _ClassVar[int]
    PROMPT_ID_FIELD_NUMBER: _ClassVar[int]
    PROMPT_VERSION_FIELD_NUMBER: _ClassVar[int]
    INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    CACHE_READ_INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    CACHE_CREATION_INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_MICRO_USD_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_CACHE_HIT_FIELD_NUMBER: _ClassVar[int]
    LLM_CALLS_FIELD_NUMBER: _ClassVar[int]
    model: str
    prompt_id: str
    prompt_version: int
    input_tokens: int
    output_tokens: int
    cache_read_input_tokens: int
    cache_creation_input_tokens: int
    cost_micro_usd: int
    response_cache_hit: bool
    llm_calls: int
    def __init__(self, model: _Optional[str] = ..., prompt_id: _Optional[str] = ..., prompt_version: _Optional[int] = ..., input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., cache_read_input_tokens: _Optional[int] = ..., cache_creation_input_tokens: _Optional[int] = ..., cost_micro_usd: _Optional[int] = ..., response_cache_hit: _Optional[bool] = ..., llm_calls: _Optional[int] = ...) -> None: ...

class RunBudget(_message.Message):
    __slots__ = ("max_steps", "max_llm_calls", "max_input_tokens", "max_output_tokens", "max_cost_micro_usd", "deadline_seconds")
    MAX_STEPS_FIELD_NUMBER: _ClassVar[int]
    MAX_LLM_CALLS_FIELD_NUMBER: _ClassVar[int]
    MAX_INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    MAX_OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    MAX_COST_MICRO_USD_FIELD_NUMBER: _ClassVar[int]
    DEADLINE_SECONDS_FIELD_NUMBER: _ClassVar[int]
    max_steps: int
    max_llm_calls: int
    max_input_tokens: int
    max_output_tokens: int
    max_cost_micro_usd: int
    deadline_seconds: int
    def __init__(self, max_steps: _Optional[int] = ..., max_llm_calls: _Optional[int] = ..., max_input_tokens: _Optional[int] = ..., max_output_tokens: _Optional[int] = ..., max_cost_micro_usd: _Optional[int] = ..., deadline_seconds: _Optional[int] = ...) -> None: ...

class RunTotals(_message.Message):
    __slots__ = ("steps", "llm_calls", "response_cache_hits", "input_tokens", "output_tokens", "cost_micro_usd")
    STEPS_FIELD_NUMBER: _ClassVar[int]
    LLM_CALLS_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_CACHE_HITS_FIELD_NUMBER: _ClassVar[int]
    INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_MICRO_USD_FIELD_NUMBER: _ClassVar[int]
    steps: int
    llm_calls: int
    response_cache_hits: int
    input_tokens: int
    output_tokens: int
    cost_micro_usd: int
    def __init__(self, steps: _Optional[int] = ..., llm_calls: _Optional[int] = ..., response_cache_hits: _Optional[int] = ..., input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., cost_micro_usd: _Optional[int] = ...) -> None: ...

class GraphNode(_message.Message):
    __slots__ = ("node_id", "agent", "action", "kind", "depends_on")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    DEPENDS_ON_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    agent: str
    action: str
    kind: StepKind
    depends_on: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, node_id: _Optional[str] = ..., agent: _Optional[str] = ..., action: _Optional[str] = ..., kind: _Optional[_Union[StepKind, str]] = ..., depends_on: _Optional[_Iterable[str]] = ...) -> None: ...

class AgentRunStarted(_message.Message):
    __slots__ = ("run_id", "firm_id", "client_company_id", "workflow", "subject_type", "subject_id", "budget", "started_at", "trace_id", "delivery_attempt", "plan")
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_COMPANY_ID_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TYPE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    BUDGET_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    TRACE_ID_FIELD_NUMBER: _ClassVar[int]
    DELIVERY_ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    PLAN_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    firm_id: str
    client_company_id: str
    workflow: str
    subject_type: str
    subject_id: str
    budget: RunBudget
    started_at: _timestamp_pb2.Timestamp
    trace_id: str
    delivery_attempt: int
    plan: _containers.RepeatedCompositeFieldContainer[GraphNode]
    def __init__(self, run_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., client_company_id: _Optional[str] = ..., workflow: _Optional[str] = ..., subject_type: _Optional[str] = ..., subject_id: _Optional[str] = ..., budget: _Optional[_Union[RunBudget, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., trace_id: _Optional[str] = ..., delivery_attempt: _Optional[int] = ..., plan: _Optional[_Iterable[_Union[GraphNode, _Mapping]]] = ...) -> None: ...

class AgentStepEvent(_message.Message):
    __slots__ = ("run_id", "firm_id", "step_id", "seq", "node_id", "depends_on", "agent", "action", "kind", "status", "attempt", "at", "duration_ms", "usage", "message_key", "message_args", "error_code", "subject_type", "subject_id", "client_company_id")
    class MessageArgsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    SEQ_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    DEPENDS_ON_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    AT_FIELD_NUMBER: _ClassVar[int]
    DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    USAGE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_KEY_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ARGS_FIELD_NUMBER: _ClassVar[int]
    ERROR_CODE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TYPE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_COMPANY_ID_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    firm_id: str
    step_id: str
    seq: int
    node_id: str
    depends_on: _containers.RepeatedScalarFieldContainer[str]
    agent: str
    action: str
    kind: StepKind
    status: AgentStepStatus
    attempt: int
    at: _timestamp_pb2.Timestamp
    duration_ms: int
    usage: ModelUsage
    message_key: str
    message_args: _containers.ScalarMap[str, str]
    error_code: str
    subject_type: str
    subject_id: str
    client_company_id: str
    def __init__(self, run_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., step_id: _Optional[str] = ..., seq: _Optional[int] = ..., node_id: _Optional[str] = ..., depends_on: _Optional[_Iterable[str]] = ..., agent: _Optional[str] = ..., action: _Optional[str] = ..., kind: _Optional[_Union[StepKind, str]] = ..., status: _Optional[_Union[AgentStepStatus, str]] = ..., attempt: _Optional[int] = ..., at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., duration_ms: _Optional[int] = ..., usage: _Optional[_Union[ModelUsage, _Mapping]] = ..., message_key: _Optional[str] = ..., message_args: _Optional[_Mapping[str, str]] = ..., error_code: _Optional[str] = ..., subject_type: _Optional[str] = ..., subject_id: _Optional[str] = ..., client_company_id: _Optional[str] = ...) -> None: ...

class AgentRunFinished(_message.Message):
    __slots__ = ("run_id", "firm_id", "status", "totals", "finished_at", "error_code", "subject_type", "subject_id")
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TOTALS_FIELD_NUMBER: _ClassVar[int]
    FINISHED_AT_FIELD_NUMBER: _ClassVar[int]
    ERROR_CODE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TYPE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    firm_id: str
    status: AgentRunStatus
    totals: RunTotals
    finished_at: _timestamp_pb2.Timestamp
    error_code: str
    subject_type: str
    subject_id: str
    def __init__(self, run_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., status: _Optional[_Union[AgentRunStatus, str]] = ..., totals: _Optional[_Union[RunTotals, _Mapping]] = ..., finished_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., error_code: _Optional[str] = ..., subject_type: _Optional[str] = ..., subject_id: _Optional[str] = ...) -> None: ...

class VerifierFinding(_message.Message):
    __slots__ = ("path", "code", "severity", "observed", "expected", "evidence_ref", "source")
    PATH_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    SEVERITY_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_REF_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    path: str
    code: str
    severity: str
    observed: str
    expected: str
    evidence_ref: str
    source: str
    def __init__(self, path: _Optional[str] = ..., code: _Optional[str] = ..., severity: _Optional[str] = ..., observed: _Optional[str] = ..., expected: _Optional[str] = ..., evidence_ref: _Optional[str] = ..., source: _Optional[str] = ...) -> None: ...

class VerifierVerdict(_message.Message):
    __slots__ = ("verdict", "confidence", "findings", "critic_model", "revisions")
    VERDICT_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    FINDINGS_FIELD_NUMBER: _ClassVar[int]
    CRITIC_MODEL_FIELD_NUMBER: _ClassVar[int]
    REVISIONS_FIELD_NUMBER: _ClassVar[int]
    verdict: Verdict
    confidence: float
    findings: _containers.RepeatedCompositeFieldContainer[VerifierFinding]
    critic_model: str
    revisions: int
    def __init__(self, verdict: _Optional[_Union[Verdict, str]] = ..., confidence: _Optional[float] = ..., findings: _Optional[_Iterable[_Union[VerifierFinding, _Mapping]]] = ..., critic_model: _Optional[str] = ..., revisions: _Optional[int] = ...) -> None: ...

class FieldChange(_message.Message):
    __slots__ = ("path", "old_value", "new_value")
    PATH_FIELD_NUMBER: _ClassVar[int]
    OLD_VALUE_FIELD_NUMBER: _ClassVar[int]
    NEW_VALUE_FIELD_NUMBER: _ClassVar[int]
    path: str
    old_value: str
    new_value: str
    def __init__(self, path: _Optional[str] = ..., old_value: _Optional[str] = ..., new_value: _Optional[str] = ...) -> None: ...

class Evidence(_message.Message):
    __slots__ = ("kind", "ref", "excerpt")
    KIND_FIELD_NUMBER: _ClassVar[int]
    REF_FIELD_NUMBER: _ClassVar[int]
    EXCERPT_FIELD_NUMBER: _ClassVar[int]
    kind: str
    ref: str
    excerpt: str
    def __init__(self, kind: _Optional[str] = ..., ref: _Optional[str] = ..., excerpt: _Optional[str] = ...) -> None: ...

class Proposal(_message.Message):
    __slots__ = ("proposal_id", "firm_id", "client_company_id", "run_id", "agent", "kind", "target_type", "target_id", "summary_key", "summary_args", "rationale", "confidence", "changes", "detail", "evidence", "created_at", "expires_at")
    class SummaryArgsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    PROPOSAL_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_COMPANY_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    TARGET_TYPE_FIELD_NUMBER: _ClassVar[int]
    TARGET_ID_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_KEY_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_ARGS_FIELD_NUMBER: _ClassVar[int]
    RATIONALE_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    CHANGES_FIELD_NUMBER: _ClassVar[int]
    DETAIL_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    proposal_id: str
    firm_id: str
    client_company_id: str
    run_id: str
    agent: str
    kind: str
    target_type: str
    target_id: str
    summary_key: str
    summary_args: _containers.ScalarMap[str, str]
    rationale: str
    confidence: float
    changes: _containers.RepeatedCompositeFieldContainer[FieldChange]
    detail: _any_pb2.Any
    evidence: _containers.RepeatedCompositeFieldContainer[Evidence]
    created_at: _timestamp_pb2.Timestamp
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, proposal_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., client_company_id: _Optional[str] = ..., run_id: _Optional[str] = ..., agent: _Optional[str] = ..., kind: _Optional[str] = ..., target_type: _Optional[str] = ..., target_id: _Optional[str] = ..., summary_key: _Optional[str] = ..., summary_args: _Optional[_Mapping[str, str]] = ..., rationale: _Optional[str] = ..., confidence: _Optional[float] = ..., changes: _Optional[_Iterable[_Union[FieldChange, _Mapping]]] = ..., detail: _Optional[_Union[_any_pb2.Any, _Mapping]] = ..., evidence: _Optional[_Iterable[_Union[Evidence, _Mapping]]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ProposalCreated(_message.Message):
    __slots__ = ("proposal",)
    PROPOSAL_FIELD_NUMBER: _ClassVar[int]
    proposal: Proposal
    def __init__(self, proposal: _Optional[_Union[Proposal, _Mapping]] = ...) -> None: ...

class AgentTaskRequested(_message.Message):
    __slots__ = ("task_id", "firm_id", "client_company_id", "agent", "subject_type", "subject_id", "input", "requested_by", "budget")
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_COMPANY_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TYPE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_BY_FIELD_NUMBER: _ClassVar[int]
    BUDGET_FIELD_NUMBER: _ClassVar[int]
    task_id: str
    firm_id: str
    client_company_id: str
    agent: str
    subject_type: str
    subject_id: str
    input: _any_pb2.Any
    requested_by: str
    budget: RunBudget
    def __init__(self, task_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., client_company_id: _Optional[str] = ..., agent: _Optional[str] = ..., subject_type: _Optional[str] = ..., subject_id: _Optional[str] = ..., input: _Optional[_Union[_any_pb2.Any, _Mapping]] = ..., requested_by: _Optional[str] = ..., budget: _Optional[_Union[RunBudget, _Mapping]] = ...) -> None: ...

class AgentTaskCompleted(_message.Message):
    __slots__ = ("task_id", "firm_id", "client_company_id", "agent", "run_id", "status", "output", "error_code", "subject_type", "subject_id")
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    FIRM_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_COMPANY_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    ERROR_CODE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TYPE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    task_id: str
    firm_id: str
    client_company_id: str
    agent: str
    run_id: str
    status: AgentRunStatus
    output: _any_pb2.Any
    error_code: str
    subject_type: str
    subject_id: str
    def __init__(self, task_id: _Optional[str] = ..., firm_id: _Optional[str] = ..., client_company_id: _Optional[str] = ..., agent: _Optional[str] = ..., run_id: _Optional[str] = ..., status: _Optional[_Union[AgentRunStatus, str]] = ..., output: _Optional[_Union[_any_pb2.Any, _Mapping]] = ..., error_code: _Optional[str] = ..., subject_type: _Optional[str] = ..., subject_id: _Optional[str] = ...) -> None: ...
