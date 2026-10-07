"""Record live responses for evals and replay them in CI (contract section 2, spec section 5.6)."""

from __future__ import annotations

import json
import os
import tempfile
from datetime import UTC, datetime
from pathlib import Path

from pydantic import ValidationError

from ai.canonical import canonical_json
from ai.gateway.errors import OutputInvalid, RecordingMissing
from ai.gateway.keys import cache_key
from ai.gateway.pricing import PRICING, Price, cost_micro_usd
from ai.gateway.types import ModelGateway, ModelRequest, ModelResponse, ToolCall, Usage


def recording_path(root: Path, req: ModelRequest) -> tuple[Path, str]:
    key = cache_key(req)
    return root / req.prompt_id / f"{key}.json", key


def _atomic_write(path: Path, data: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix=".rec-")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write(data)
    Path(tmp).replace(path)


class RecordingGateway:
    """Live calls through `inner`; writes root/<prompt_id>/<cache_key>.json. Never writes document bytes."""

    def __init__(self, inner: ModelGateway, root: str | Path) -> None:
        self._inner = inner
        self._root = Path(root)

    async def complete(self, req: ModelRequest) -> ModelResponse:
        resp = await self._inner.complete(req)
        path, key = recording_path(self._root, req)
        u = resp.usage
        _atomic_write(path, canonical_json({
            "v": 1, "prompt_id": req.prompt_id, "prompt_version": req.prompt_version, "cache_key": key,
            "model": resp.model, "text": resp.text, "stop_reason": resp.stop_reason,
            "tool_calls": [[c.id, c.name, dict(c.input)] for c in resp.tool_calls],
            "usage": {"input_tokens": u.input_tokens, "output_tokens": u.output_tokens,
                      "cache_read_input_tokens": u.cache_read_input_tokens,
                      "cache_creation_input_tokens": u.cache_creation_input_tokens},
            "recorded_at": datetime.now(UTC).isoformat(timespec="seconds"),
        }) + "\n")
        return resp


class ReplayGateway:
    """Reads recordings; a missing key raises RecordingMissing. Cost is recomputed from recorded usage."""

    def __init__(self, root: str | Path, pricing: dict[str, Price] = PRICING) -> None:
        self._root = Path(root)
        self._pricing = pricing

    async def complete(self, req: ModelRequest) -> ModelResponse:
        path, key = recording_path(self._root, req)
        if not path.is_file():
            raise RecordingMissing(req.prompt_id, key)
        d = json.loads(path.read_text(encoding="utf-8"))
        parsed = None
        if req.output_model is not None and d["stop_reason"] != "tool_use":
            try:
                parsed = req.output_model.model_validate_json(d["text"])
            except ValidationError:
                raise OutputInvalid(f"{req.prompt_id}: recorded output does not match schema") from None
        t = d["usage"]
        usage = Usage(model=req.model, prompt_id=req.prompt_id, prompt_version=req.prompt_version,
                      input_tokens=t["input_tokens"], output_tokens=t["output_tokens"],
                      cache_read_input_tokens=t["cache_read_input_tokens"],
                      cache_creation_input_tokens=t["cache_creation_input_tokens"],
                      cost_micro_usd=cost_micro_usd(req.model, pricing=self._pricing, **t), llm_calls=1)
        calls = tuple(ToolCall(c[0], c[1], c[2]) for c in d["tool_calls"])
        return ModelResponse(model=d["model"], text=d["text"], parsed=parsed, tool_calls=calls,
                             stop_reason=d["stop_reason"], usage=usage, latency_ms=0, cache_key=key)
