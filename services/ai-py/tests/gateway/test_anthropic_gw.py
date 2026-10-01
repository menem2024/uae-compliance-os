import base64

import anthropic
import httpx2
import pytest
from anthropic.types import Message as SdkMessage
from pydantic import BaseModel, ConfigDict

from ai.gateway.anthropic_gw import AnthropicGateway, build_kwargs, make_client
from ai.gateway.errors import ModelRefusal, OutputInvalid, PermanentModelError, TransientModelError
from ai.gateway.types import (
    HAIKU,
    OPUS,
    SONNET,
    DocumentPart,
    Message,
    ModelRequest,
    TextPart,
    ToolSpec,
)


class Out(BaseModel):
    model_config = ConfigDict(extra="forbid")
    number: str = ""


class LookupArgs(BaseModel):
    q: str = ""


PDF = b"%PDF-1.7 synthetic"


def req(model=SONNET, effort=None, output_model=Out, tools=()) -> ModelRequest:
    return ModelRequest(model=model, prompt_id="extraction.invoice", prompt_version=1, system="sys",
                        messages=(Message("user", (TextPart("read this"), DocumentPart.of(PDF))),),
                        output_model=output_model, tools=tools, max_tokens=512, effort=effort)


def sdk_message(content, stop="end_turn", stop_details=None, usage=None):
    return SdkMessage.model_validate({
        "id": "msg_1", "type": "message", "role": "assistant", "model": "claude-sonnet-5", "content": content,
        "stop_reason": stop, "stop_details": stop_details, "stop_sequence": None,
        "usage": usage or {"input_tokens": 1000, "output_tokens": 200, "cache_read_input_tokens": 500,
                           "cache_creation_input_tokens": None}})


class StubMessages:
    def __init__(self, result):
        self.result = result
        self.kwargs = None

    async def create(self, **kwargs):
        self.kwargs = kwargs
        if isinstance(self.result, Exception):
            raise self.result
        return self.result


class StubClient:
    def __init__(self, result):
        self.messages = StubMessages(result)


def test_request_shape_sonnet_structured():
    k = build_kwargs(req(effort="low"))
    assert set(k) == {"model", "max_tokens", "system", "messages", "cache_control", "output_config"}
    assert k["cache_control"] == {"type": "ephemeral"}
    assert k["output_config"]["effort"] == "low"
    fmt = k["output_config"]["format"]
    assert fmt["type"] == "json_schema" and fmt["schema"]["additionalProperties"] is False
    blocks = k["messages"][0]["content"]
    assert [b["type"] for b in blocks] == ["document", "text"]  # binary parts first
    assert base64.b64decode(blocks[0]["source"]["data"]) == PDF
    for banned in ("temperature", "top_p", "top_k", "thinking"):
        assert banned not in k


def test_effort_rules_per_model():
    assert "effort" not in build_kwargs(req(model=HAIKU, effort="low"))["output_config"]
    assert build_kwargs(req(model=OPUS))["output_config"]["effort"] == "medium"
    assert "output_config" not in build_kwargs(req(model=SONNET, output_model=None))


def test_tools_are_strict_with_auto_choice():
    spec = ToolSpec(name="lookup", version=1, description="d", input_model=LookupArgs, side_effects="read")
    k = build_kwargs(req(tools=(spec,), output_model=None))
    assert k["tools"][0]["strict"] is True and k["tool_choice"] == {"type": "auto"}
    assert k["tools"][0]["input_schema"]["required"] == ["q"]


async def test_response_parsing_usage_and_cost():
    msg = sdk_message([{"type": "thinking", "thinking": "", "signature": "s"},
                       {"type": "text", "text": '{"number": "INV-7"}'}])
    resp = await AnthropicGateway(StubClient(msg)).complete(req())
    assert resp.parsed == Out(number="INV-7") and resp.stop_reason == "end_turn"
    u = resp.usage
    assert (u.input_tokens, u.output_tokens, u.cache_read_input_tokens, u.cache_creation_input_tokens) == (
        1000, 200, 500, 0)
    assert u.cost_micro_usd == 2000 + 2000 + 100 and u.llm_calls == 1 and not u.response_cache_hit
    assert len(resp.cache_key) == 64


async def test_tool_use_blocks_become_tool_calls():
    msg = sdk_message([{"type": "tool_use", "id": "tu1", "name": "lookup", "input": {"q": "x"}}], stop="tool_use")
    resp = await AnthropicGateway(StubClient(msg)).complete(req(output_model=Out))
    assert resp.parsed is None and resp.tool_calls[0].name == "lookup" and resp.tool_calls[0].input == {"q": "x"}


async def test_refusal_max_tokens_and_bad_json():
    refusal = sdk_message([], stop="refusal", stop_details={"type": "refusal", "category": "cyber"})
    with pytest.raises(ModelRefusal) as ei:
        await AnthropicGateway(StubClient(refusal)).complete(req())
    assert ei.value.category == "cyber"
    with pytest.raises(OutputInvalid):
        await AnthropicGateway(StubClient(sdk_message([{"type": "text", "text": "{"}], stop="max_tokens"))).complete(req())
    secret = '{"nope": "a.b@x.ae"}'
    with pytest.raises(OutputInvalid) as ei2:
        await AnthropicGateway(StubClient(sdk_message([{"type": "text", "text": secret}]))).complete(req())
    assert "a.b@x.ae" not in str(ei2.value)


def _status_error(cls, status, message="boom", headers=None):
    request = httpx2.Request("POST", "https://api.anthropic.com/v1/messages")
    return cls(message, response=httpx2.Response(status, headers=headers or {}, request=request), body=None)


@pytest.mark.parametrize(("exc", "expected", "retry_after"), [
    (_status_error(anthropic.RateLimitError, 429, headers={"retry-after": "7"}), TransientModelError, 7.0),
    (_status_error(anthropic.OverloadedError, 529), TransientModelError, None),
    (_status_error(anthropic.InternalServerError, 500), TransientModelError, None),
    (anthropic.APITimeoutError(request=httpx2.Request("POST", "https://api.anthropic.com")), TransientModelError, None),
    (_status_error(anthropic.BadRequestError, 400, message="bad a.b@x.ae"), PermanentModelError, None),
    (_status_error(anthropic.AuthenticationError, 401), PermanentModelError, None),
])
async def test_error_mapping(exc, expected, retry_after):
    with pytest.raises(expected) as ei:
        await AnthropicGateway(StubClient(exc)).complete(req())
    assert "a.b@x.ae" not in str(ei.value)
    assert ei.value.__cause__ is None and ei.value.__suppress_context__
    if expected is TransientModelError:
        assert ei.value.retry_after_s == retry_after


def test_client_never_retries_itself():
    c = make_client("sk-test-not-a-key")
    assert c.max_retries == 0 and c.timeout.connect == 5.0 and c.timeout.read == 120.0
