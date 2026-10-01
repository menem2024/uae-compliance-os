"""Deterministic gateways for tests, the compose default and the chaos test (contract section 2)."""

from __future__ import annotations

from collections.abc import Callable, Mapping, Sequence

from pydantic import BaseModel

from ai.gateway.errors import OutputInvalid, PermanentModelError
from ai.gateway.keys import cache_key
from ai.gateway.types import ModelRequest, ModelResponse, Usage

type FakeResult = ModelResponse | BaseModel | Exception
type FakeScript = Callable[[ModelRequest], FakeResult] | Mapping[str, Sequence[FakeResult]]

FAKE_INPUT_TOKENS = 1000
FAKE_OUTPUT_TOKENS = 200


def fake_usage(req: ModelRequest) -> Usage:
    return Usage(model=req.model, prompt_id=req.prompt_id, prompt_version=req.prompt_version,
                 input_tokens=FAKE_INPUT_TOKENS, output_tokens=FAKE_OUTPUT_TOKENS, cost_micro_usd=0,
                 llm_calls=1)


def response_for(req: ModelRequest, value: BaseModel | str, *, stop_reason: str = "end_turn") -> ModelResponse:
    if isinstance(value, BaseModel):
        text, parsed = value.model_dump_json(), value
    else:
        text = value
        parsed = req.output_model.model_validate_json(value) if req.output_model else None
    return ModelResponse(model=req.model, text=text, parsed=parsed, tool_calls=(), stop_reason=stop_reason,
                         usage=fake_usage(req), latency_ms=0, cache_key=cache_key(req))


class FakeGateway:
    """Scripted gateway. Records every request in .calls."""

    def __init__(self, script: FakeScript) -> None:
        self._script = script
        self._cursor: dict[str, int] = {}
        self.calls: list[ModelRequest] = []

    async def complete(self, req: ModelRequest) -> ModelResponse:
        self.calls.append(req)
        if callable(self._script):
            out = self._script(req)
        else:
            seq = self._script.get(req.prompt_id)
            if not seq:
                raise PermanentModelError(f"FakeGateway has no script for {req.prompt_id}")
            i = self._cursor.get(req.prompt_id, 0)
            self._cursor[req.prompt_id] = i + 1
            out = seq[min(i, len(seq) - 1)]
        if isinstance(out, Exception):
            raise out
        if isinstance(out, ModelResponse):
            return out
        if req.output_model is not None and not isinstance(out, req.output_model):
            raise OutputInvalid(f"fake output for {req.prompt_id} is not {req.output_model.__name__}")
        return response_for(req, out)
