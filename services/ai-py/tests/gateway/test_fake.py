import pytest
from pydantic import BaseModel

from ai.gateway.errors import OutputInvalid, PermanentModelError, TransientModelError
from ai.gateway.fake import FakeGateway
from ai.gateway.types import HAIKU, Message, ModelRequest, TextPart


class A(BaseModel):
    x: str = ""


class B(BaseModel):
    y: str = ""


def req(pid: str = "p.a", model=A) -> ModelRequest:
    return ModelRequest(model=HAIKU, prompt_id=pid, prompt_version=1, system="s",
                        messages=(Message("user", (TextPart("t"),)),), output_model=model)


async def test_sequence_script_advances_and_repeats_last():
    gw = FakeGateway({"p.a": [A(x="1"), TransientModelError("429"), A(x="3")]})
    assert (await gw.complete(req())).parsed == A(x="1")
    with pytest.raises(TransientModelError):
        await gw.complete(req())
    assert (await gw.complete(req())).parsed == A(x="3")
    assert (await gw.complete(req())).parsed == A(x="3")
    assert len(gw.calls) == 4


async def test_unknown_prompt_and_wrong_type():
    with pytest.raises(PermanentModelError):
        await FakeGateway({}).complete(req())
    with pytest.raises(OutputInvalid):
        await FakeGateway({"p.a": [B()]}).complete(req())


async def test_callable_script_and_usage():
    gw = FakeGateway(lambda r: A(x=r.prompt_id))
    resp = await gw.complete(req("p.z"))
    assert resp.parsed == A(x="p.z") and resp.usage.llm_calls == 1 and resp.usage.cost_micro_usd == 0
    assert len(resp.cache_key) == 64
