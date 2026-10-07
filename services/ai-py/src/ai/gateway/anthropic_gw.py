"""The only provider adapter: Anthropic Messages API (contract sections 1 and 2).

This is the only module in ai-py that imports the anthropic SDK (tests/test_import_boundaries.py).
Verified against anthropic==1.8.0: AsyncMessages.create has no temperature/top_p/top_k parameters;
effort and structured output go in output_config; our RetryPolicy owns retries (max_retries=0).
"""

from __future__ import annotations

import base64
import time
from collections.abc import Sequence
from typing import Any, Protocol

import anthropic
from pydantic import ValidationError

from ai.gateway.errors import (
    GatewayError,
    ModelRefusal,
    OutputInvalid,
    PermanentModelError,
    TransientModelError,
)
from ai.gateway.keys import cache_key
from ai.gateway.pricing import PRICING, Price, cost_micro_usd
from ai.gateway.redact import redact
from ai.gateway.schema import strict_schema
from ai.gateway.types import (
    HAIKU,
    OPUS,
    DocumentPart,
    ImagePart,
    ModelRequest,
    ModelResponse,
    Part,
    TextPart,
    ToolCall,
    ToolResultPart,
    ToolUsePart,
    Usage,
)

_TRANSIENT = (anthropic.RateLimitError, anthropic.OverloadedError, anthropic.ServiceUnavailableError,
              anthropic.InternalServerError, anthropic.DeadlineExceededError)


class _Messages(Protocol):
    async def create(self, **kwargs: Any) -> Any: ...


class AnthropicLike(Protocol):
    @property
    def messages(self) -> _Messages: ...


def make_client(api_key: str | None = None) -> anthropic.AsyncAnthropic:
    """ANTHROPIC_API_KEY is read by the SDK when api_key is None."""
    return anthropic.AsyncAnthropic(api_key=api_key, max_retries=0,
                                    timeout=anthropic.Timeout(120.0, connect=5.0))


def _block(p: Part) -> dict[str, Any]:
    match p:
        case TextPart(text=text):
            return {"type": "text", "text": text}
        case DocumentPart(data=data, media_type=mt):
            return {"type": "document",
                    "source": {"type": "base64", "media_type": mt, "data": base64.b64encode(data).decode()}}
        case ImagePart(data=data, media_type=mt):
            return {"type": "image",
                    "source": {"type": "base64", "media_type": mt, "data": base64.b64encode(data).decode()}}
        case ToolUsePart(id=id_, name=name, input=inp):
            return {"type": "tool_use", "id": id_, "name": name, "input": dict(inp)}
        case ToolResultPart(tool_use_id=tid, content=content, is_error=err):
            return {"type": "tool_result", "tool_use_id": tid, "content": content, "is_error": err}
    raise TypeError(f"unknown part {type(p).__name__}")


def _content(parts: Sequence[Part]) -> list[dict[str, Any]]:
    binary = [p for p in parts if isinstance(p, DocumentPart | ImagePart)]
    rest = [p for p in parts if not isinstance(p, DocumentPart | ImagePart)]
    return [_block(p) for p in (*binary, *rest)]  # binary parts before text (contract section 1)


def build_kwargs(req: ModelRequest) -> dict[str, Any]:
    """The exact keyword arguments for messages.create. Never temperature/top_p/top_k/thinking/prefill."""
    kwargs: dict[str, Any] = {
        "model": req.model,
        "max_tokens": req.max_tokens,
        "system": req.system,
        "messages": [{"role": m.role, "content": _content(m.parts)} for m in req.messages],
        "cache_control": {"type": "ephemeral"},
    }
    effort = None if req.model == HAIKU else (req.effort or ("medium" if req.model == OPUS else None))
    output_config: dict[str, Any] = {}
    if effort is not None:
        output_config["effort"] = effort
    if req.output_model is not None:
        output_config["format"] = {"type": "json_schema", "schema": strict_schema(req.output_model)}
    if output_config:
        kwargs["output_config"] = output_config
    if req.tools:
        kwargs["tools"] = [{"name": t.name, "description": t.description,
                            "input_schema": strict_schema(t.input_model), "strict": True} for t in req.tools]
        kwargs["tool_choice"] = {"type": "auto"}
    return kwargs


