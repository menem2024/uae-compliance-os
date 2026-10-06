import base64
import json

import httpx2
import pytest
from pydantic import BaseModel, ConfigDict

from ai.gateway.errors import ModelRefusal, OutputInvalid, PermanentModelError, TransientModelError
from ai.gateway.openai_compat_gw import OpenAICompatGateway
from ai.gateway.types import (
    HAIKU,
    OPUS,
    SONNET,
    DocumentPart,
    ImagePart,
    Message,
    ModelRequest,
    TextPart,
    ToolResultPart,
    ToolSpec,
    ToolUsePart,
)

KEY = "unit-test-key-0001"
PNG = b"\x89PNG synthetic"


class Out(BaseModel):
    model_config = ConfigDict(extra="forbid")
    number: str = ""


class LookupArgs(BaseModel):
    q: str = ""


def req(model=SONNET, output_model=Out, tools=(), messages=None) -> ModelRequest:
    msgs = messages or (Message("user", (TextPart("read this"), ImagePart.of(PNG, "image/png"))),)
    return ModelRequest(model=model, prompt_id="extraction.invoice", prompt_version=1, system="sys",
                        messages=msgs, output_model=output_model, tools=tools, max_tokens=512)


def completion(content='{"number": "INV-1"}', finish="stop", usage=None, message=None):
    return {"id": "gen-1", "model": "vendor/model:free",
            "choices": [{"index": 0, "finish_reason": finish,
                         "message": message or {"role": "assistant", "content": content}}],
            "usage": usage or {"prompt_tokens": 1000, "completion_tokens": 200,
                               "prompt_tokens_details": {"cached_tokens": 400}}}


class Server:
    def __init__(self, status=200, body=None, headers=None, exc=None):
        self.status, self.body, self.headers, self.exc = status, body, headers or {}, exc
        self.requests: list[httpx2.Request] = []

    def __call__(self, request: httpx2.Request) -> httpx2.Response:
        self.requests.append(request)
        if self.exc:
            raise self.exc
        return httpx2.Response(self.status, json=self.body, headers=self.headers)

    def gateway(self, **kw) -> OpenAICompatGateway:
        client = httpx2.AsyncClient(transport=httpx2.MockTransport(self))
        cfg = {"base_url": "https://llm.example/v1/", "api_key": KEY, "model_fast": "fast-m",
               "model_smart": "smart-m", **kw}
        return OpenAICompatGateway(client=client, **cfg)

    @property
    def sent(self) -> dict:
        return json.loads(self.requests[-1].content)


async def test_success_parses_structured_output_and_usage():
    srv = Server(body=completion())
    resp = await srv.gateway().complete(req())
    assert resp.parsed == Out(number="INV-1") and resp.stop_reason == "end_turn"
    assert resp.model == "vendor/model:free" and resp.provider_request_id == "gen-1"
    u = resp.usage
    assert (u.input_tokens, u.output_tokens, u.cache_read_input_tokens, u.llm_calls) == (600, 200, 400, 1)
    assert u.cost_micro_usd == 0  # free tier: no price configured
    r = srv.requests[0]
    assert str(r.url) == "https://llm.example/v1/chat/completions"
    assert r.headers["authorization"] == f"Bearer {KEY}"
    fmt = srv.sent["response_format"]
    assert fmt["type"] == "json_schema" and fmt["json_schema"]["schema"]["additionalProperties"] is False
    assert srv.sent["max_tokens"] == 512 and srv.sent["messages"][0]["role"] == "system"


async def test_configured_price_gives_cost():
    srv = Server(body=completion())
    resp = await srv.gateway(price_input_micro=1_000_000, price_output_micro=2_000_000).complete(req())
    assert resp.usage.cost_micro_usd == 600 + 400 + 400 * 1  # 600 in + 400 out + 400 cached at input price


@pytest.mark.parametrize(("tier", "model"), [(HAIKU, "fast-m"), (SONNET, "smart-m"), (OPUS, "smart-m")])
async def test_tiers_map_to_configured_models(tier, model):
    srv = Server(body=completion())
    await srv.gateway().complete(req(model=tier))
    assert srv.sent["model"] == model


async def test_fenced_json_is_accepted():
    srv = Server(body=completion('```json\n{"number": "INV-2"}\n```'))
    assert (await srv.gateway().complete(req())).parsed == Out(number="INV-2")


async def test_schema_mismatch_is_output_invalid_and_billed_without_the_raw_output():
    srv = Server(body=completion('{"number": 1, "extra": "secret-pii"}'))
    with pytest.raises(OutputInvalid) as ei:
        await srv.gateway().complete(req())
    assert "secret-pii" not in str(ei.value) and ei.value.usage is not None and ei.value.usage.llm_calls == 1


