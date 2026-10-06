"""The service's ModelGateway: Caching -> ConcurrencyLimited -> SpendLimited -> provider (contract section 2).

The spend cap sits inside the concurrency limit (review B #2): a call reserves its cost only once it holds a
semaphore slot, so queued calls neither pass the cap check together nor hold reservations while they wait.
Only the fake gateway may run without the spend limiter's store (`live=False`); every other provider fails
closed when Valkey is down.

The provider is chosen by AI_GATEWAY (alias AI_PROVIDER): `anthropic`, `openai_compat` (any OpenAI-compatible
chat-completions endpoint, AI_OPENAI_*; live like anthropic: same spend cap, fail-closed limiter and
cache layers), `replay` (recordings under
AI_RECORDINGS_DIR) or `fake` (ScenarioGateway, the compose default). AI_CACHE picks the backing store of both
the response cache and the per-Firm spend counters: `valkey` shares them across replicas through one client,
`memory` keeps them in the process, `none` drops the response cache (the spend cap stays, in memory).

Everything is lazy: no Valkey connection, provider call or API-key check happens here. A missing
ANTHROPIC_API_KEY (or AI_OPENAI_API_KEY) or an unreachable Valkey surfaces on the first call, as a node
failure the runtime handles.
"""

from __future__ import annotations

from typing import Any

from ai.gateway.cache import VALKEY_PREFIX, CachingGateway, MemoryCache, ResponseCache, ValkeyCache
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
        case "openai_compat":
            from ai.gateway.openai_compat_gw import make_gateway as make_openai_gateway

            return make_openai_gateway(
                base_url=settings.openai_base_url, api_key=settings.openai_api_key,
                model_fast=settings.openai_model_fast, model_smart=settings.openai_model_smart,
                price_input_micro=settings.openai_price_input_micro,
                price_output_micro=settings.openai_price_output_micro)
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
    gw: ModelGateway = SpendLimitedGateway(_provider(settings), limiter, live=settings.gateway != "fake")
    gw = ConcurrencyLimitedGateway(gw, settings.max_concurrent_llm_calls)
    if settings.cache == "none":
        return gw
    # Canned fake responses live under their own prefix: after a switch to a live gateway, a reprocessed
    # document must never be served a cached fake extraction (Valkey keeps entries for 30 days).
    # The same goes for openai_compat: its cache key (tier, prompt, input) matches Anthropic's, but its answers
    # come from another model and must never be served to, or from, an Anthropic-backed run.
    prefix = (f"{VALKEY_PREFIX}{settings.gateway}:" if settings.gateway in ("fake", "openai_compat")
              else VALKEY_PREFIX)
    cache: ResponseCache = ValkeyCache(client=client, prefix=prefix) if client is not None else MemoryCache()
    return CachingGateway(gw, cache)
