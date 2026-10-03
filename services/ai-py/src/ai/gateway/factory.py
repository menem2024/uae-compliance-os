"""The service's ModelGateway: Caching -> SpendLimited -> ConcurrencyLimited -> provider (contract section 2).

The provider is chosen by AI_GATEWAY: `anthropic` (the only live adapter), `replay` (recordings under
AI_RECORDINGS_DIR) or `fake` (ScenarioGateway, the compose default). AI_CACHE picks the backing store of both
the response cache and the per-Firm spend counters: `valkey` shares them across replicas through one client,
`memory` keeps them in the process, `none` drops the response cache (the spend cap stays, in memory).

Everything is lazy: no Valkey connection, provider call or API-key check happens here. A missing
ANTHROPIC_API_KEY or an unreachable Valkey surfaces on the first call, as a node failure the runtime handles.
"""

from __future__ import annotations

from typing import Any

from ai.gateway.cache import CachingGateway, MemoryCache, ResponseCache, ValkeyCache
from ai.gateway.fake import ScenarioGateway
from ai.gateway.limits import (
    ConcurrencyLimitedGateway,
    MemorySpendLimiter,
    SpendLimitedGateway,
    SpendLimiter,
    ValkeySpendLimiter,
)
from ai.gateway.recorded import ReplayGateway
from ai.gateway.types import ModelGateway
from ai.settings import Settings


def _provider(settings: Settings) -> ModelGateway:
    match settings.gateway:
        case "anthropic":
            from ai.gateway.anthropic_gw import make_gateway  # the SDK is imported only when it is used

            return make_gateway()
        case "replay":
            return ReplayGateway(settings.recordings_dir)
        case "fake":
            return ScenarioGateway.from_env(settings)
    raise ValueError(f"unknown AI_GATEWAY {settings.gateway!r}")


def _valkey(settings: Settings) -> Any:
    from redis.asyncio import Redis

    return Redis.from_url(settings.valkey_url)  # connects on first command


def build_gateway(settings: Settings) -> ModelGateway:
    client = _valkey(settings) if settings.cache == "valkey" else None
    cap = settings.daily_spend_cap_micro_usd
    limiter: SpendLimiter = (ValkeySpendLimiter(cap, client=client) if client is not None
                             else MemorySpendLimiter(cap))
    gw: ModelGateway = ConcurrencyLimitedGateway(_provider(settings), settings.max_concurrent_llm_calls)
    gw = SpendLimitedGateway(gw, limiter)
    if settings.cache == "none":
        return gw
    cache: ResponseCache = ValkeyCache(client=client) if client is not None else MemoryCache()
    return CachingGateway(gw, cache)
