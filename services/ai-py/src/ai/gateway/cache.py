"""Gateway response cache (contract section 2): a hit costs nothing and makes no provider call (AC-E3b)."""

from __future__ import annotations

import json
import logging
from collections import OrderedDict
from typing import Any, Protocol

from pydantic import ValidationError

from ai.canonical import canonical_json
from ai.gateway.keys import cache_key
from ai.gateway.types import ModelGateway, ModelRequest, ModelResponse, ToolCall, Usage

log = logging.getLogger(__name__)

VALKEY_PREFIX = "llmcache:v1:"


class ResponseCache(Protocol):
    async def get(self, key: str) -> bytes | None: ...
    async def set(self, key: str, value: bytes) -> None: ...


class MemoryCache:
    """Process-local LRU cache (tests, compose default)."""

    def __init__(self, max_entries: int = 10_000) -> None:
        self._max = max_entries
        self._data: OrderedDict[str, bytes] = OrderedDict()

    async def get(self, key: str) -> bytes | None:
        value = self._data.get(key)
        if value is not None:
            self._data.move_to_end(key)
        return value

    async def set(self, key: str, value: bytes) -> None:
        self._data[key] = value
        self._data.move_to_end(key)
        while len(self._data) > self._max:
            self._data.popitem(last=False)


class ValkeyCache:
    """Shared cache in Valkey, key llmcache:v1:<cache_key>. `client` is for tests (fakeredis)."""

    def __init__(self, url: str = "", ttl_s: int = 30 * 86400, *, client: Any = None) -> None:
        if client is None:
            from redis.asyncio import Redis

            client = Redis.from_url(url)
        self._r = client
        self._ttl = ttl_s

    async def get(self, key: str) -> bytes | None:
        value = await self._r.get(VALKEY_PREFIX + key)
        return bytes(value) if value is not None else None

    async def set(self, key: str, value: bytes) -> None:
        await self._r.set(VALKEY_PREFIX + key, value, ex=self._ttl)

    async def aclose(self) -> None:
        await self._r.aclose()


def dump_response(resp: ModelResponse) -> bytes:
    """What the cache stores: the response text and shape, never request content or document bytes."""
    return canonical_json({
        "v": 1, "model": resp.model, "text": resp.text, "stop_reason": resp.stop_reason,
        "tool_calls": [[c.id, c.name, dict(c.input)] for c in resp.tool_calls],
    }).encode()


def load_response(req: ModelRequest, raw: bytes, key: str) -> ModelResponse:
    """Rebuilds a hit. Raises ValueError/ValidationError on a corrupt or stale entry (treated as a miss)."""
    d = json.loads(raw)
    if d.get("v") != 1:
        raise ValueError("unknown cache entry version")
    stop = d["stop_reason"]
    parsed = None
    if req.output_model is not None and stop != "tool_use":
        parsed = req.output_model.model_validate_json(d["text"])
    usage = Usage(model=req.model, prompt_id=req.prompt_id, prompt_version=req.prompt_version,
                  response_cache_hit=True, llm_calls=0, cost_micro_usd=0)
    calls = tuple(ToolCall(c[0], c[1], c[2]) for c in d["tool_calls"])
    return ModelResponse(model=d["model"], text=d["text"], parsed=parsed, tool_calls=calls, stop_reason=stop,
                         usage=usage, latency_ms=0, cache_key=key)


class CachingGateway:
    """Outermost gateway layer. Cache failures never fail a call: they are logged and treated as misses."""

    def __init__(self, inner: ModelGateway, cache: ResponseCache) -> None:
        self._inner = inner
        self._cache = cache

    async def complete(self, req: ModelRequest) -> ModelResponse:
        if not req.cacheable:
            return await self._inner.complete(req)
        key = cache_key(req)
        try:
            raw = await self._cache.get(key)
        except Exception:  # a cache outage must not stop extraction
            log.warning("response cache get failed prompt_id=%s", req.prompt_id, exc_info=True)
            raw = None
        if raw is not None:
            try:
                return load_response(req, raw, key)
            except (ValueError, KeyError, IndexError, ValidationError):
                log.warning("discarding unreadable cache entry prompt_id=%s", req.prompt_id)
        resp = await self._inner.complete(req)
        try:
            await self._cache.set(key, dump_response(resp))
        except Exception:
            log.warning("response cache set failed prompt_id=%s", req.prompt_id, exc_info=True)
        return resp
