"""Runtime core types (agent-runtime contract v0.2, section 3.1)."""

from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import datetime
from enum import StrEnum

from ai.gateway.types import Usage

type MicroUSD = int


class RunStatus(StrEnum):
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    BUDGET_EXCEEDED = "budget_exceeded"
    CANCELLED = "cancelled"


class StepStatus(StrEnum):
    STARTED = "started"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    RETRYING = "retrying"
    SKIPPED = "skipped"


TERMINAL_STEP_STATUSES = frozenset({StepStatus.SUCCEEDED, StepStatus.FAILED, StepStatus.SKIPPED})


class StepKind(StrEnum):
    DETERMINISTIC = "deterministic"
    LLM = "llm"
    TOOL = "tool"
    ROUTER = "router"


@dataclass(frozen=True, slots=True)
class Budget:
    max_steps: int = 40
    max_llm_calls: int = 6
    max_input_tokens: int = 80_000
    max_output_tokens: int = 12_000
    max_cost_micro_usd: MicroUSD = 150_000  # USD 0.15 per run
    deadline_s: float = 300.0


@dataclass(frozen=True, slots=True)
class RunIdentity:
    run_id: str  # uuid4 string, new for every delivery attempt
    firm_id: str
    client_company_id: str  # "" when not scoped to a ClientCompany
    workflow: str  # "<name>@<version>", e.g. "document_ingestion@1"
    subject_type: str  # "document" | "invoice" | "validation_issue" | "source" | "client_company"
    subject_id: str
    delivery_attempt: int = 1  # JetStream num_delivered of the triggering message


@dataclass(frozen=True, slots=True)
class GraphNodeSpec:  # static view of a Node, sent in AgentRunStarted.plan
    node_id: str
    agent: str
    action: str
    kind: StepKind
    depends_on: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class RunTotals:
    steps: int = 0
    llm_calls: int = 0
    response_cache_hits: int = 0
    input_tokens: int = 0
    output_tokens: int = 0
    cost_micro_usd: MicroUSD = 0


@dataclass(frozen=True, slots=True)
class StepRecord:
    run_id: str
    firm_id: str
    step_id: str  # uuid5(run_id, f"{node_id}#{attempt}")
    seq: int  # monotonic per run, starts at 1
    node_id: str
    depends_on: tuple[str, ...]
    agent: str
    action: str
    kind: StepKind
    status: StepStatus
    attempt: int
    at: datetime  # timezone-aware UTC
    duration_ms: int = 0
    usage: Usage | None = None
    message_key: str = ""  # web namespace P1Agents.feed.<key>
    message_args: Mapping[str, str] = field(default_factory=dict)
    error_code: str = ""


@dataclass(frozen=True, slots=True)
class RunOutcome:
    identity: RunIdentity
    status: RunStatus
    results: Mapping[str, object]  # node_id -> node result
    node_status: Mapping[str, StepStatus]
    totals: RunTotals
    error_code: str = ""