async def test_length_finish_is_output_invalid():
    with pytest.raises(OutputInvalid):
        await Server(body=completion("{", finish="length")).gateway().complete(req())


async def test_refusal_and_content_filter():
    with pytest.raises(ModelRefusal):
        await Server(body=completion(finish="content_filter")).gateway().complete(req())
    msg = {"role": "assistant", "content": None, "refusal": "no"}
    with pytest.raises(ModelRefusal):
        await Server(body=completion(message=msg)).gateway().complete(req())


async def test_429_is_transient_with_retry_after():
    srv = Server(status=429, body={"error": {"message": "slow down"}}, headers={"Retry-After": "7"})
    with pytest.raises(TransientModelError) as ei:
        await srv.gateway().complete(req())
    assert ei.value.retry_after_s == 7.0


@pytest.mark.parametrize("status", [500, 502, 503])
async def test_5xx_is_transient(status):
    with pytest.raises(TransientModelError) as ei:
        await Server(status=status, body={}).gateway().complete(req())
    assert ei.value.retry_after_s is None


@pytest.mark.parametrize("exc", [httpx2.ConnectError("boom"), httpx2.ReadTimeout("slow")])
async def test_connect_and_timeout_are_transient(exc):
    with pytest.raises(TransientModelError):
        await Server(exc=exc).gateway().complete(req())


async def test_401_is_permanent_and_never_leaks_the_key_or_the_request():
    srv = Server(status=401, body={"error": {"message": f"bad key {KEY} for user a@b.com"}})
    with pytest.raises(PermanentModelError) as ei:
        await srv.gateway().complete(req())
    text = str(ei.value)
    assert KEY not in text and "a@b.com" not in text and "401" in text
    assert "read this" not in text


async def test_error_body_inside_a_200_maps_by_code():
    with pytest.raises(TransientModelError):
        await Server(body={"error": {"code": 429, "message": "x"}}).gateway().complete(req())
    with pytest.raises(PermanentModelError):
        await Server(body={"error": {"code": 400, "message": "x"}}).gateway().complete(req())
    with pytest.raises(TransientModelError):
        await Server(body={"choices": []}).gateway().complete(req())


async def test_missing_key_is_permanent_and_makes_no_call():
    srv = Server(body=completion())
    with pytest.raises(PermanentModelError, match="AI_OPENAI_API_KEY"):
        await srv.gateway(api_key="").complete(req())
    assert srv.requests == []


async def test_image_and_pdf_encoding_and_binary_before_text():
    pdf = b"%PDF-1.7 synthetic"
    msgs = (Message("user", (TextPart("read"), DocumentPart.of(pdf), ImagePart.of(PNG, "image/png"))),)
    srv = Server(body=completion())
    await srv.gateway().complete(req(messages=msgs))
    content = srv.sent["messages"][1]["content"]
    assert [c["type"] for c in content] == ["file", "image_url", "text"]
    img = next(c for c in content if c["type"] == "image_url")["image_url"]["url"]
    assert img == "data:image/png;base64," + base64.b64encode(PNG).decode()
    f = next(c for c in content if c["type"] == "file")["file"]
    assert f["file_data"] == "data:application/pdf;base64," + base64.b64encode(pdf).decode()


async def test_tools_roundtrip():
    tool = ToolSpec("lookup", 1, "look it up", LookupArgs, "read")
    msg = {"role": "assistant", "content": None,
           "tool_calls": [{"id": "c1", "type": "function",
                           "function": {"name": "lookup", "arguments": '{"q": "trn"}'}}]}
    srv = Server(body=completion(finish="tool_calls", message=msg))
    resp = await srv.gateway().complete(req(tools=(tool,), output_model=None))
    assert resp.stop_reason == "tool_use" and resp.parsed is None
    assert [(c.id, c.name, dict(c.input)) for c in resp.tool_calls] == [("c1", "lookup", {"q": "trn"})]
    t = srv.sent["tools"][0]["function"]
    assert t["name"] == "lookup" and t["parameters"]["additionalProperties"] is False

    follow = (Message("user", (TextPart("go"),)),
              Message("assistant", (ToolUsePart("c1", "lookup", {"q": "trn"}),)),
              Message("user", (ToolResultPart("c1", "found"),)))
    srv2 = Server(body=completion())
    await srv2.gateway().complete(req(messages=follow))
    roles = [m["role"] for m in srv2.sent["messages"]]
    assert roles == ["system", "user", "assistant", "tool"]
    assert srv2.sent["messages"][2]["tool_calls"][0]["function"]["arguments"] == '{"q": "trn"}'
    assert srv2.sent["messages"][3] == {"role": "tool", "tool_call_id": "c1", "content": "found"}
