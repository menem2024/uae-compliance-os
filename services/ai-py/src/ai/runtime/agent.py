"""Agent protocols and the LLMAgent base (contract section 3.4)."""

from typing import ClassVar, Protocol

from pydantic import BaseModel

from ai.gateway.types import ModelId, ModelRequest, ModelResponse
from ai.runtime.budget import BudgetMeter
from ai.runtime.proposals import ProposalDraft
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import RunIdentity


class AgentContext(Protocol):
    run: RunIdentity
    node_id: str
    step_id: str
    attempt: int
    budget: BudgetMeter
    tools: ToolRegistry

    async def complete(self, req: ModelRequest) -> ModelResponse: ...
    async def run_tool_loop(self, req: ModelRequest, *, max_turns: int = 8) -> ModelResponse: ...
    def emit(self, message_key: str, **args: str) -> None: ...
    async def propose(self, draft: ProposalDraft) -> str: ...


class Agent[InT: BaseModel, OutT: BaseModel](Protocol):
    name: ClassVar[str]
    version: ClassVar[int]
    input_model: ClassVar[type[InT]]
    output_model: ClassVar[type[OutT]]
    tools: ClassVar[tuple[str, ...]]

    async def run(self, ctx: AgentContext, inp: InT) -> OutT: ...


class LLMAgent[InT: BaseModel, OutT: BaseModel]:
    """Convenience base: subclasses set name/version/models/prompt and implement build_request()."""

    name: ClassVar[str]
    version: ClassVar[int]
    input_model: ClassVar[type[BaseModel]]
    output_model: ClassVar[type[BaseModel]]
    tools: ClassVar[tuple[str, ...]] = ()
    model: ModelId
    prompt_id: str
    prompt_version: int

    def build_request(self, inp: InT) -> ModelRequest:
        raise NotImplementedError

    async def run(self, ctx: AgentContext, inp: InT) -> OutT:
        resp = await ctx.complete(self.build_request(inp))
        return self.output_model.model_validate(resp.parsed)  # type: ignore[return-value]