def _retry_after(exc: anthropic.APIStatusError) -> float | None:
    raw = exc.response.headers.get("retry-after", "")
    try:
        return float(raw) if raw else None
    except ValueError:
        return None


def _billed(exc: GatewayError, usage: Usage) -> GatewayError:
    """A call the provider completed (and billed) that still failed: SpendLimitedGateway charges `usage`."""
    exc.usage = usage
    return exc


def map_error(exc: anthropic.APIError) -> GatewayError:
    """SDK error -> gateway taxonomy. Messages are redacted and never carry request content."""
    name = type(exc).__name__
    if isinstance(exc, anthropic.APIConnectionError):  # includes APITimeoutError
        return TransientModelError(name)
    if isinstance(exc, anthropic.APIStatusError):
        if isinstance(exc, _TRANSIENT) or exc.status_code == 429 or exc.status_code >= 500:
            return TransientModelError(f"{name} {exc.status_code}", retry_after_s=_retry_after(exc))
        return PermanentModelError(redact(f"{name} {exc.status_code}: {exc.message}"))
    return PermanentModelError(name)


class AnthropicGateway:
    def __init__(self, client: AnthropicLike, pricing: dict[str, Price] = PRICING) -> None:
        self._client = client
        self._pricing = pricing

    async def complete(self, req: ModelRequest) -> ModelResponse:
        kwargs = build_kwargs(req)
        started = time.monotonic()
        try:
            msg = await self._client.messages.create(**kwargs)
        except anthropic.APIError as exc:
            raise map_error(exc) from None  # `from None`: the SDK error may echo request content
        return self._to_response(req, msg, int((time.monotonic() - started) * 1000))

    def _usage(self, req: ModelRequest, msg: Any) -> Usage:
        u = msg.usage
        read, write = u.cache_read_input_tokens or 0, u.cache_creation_input_tokens or 0
        return Usage(model=req.model, prompt_id=req.prompt_id, prompt_version=req.prompt_version,
                     input_tokens=u.input_tokens, output_tokens=u.output_tokens,
                     cache_read_input_tokens=read, cache_creation_input_tokens=write,
                     cost_micro_usd=cost_micro_usd(req.model, input_tokens=u.input_tokens,
                                                   output_tokens=u.output_tokens,
                                                   cache_read_input_tokens=read,
                                                   cache_creation_input_tokens=write, pricing=self._pricing),
                     llm_calls=1)

    def _to_response(self, req: ModelRequest, msg: Any, latency_ms: int) -> ModelResponse:
        stop = msg.stop_reason or ""
        usage = self._usage(req, msg)  # billed even when the call fails below: the error carries it
        if stop == "refusal":
            raise _billed(ModelRefusal(msg.stop_details.category if msg.stop_details else None), usage)
        if stop == "max_tokens":
            raise _billed(OutputInvalid(f"{req.prompt_id}: stop_reason max_tokens"), usage)
        if stop == "model_context_window_exceeded":
            raise _billed(PermanentModelError(f"{req.prompt_id}: context window exceeded"), usage)
        # thinking blocks (adaptive thinking) and every other non-text block are ignored
        text = "".join(b.text for b in msg.content if b.type == "text")
        calls = tuple(ToolCall(b.id, b.name, dict(b.input)) for b in msg.content if b.type == "tool_use")
        parsed = None
        if req.output_model is not None and stop != "tool_use":
            try:
                parsed = req.output_model.model_validate_json(text)
            except ValidationError:
                name = req.output_model.__name__
                raise _billed(OutputInvalid(f"{req.prompt_id}: output does not match {name}"),
                              usage) from None
        return ModelResponse(model=msg.model, text=text, parsed=parsed, tool_calls=calls, stop_reason=stop,
                             usage=usage, latency_ms=latency_ms, cache_key=cache_key(req),
                             provider_request_id=getattr(msg, "_request_id", None) or "")


def make_gateway(api_key: str | None = None) -> AnthropicGateway:
    return AnthropicGateway(make_client(api_key))
