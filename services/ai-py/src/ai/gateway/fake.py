"""Deterministic gateways for tests, the compose default and the chaos test (contract section 2)."""

from __future__ import annotations

import asyncio
import json
from collections.abc import Awaitable, Callable, Mapping, Sequence
from pathlib import Path

from pydantic import BaseModel, ValidationError

from ai.gateway.errors import OutputInvalid, PermanentModelError
from ai.gateway.keys import cache_key
from ai.gateway.types import ModelRequest, ModelResponse, Usage
from ai.settings import Settings

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


DEFAULT_SCENARIO = Path(__file__).parent / "scenarios" / "default.json"


class ScenarioGateway:
    """AI_GATEWAY=fake: canned responses from a JSON scenario `{prompt_id: [response, ...]}`.

    A response is raw text (a JSON string) or a structured JSON value; each prompt id's responses are used
    in order and the last one repeats. AI_FAKE_LATENCY_MS is slept before every call. The packaged default
    answers every Phase 1 prompt id with a clean, high-confidence result that the Verifier accepts. The
    document consumer still publishes it as needs_review (AI_FAKE_RESULTS=review, the default), so canned
    output never lands as a clean invoice; the chaos test sets AI_FAKE_RESULTS=accept so every document
    reaches `extracted` deterministically. No call leaves the process.
    """

    def __init__(self, script: Mapping[str, Sequence[object]], *, latency_ms: int = 0,
                 sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
                 source: Path | None = None) -> None:
        if latency_ms < 0:
            raise ValueError("latency_ms must be >= 0")
        for prompt_id, responses in script.items():
            if not isinstance(responses, list | tuple) or not responses:
                raise ValueError(f"scenario {prompt_id}: expected a non-empty list of responses")
            if not all(isinstance(r, str | dict | list) for r in responses):
                raise ValueError(f"scenario {prompt_id}: a response is a string or a JSON object or array")
        self._script = {k: tuple(v) for k, v in script.items()}
        self._cursor: dict[str, int] = {}
        self._sleep = sleep
        self.latency_ms = latency_ms
        self.source = source

    @property
    def prompt_ids(self) -> tuple[str, ...]:
        return tuple(self._script)

    @classmethod
    def from_file(cls, path: str | Path, *, latency_ms: int = 0) -> ScenarioGateway:
        p = Path(path)
        data = json.loads(p.read_text(encoding="utf-8"))
        if not isinstance(data, dict) or not data:
            raise ValueError(f"{p}: a scenario is a non-empty JSON object of prompt_id -> [response, ...]")
        return cls(data, latency_ms=latency_ms, source=p)

    @classmethod
    def from_env(cls, settings: Settings | None = None) -> ScenarioGateway:
        """AI_FAKE_SCENARIO (empty: the packaged default) and AI_FAKE_LATENCY_MS."""
        s = settings if settings is not None else Settings.from_env()
        return cls.from_file(s.fake_scenario or DEFAULT_SCENARIO, latency_ms=s.fake_latency_ms)

    async def complete(self, req: ModelRequest) -> ModelResponse:
        if self.latency_ms:
            await self._sleep(self.latency_ms / 1000)
        responses = self._script.get(req.prompt_id)
        if not responses:
            raise PermanentModelError(f"scenario has no response for {req.prompt_id}")
        i = self._cursor.get(req.prompt_id, 0)
        self._cursor[req.prompt_id] = min(i + 1, len(responses) - 1)
        value = responses[i]
        text = value if isinstance(value, str) else json.dumps(value, ensure_ascii=False)
        try:
            return response_for(req, text)
        except ValidationError:
            name = req.output_model.__name__ if req.output_model else "the output model"
            raise OutputInvalid(f"scenario response for {req.prompt_id} does not match {name}") from None
