import pytest
from pydantic import BaseModel

from ai.gateway.types import ToolCall, ToolSpec
from ai.runtime.errors import ToolNotAllowed, ToolTransientError
from ai.runtime.tools import ToolRegistry, tool


class Args(BaseModel):
    trn: str


@tool(name="lookup_trn", version=1, description="d", side_effects="read")
async def lookup(ctx, args: Args) -> str:
    if args.trn == "boom":
        raise ValueError("backend said a.b@x.ae is invalid")
    if args.trn == "flaky":
        raise ToolTransientError("try again")
    return f"ok {args.trn}"


class Ctx:
    run = None

    def emit(self, message_key: str, **args: str) -> None:
        pass


def test_decorator_reads_input_model():
    assert lookup.spec == ToolSpec("lookup_trn", 1, "d", Args, "read")


def test_register_rejects_writes_bad_names_and_duplicates():
    reg = ToolRegistry([lookup])
    with pytest.raises(ValueError):
        reg.register(lookup)

    class Writer:
        spec = ToolSpec("writer", 1, "d", Args, "write")  # type: ignore[arg-type]

    with pytest.raises(ValueError):
        reg.register(Writer())  # type: ignore[arg-type]


def test_scoped_allowlist():
    reg = ToolRegistry([lookup])
    assert reg.scoped([]).specs() == ()
    assert reg.scoped(["lookup_trn"]).get("lookup_trn") is lookup
    with pytest.raises(ToolNotAllowed):
        reg.scoped(["nope"])


async def test_invoke_ok_error_and_transient():
    reg = ToolRegistry([lookup])
    ok = await reg.invoke(Ctx(), ToolCall("t1", "lookup_trn", {"trn": "1"}))
    assert (ok.content, ok.is_error) == ("ok 1", False)
    bad_input = await reg.invoke(Ctx(), ToolCall("t2", "lookup_trn", {"nope": 1}))
    assert bad_input.is_error
    err = await reg.invoke(Ctx(), ToolCall("t3", "lookup_trn", {"trn": "boom"}))
    assert err.is_error and "a.b@x.ae" not in err.content and "[email]" in err.content
    unknown = await reg.invoke(Ctx(), ToolCall("t4", "other", {}))
    assert unknown.is_error
    with pytest.raises(ToolTransientError):
        await reg.invoke(Ctx(), ToolCall("t5", "lookup_trn", {"trn": "flaky"}))
