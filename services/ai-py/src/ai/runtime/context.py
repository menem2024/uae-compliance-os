"""NodeContext: the AgentContext implementation handed to every node (contract sections 3.4, 3.5)."""

from __future__ import annotations

import dataclasses
import logging
import time
from collections.abc import Callable, Mapping, Sequence
from typing import Protocol

from opentelemetry import trace
from opentelemetry.trace import SpanKind, Status, StatusCode
from pydantic import ValidationError

from ai.gateway.types import (
    Message,
    ModelGateway,
    ModelRequest,
    ModelResponse,
    RequestMeta,
    TextPart,
    ToolUsePart,
    Usage,
)
from ai.runtime.agent import AgentContext
from ai.runtime.budget import BudgetMeter
from ai.runtime.errors import BudgetExceeded
from ai.runtime.evalhooks import EvalHook
from ai.runtime.events import EventSink
from ai.runtime.proposals import ProposalDraft, proposal_id
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import RunIdentity

log = logging.getLogger(__name__)
_tracer = trace.get_tracer("ai.runtime")


def error_code_of(exc: BaseException) -> str:
    code = getattr(exc, "code", None)
    if isinstance(code, str) and code:
        return code
    if isinstance(exc, TimeoutError):
        return "timeout"
    if isinstance(exc, ValidationError):
        return "output_invalid"
    return "internal"


def merge_usage(a: Usage | None, b: Usage) -> Usage:
    if a is None:
        return b
    return Usage(
        model=b.model or a.model, prompt_id=b.prompt_id or a.prompt_id,
        prompt_version=b.prompt_version or a.prompt_version,
        input_tokens=a.input_tokens + b.input_tokens, output_tokens=a.output_tokens + b.output_tokens,
        cache_read_input_tokens=a.cache_read_input_tokens + b.cache_read_input_tokens,
        cache_creation_input_tokens=a.cache_creation_input_tokens + b.cache_creation_input_tokens,
        cost_micro_usd=a.cost_micro_usd + b.cost_micro_usd,
        response_cache_hit=a.response_cache_hit and b.response_cache_hit,
        llm_calls=a.llm_calls + b.llm_calls,
    )


class NodeContext(AgentContext, Protocol):
    results: Mapping[str, object]

    def result[T](self, node_id: str, typ: type[T]) -> T: ...


class NodeContextImpl:
    def __init__(self, *, run: RunIdentity, node_id: str, step_id: str, attempt: int, budget: BudgetMeter,
                 tools: ToolRegistry, gateway: ModelGateway, sink: EventSink, hooks: Sequence[EvalHook],
                 results: Mapping[str, object],
                 emit_progress: Callable[[str, Mapping[str, str]], None]) -> None:
        self.run = run
        self.node_id = node_id
        self.step_id = step_id
        self.attempt = attempt
        self.budget = budget
        self.tools = tools
        self.results = results
        self._gateway = gateway
        self._sink = sink
        self._hooks = hooks
        self._emit_progress = emit_progress
        self.usage: Usage | None = None
        self.last_line: tuple[str, Mapping[str, str]] | None = None

    def result[T](self, node_id: str, typ: type[T]) -> T:
        value = self.results[node_id]
        if not isinstance(value, typ):
            raise TypeError(f"result of {node_id} is {type(value).__name__}, not {typ.__name__}")
        return value

    def emit(self, message_key: str, **args: str) -> None:
        line = (message_key, {k: str(v) for k, v in args.items()})
        self.last_line = line
        self._emit_progress(*line)

    async def complete(self, req: ModelRequest) -> ModelResponse:
        self.budget.check_can_call()
        req = dataclasses.replace(req, meta=RequestMeta(self.run.firm_id, self.run.run_id, self.step_id))
        started = time.monotonic()
        with _tracer.start_as_current_span(f"chat {req.model}", kind=SpanKind.CLIENT) as span:
            span.set_attribute("gen_ai.system", "anthropic")
            span.set_attribute("gen_ai.request.model", req.model)
            span.set_attribute("gen_ai.request.max_tokens", req.max_tokens)
            span.set_attribute("compliance.prompt.id", req.prompt_id)
            span.set_attribute("compliance.prompt.version", req.prompt_version)
            try:
                resp = await self._gateway.complete(req)
            except Exception as exc:
                span.set_attribute("compliance.error_code", error_code_of(exc))
                span.set_status(Status(StatusCode.ERROR, error_code_of(exc)))
                raise
            u = resp.usage
            span.set_attribute("gen_ai.usage.input_tokens", u.input_tokens)
            span.set_attribute("gen_ai.usage.output_tokens", u.output_tokens)
            span.set_attribute("gen_ai.response.finish_reasons", [resp.stop_reason])
            span.set_attribute("compliance.cache_key", resp.cache_key)
            span.set_attribute("compliance.cache_hit", u.response_cache_hit)
            span.set_attribute("compliance.cost_micro_usd", u.cost_micro_usd)
            span.set_attribute("compliance.cache_read_input_tokens", u.cache_read_input_tokens)
        self.usage = merge_usage(self.usage, u)
        self.budget.add_usage(u)
        for h in self._hooks:
            try:
                h.on_model_call(req, resp)
            except Exception:
                log.exception("eval hook on_model_call failed")
        if u.response_cache_hit:
            self.emit("gateway.cache_hit", prompt=req.prompt_id)
        log.debug("model call %s took %.0f ms", req.prompt_id, (time.monotonic() - started) * 1000)
        return resp

    async def run_tool_loop(self, req: ModelRequest, *, max_turns: int = 8) -> ModelResponse:
        messages = list(req.messages)
        for _ in range(max_turns):
            resp = await self.complete(dataclasses.replace(req, messages=tuple(messages)))
            if resp.stop_reason != "tool_use" or not resp.tool_calls:
                return resp
            assistant_parts: list[TextPart | ToolUsePart] = [TextPart(resp.text)] if resp.text else []
            assistant_parts += [ToolUsePart(c.id, c.name, c.input) for c in resp.tool_calls]
            messages.append(Message("assistant", tuple(assistant_parts)))
            results = [await self.tools.invoke(self, call) for call in resp.tool_calls]
            messages.append(Message("user", tuple(results)))
        raise BudgetExceeded("steps")

    async def propose(self, draft: ProposalDraft) -> str:
        pid = proposal_id(self.run, draft)
        await self._sink.proposal(self.run, draft, pid)  # raises -> node retry (NatsSink raises retryable)
        self.emit("proposal.created", kind=draft.kind)
        return pid
