"""Tools and the per-agent ToolRegistry (contract section 3.3). Tools never write."""

import asyncio
import inspect
import re
from collections.abc import Awaitable, Callable, Iterable
from typing import Literal, Protocol, get_type_hints

from pydantic import BaseModel

from ai.gateway.redact import redact
from ai.gateway.types import ToolCall, ToolResultPart, ToolSpec
from ai.runtime.errors import ToolNotAllowed, ToolTransientError
from ai.runtime.types import RunIdentity

_TOOL_NAME = re.compile(r"^[a-z][a-z0-9_]{1,63}$")
_ALLOWED_EFFECTS = frozenset({"none", "read", "propose"})


class ToolContext(Protocol):
    run: RunIdentity

    def emit(self, message_key: str, **args: str) -> None: ...


class Tool(Protocol):
    spec: ToolSpec

    async def __call__(self, ctx: ToolContext, args: BaseModel) -> BaseModel | str: ...


type ToolFn = Callable[[ToolContext, BaseModel], Awaitable[BaseModel | str]]


class _FnTool:
    def __init__(self, spec: ToolSpec, fn: ToolFn) -> None:
        self.spec = spec
        self._fn = fn

    async def __call__(self, ctx: ToolContext, args: BaseModel) -> BaseModel | str:
        return await self._fn(ctx, args)


def tool(*, name: str, version: int, description: str, side_effects: Literal["none", "read", "propose"],
         timeout_s: float = 30.0) -> Callable[[ToolFn], Tool]:
    def wrap(fn: ToolFn) -> Tool:
        hints = get_type_hints(fn)
        params = list(inspect.signature(fn).parameters)
        if len(params) != 2 or params[1] not in hints:
            raise TypeError("a tool function takes (ctx, args: <pydantic model>)")
        input_model = hints[params[1]]
        if not (isinstance(input_model, type) and issubclass(input_model, BaseModel)):
            raise TypeError("tool args must be annotated with a pydantic model")
        spec = ToolSpec(name=name, version=version, description=description, input_model=input_model,
                        side_effects=side_effects, timeout_s=timeout_s)
        return _FnTool(spec, fn)

    return wrap


class ToolRegistry:
    def __init__(self, tools: Iterable[Tool] = ()) -> None:
        self._tools: dict[str, Tool] = {}
        for t in tools:
            self.register(t)

    def register(self, tool: Tool) -> None:
        spec = tool.spec
        if not _TOOL_NAME.match(spec.name):
            raise ValueError(f"invalid tool name {spec.name!r}")
        if spec.side_effects not in _ALLOWED_EFFECTS:
            raise ValueError(f"tool {spec.name}: side_effects must be none, read or propose")
        if spec.name in self._tools:
            raise ValueError(f"duplicate tool {spec.name}")
        self._tools[spec.name] = tool

    def get(self, name: str) -> Tool:
        try:
            return self._tools[name]
        except KeyError:
            raise ToolNotAllowed(name) from None

    def scoped(self, allowed: Iterable[str]) -> ToolRegistry:
        view = ToolRegistry()
        for name in allowed:
            view._tools[name] = self.get(name)
        return view

    def specs(self) -> tuple[ToolSpec, ...]:
        return tuple(t.spec for t in self._tools.values())

    async def invoke(self, ctx: ToolContext, call: ToolCall) -> ToolResultPart:
        try:
            t = self.get(call.name)
            args = t.spec.input_model.model_validate(dict(call.input))
            async with asyncio.timeout(t.spec.timeout_s):
                out = await t(ctx, args)
        except ToolTransientError:
            raise
        except Exception as exc:  # noqa: BLE001 - becomes an is_error tool result the model can read
            return ToolResultPart(tool_use_id=call.id, content=redact(f"{type(exc).__name__}: {exc}"),
                                  is_error=True)
        content = out if isinstance(out, str) else out.model_dump_json()
        return ToolResultPart(tool_use_id=call.id, content=content)
