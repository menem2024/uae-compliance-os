"""Runtime errors (contract section 3.1)."""

from typing import ClassVar, Literal

type BudgetLimit = Literal["steps", "llm_calls", "input_tokens", "output_tokens", "cost", "deadline"]


class AgentRuntimeError(Exception):
    code: ClassVar[str] = "runtime_error"


class BudgetExceeded(AgentRuntimeError):
    code = "budget_exceeded"

    def __init__(self, limit: BudgetLimit) -> None:
        super().__init__(f"budget exceeded: {limit}")
        self.limit = limit


class GraphInvalid(AgentRuntimeError):  # duplicate id, unknown dependency, cycle, too many nodes
    code = "graph_invalid"


class ToolNotAllowed(AgentRuntimeError):  # tool not in the agent's allowlist
    code = "tool_not_allowed"


class ToolTransientError(AgentRuntimeError):  # a read tool hit a transient backend error; retryable
    code = "tool_transient"
