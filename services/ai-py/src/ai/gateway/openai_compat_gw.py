"""Provider adapter for any OpenAI-compatible chat-completions endpoint (OpenRouter, Google Gemini's
OpenAI-compatible API, a local server).

This is the only module in ai-py that imports httpx2 (tests/test_import_boundaries.py). Our RetryPolicy owns
retries: the client makes exactly one attempt. Errors map to the same GatewayError taxonomy as the Anthropic
adapter and never carry request content or the API key.

Our abstract tiers (HAIKU, SONNET, OPUS) map to two configurable model ids: HAIKU -> `model_fast`, SONNET and
OPUS -> `model_smart`. Results are as trustworthy as the model: this adapter changes nothing about how the
runtime treats them (the verifier still decides accept versus needs_review).
"""

from __future__ import annotations

import base64
import json
import time
from collections.abc import Sequence
from typing import Any

import httpx2
from pydantic import ValidationError

from ai.gateway.errors import (
    GatewayError,
    ModelRefusal,
    OutputInvalid,
    PermanentModelError,
    TransientModelError,
)
from ai.gateway.keys import cache_key
from ai.gateway.pricing import Price, cost_micro_usd
from ai.gateway.redact import redact
from ai.gateway.schema import strict_schema
from ai.gateway.types import (
    HAIKU,
    OPUS,
    SONNET,
    DocumentPart,
    ImagePart,
    Message,
    ModelRequest,
    ModelResponse,
    TextPart,
    ToolCall,
    ToolResultPart,
    ToolUsePart,
    Usage,
)

_ERROR_TEXT_MAX = 200
_FINISH_TO_STOP = {"stop": "end_turn", "length": "max_tokens", "tool_calls": "tool_use",
                   "function_call": "tool_use"}


def _data_url(media_type: str, data: bytes) -> str:
    return f"data:{media_type};base64,{base64.b64encode(data).decode()}"


def _user_content(parts: Sequence[Any]) -> list[dict[str, Any]]:
    binary = [p for p in parts if isinstance(p, DocumentPart | ImagePart)]
    texts = [p for p in parts if isinstance(p, TextPart)]
    out: list[dict[str, Any]] = []
    for p in binary:  # binary parts before text, as the Anthropic adapter does
        if isinstance(p, ImagePart):
            out.append({"type": "image_url", "image_url": {"url": _data_url(p.media_type, p.data)}})
        else:
            out.append({"type": "file", "file": {"filename": "document.pdf",
                                                 "file_data": _data_url(p.media_type, p.data)}})
    out.extend({"type": "text", "text": p.text} for p in texts)
    return out


def _messages(req: ModelRequest) -> list[dict[str, Any]]:
    system = req.system
    if req.output_model is not None:
        # Many free models ignore response_format: state the contract in the prompt as well.
        schema = json.dumps(strict_schema(req.output_model), separators=(",", ":"))
        system = f"{system}\n\nRespond with a single JSON object and nothing else, matching this JSON schema:\n{schema}"
    out: list[dict[str, Any]] = [{"role": "system", "content": system}]
    for m in req.messages:
        out.extend(_message(m))
    return out


def _message(m: Message) -> list[dict[str, Any]]:
    if m.role == "assistant":
        text = "".join(p.text for p in m.parts if isinstance(p, TextPart))
        calls = [{"id": p.id, "type": "function",
                  "function": {"name": p.name, "arguments": json.dumps(dict(p.input))}}
                 for p in m.parts if isinstance(p, ToolUsePart)]
        msg: dict[str, Any] = {"role": "assistant", "content": text or None}
        if calls:
            msg["tool_calls"] = calls
        return [msg]
    out: list[dict[str, Any]] = [
        {"role": "tool", "tool_call_id": p.tool_use_id,
         "content": f"error: {p.content}" if p.is_error else p.content}
        for p in m.parts if isinstance(p, ToolResultPart)]
    rest = [p for p in m.parts if not isinstance(p, ToolResultPart)]
    if rest:
        out.append({"role": "user", "content": _user_content(rest)})
    return out


def build_body(req: ModelRequest, model_id: str) -> dict[str, Any]:
    body: dict[str, Any] = {"model": model_id, "messages": _messages(req), "max_tokens": req.max_tokens}
    if req.output_model is not None:
        body["response_format"] = {"type": "json_schema", "json_schema": {
            "name": req.output_model.__name__, "strict": True, "schema": strict_schema(req.output_model)}}
    if req.tools:
        body["tools"] = [{"type": "function", "function": {
            "name": t.name, "description": t.description, "parameters": strict_schema(t.input_model)}}
            for t in req.tools]
        body["tool_choice"] = "auto"
    return body


def _retry_after(response: httpx2.Response) -> float | None:
    raw = response.headers.get("retry-after", "")
    try:
        return float(raw) if raw else None
    except ValueError:
        return None  # an HTTP-date: our own backoff applies


def _json(response: httpx2.Response) -> Any:
    try:
        return response.json()
    except ValueError:
        return None


def _provider_message(payload: Any, secret: str) -> str:
    """The provider's own error text, redacted, truncated and with the API key removed."""
    err = payload.get("error") if isinstance(payload, dict) else None
    text = err.get("message") if isinstance(err, dict) else err if isinstance(err, str) else ""
    text = redact(str(text or ""))[:_ERROR_TEXT_MAX]
    return text.replace(secret, "[key]") if secret else text


def _billed(exc: GatewayError, usage: Usage) -> GatewayError:
    exc.usage = usage
    return exc


def _strip_fences(text: str) -> str:
    t = text.strip()
    if t.startswith("```"):
        t = t.split("\n", 1)[1] if "\n" in t else ""
        t = t.rsplit("```", 1)[0]
    return t.strip()


class OpenAICompatGateway:
    def __init__(self, *, base_url: str, api_key: str, model_fast: str, model_smart: str,
                 price_input_micro: int = 0, price_output_micro: int = 0,
                 client: httpx2.AsyncClient | None = None) -> None:
        self._url = f"{base_url.rstrip('/')}/chat/completions"
        self._key = api_key
        self._models = {HAIKU: model_fast, SONNET: model_smart, OPUS: model_smart}
        price = Price(input=price_input_micro, output=price_output_micro, cache_read=price_input_micro,
                      cache_write=price_input_micro)
        self._pricing = {tier: price for tier in self._models}
        self._client = client or httpx2.AsyncClient(timeout=httpx2.Timeout(120.0, connect=5.0))

    async def complete(self, req: ModelRequest) -> ModelResponse:
        if not self._key:
            raise PermanentModelError("AI_OPENAI_API_KEY is not set")
        model_id = self._models[req.model]
        body = build_body(req, model_id)
        started = time.monotonic()
        try:
            response = await self._client.post(self._url, json=body, headers={
                "Authorization": f"Bearer {self._key}", "Content-Type": "application/json"})
        except httpx2.HTTPError as exc:  # connect, read and timeout errors; `from None`: never chain request data
            raise TransientModelError(type(exc).__name__) from None
        payload = _json(response)
        if response.status_code == 429 or response.status_code >= 500:
            raise TransientModelError(f"HTTP {response.status_code}", retry_after_s=_retry_after(response))
        if response.status_code >= 400:
            detail = _provider_message(payload, self._key)
            raise PermanentModelError(f"HTTP {response.status_code}: {detail}" if detail
                                      else f"HTTP {response.status_code}")
        return self._to_response(req, model_id, payload, response, int((time.monotonic() - started) * 1000))

    def _to_response(self, req: ModelRequest, model_id: str, payload: Any, response: httpx2.Response,
                     latency_ms: int) -> ModelResponse:
        if not isinstance(payload, dict):
            raise TransientModelError("provider returned a body that is not JSON")
        if payload.get("error"):  # some gateways answer 200 with an error body
            code = payload["error"].get("code") if isinstance(payload["error"], dict) else None
            detail = _provider_message(payload, self._key)
            if isinstance(code, int) and code < 500 and code != 429:
                raise PermanentModelError(f"provider error {code}: {detail}")
            raise TransientModelError(f"provider error {code}")
        choices = payload.get("choices") or []
        if not choices or not isinstance(choices[0], dict):
            raise TransientModelError("provider returned no choices")
        usage = self._usage(req, payload.get("usage") or {})
        choice = choices[0]
        message = choice.get("message") or {}
        finish = choice.get("finish_reason") or "stop"
        if finish == "content_filter" or message.get("refusal"):
            raise _billed(ModelRefusal(finish if finish == "content_filter" else "refusal"), usage)
        stop = _FINISH_TO_STOP.get(finish, finish)
        if stop == "max_tokens":
            raise _billed(OutputInvalid(f"{req.prompt_id}: finish_reason length"), usage)
        text = message.get("content") or ""
        if not isinstance(text, str):  # list-of-parts content
            text = "".join(c.get("text", "") for c in text if isinstance(c, dict))
        calls = self._tool_calls(req, message, usage)
        if calls:
            stop = "tool_use"
        parsed = None
        if req.output_model is not None and stop != "tool_use":
            text = _strip_fences(text)
            try:
                parsed = req.output_model.model_validate_json(text)
            except ValidationError:
                name = req.output_model.__name__
                raise _billed(OutputInvalid(f"{req.prompt_id}: output does not match {name}"), usage) from None
        return ModelResponse(model=str(payload.get("model") or model_id), text=text, parsed=parsed,
                             tool_calls=calls, stop_reason=stop, usage=usage, latency_ms=latency_ms,
                             cache_key=cache_key(req), provider_request_id=str(payload.get("id") or
                                                                              response.headers.get("x-request-id", "")))

    @staticmethod
    def _tool_calls(req: ModelRequest, message: dict[str, Any], usage: Usage) -> tuple[ToolCall, ...]:
        out: list[ToolCall] = []
        for c in message.get("tool_calls") or []:
            fn = c.get("function") or {}
            try:
                args = json.loads(fn.get("arguments") or "{}")
            except ValueError:
                args = None
            if not isinstance(args, dict):
                raise _billed(OutputInvalid(f"{req.prompt_id}: tool call arguments are not a JSON object"), usage)
            out.append(ToolCall(str(c.get("id") or ""), str(fn.get("name") or ""), args))
        return tuple(out)

    def _usage(self, req: ModelRequest, u: dict[str, Any]) -> Usage:
        prompt, completion = int(u.get("prompt_tokens") or 0), int(u.get("completion_tokens") or 0)
        cached = int((u.get("prompt_tokens_details") or {}).get("cached_tokens") or 0)
        fresh = max(0, prompt - cached)
        return Usage(model=req.model, prompt_id=req.prompt_id, prompt_version=req.prompt_version,
                     input_tokens=fresh, output_tokens=completion, cache_read_input_tokens=cached,
                     cost_micro_usd=cost_micro_usd(req.model, input_tokens=fresh, output_tokens=completion,
                                                   cache_read_input_tokens=cached, pricing=self._pricing),
                     llm_calls=1)


def make_gateway(*, base_url: str, api_key: str, model_fast: str, model_smart: str,
                 price_input_micro: int = 0, price_output_micro: int = 0) -> OpenAICompatGateway:
    return OpenAICompatGateway(base_url=base_url, api_key=api_key, model_fast=model_fast,
                               model_smart=model_smart, price_input_micro=price_input_micro,
                               price_output_micro=price_output_micro)
