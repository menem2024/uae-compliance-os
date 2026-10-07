"""build_gateway: Caching -> ConcurrencyLimited -> SpendLimited -> provider, chosen by AI_GATEWAY."""

import asyncio
import dataclasses
from pathlib import Path

import pytest

from ai.agents.intake import agent as intake_agent
from ai.agents.intake.schema import IntakeResult
from ai.gateway import factory
from ai.gateway.anthropic_gw import AnthropicGateway
from ai.gateway.cache import CachingGateway, MemoryCache, ValkeyCache
from ai.gateway.errors import SpendCapExceeded, SpendLimiterUnavailable
from ai.gateway.factory import build_gateway
from ai.gateway.fake import DEFAULT_SCENARIO, ScenarioGateway, response_for
from ai.gateway.limits import (
    ConcurrencyLimitedGateway,
    MemorySpendLimiter,
    SpendLimitedGateway,
    ValkeySpendLimiter,
)
from ai.gateway.pacing import ResilientGateway
from ai.gateway.recorded import ReplayGateway
from ai.gateway.types import HAIKU, SONNET, Message, ModelRequest, RequestMeta, TextPart
from ai.settings import Settings

UNREACHABLE_VALKEY = "redis://127.0.0.1:1/0"  # loopback, nothing listens: connection refused


def layers(gw: object) -> list[object]:
    out = [gw]
    while (inner := getattr(out[-1], "_inner", None)) is not None:
        out.append(inner)
    return out


def intake_req() -> ModelRequest:
    return ModelRequest(model=HAIKU, prompt_id=intake_agent.PROMPT_ID, prompt_version=1, system="s",
                        messages=(Message("user", (TextPart("t"),)),), output_model=IntakeResult,
                        meta=RequestMeta("firm-1", "run-1", "step-1"))


def test_fake_builds_the_full_stack_over_the_scenario_gateway(monkeypatch):
    monkeypatch.delenv("AI_FAKE_SCENARIO", raising=False)
    s = Settings(gateway="fake", cache="memory", daily_spend_cap_micro_usd=1234, max_concurrent_llm_calls=3)
    stack = layers(build_gateway(s))
    assert [type(x) for x in stack] == [CachingGateway, ConcurrencyLimitedGateway, SpendLimitedGateway,
                                        ScenarioGateway]
    caching, conc, spend, provider = stack
    assert isinstance(caching._cache, MemoryCache)  # type: ignore[attr-defined]
    limiter = spend._limiter  # type: ignore[attr-defined]
    assert isinstance(limiter, MemorySpendLimiter) and limiter.cap_micro_usd == 1234
    assert conc._sem._value == 3  # type: ignore[attr-defined]
    assert provider.source == DEFAULT_SCENARIO  # type: ignore[attr-defined]


def test_fake_uses_the_settings_scenario(tmp_path):
    path = tmp_path / "s.json"
    path.write_text('{"p.a": ["x"]}', encoding="utf-8")
    provider = layers(build_gateway(Settings(gateway="fake", fake_scenario=str(path), fake_latency_ms=5)))[-1]
    assert isinstance(provider, ScenarioGateway) and provider.source == path and provider.latency_ms == 5


def test_replay_reads_the_recordings_dir():
    stack = layers(build_gateway(Settings(gateway="replay", recordings_dir="some/recordings")))
    assert [type(x) for x in stack] == [CachingGateway, ConcurrencyLimitedGateway, SpendLimitedGateway,
                                        ReplayGateway]
    assert stack[-1]._root == Path("some/recordings")  # type: ignore[attr-defined]


def test_cache_none_drops_only_the_cache_layer():
    stack = layers(build_gateway(Settings(gateway="fake", cache="none")))
    assert [type(x) for x in stack] == [ConcurrencyLimitedGateway, SpendLimitedGateway, ScenarioGateway]


async def test_anthropic_builds_without_a_key_or_valkey_and_fails_only_on_first_call(monkeypatch):
    monkeypatch.delenv("ANTHROPIC_API_KEY", raising=False)
    s = Settings(gateway="anthropic", cache="valkey", valkey_url=UNREACHABLE_VALKEY)
    gw = build_gateway(s)  # lazy: no connection, no key check
    stack = layers(gw)
    assert [type(x) for x in stack] == [CachingGateway, ConcurrencyLimitedGateway, SpendLimitedGateway,
                                        AnthropicGateway]
    cache = stack[0]._cache  # type: ignore[attr-defined]
    limiter = stack[2]._limiter  # type: ignore[attr-defined]
    assert isinstance(cache, ValkeyCache) and isinstance(limiter, ValkeySpendLimiter)
    assert cache._r is limiter._r  # one Valkey client for the cache and the spend counters
    assert limiter.cap_micro_usd == s.daily_spend_cap_micro_usd
    with pytest.raises(SpendLimiterUnavailable):  # the cache miss is tolerated; the cap fails closed
        await gw.complete(intake_req())
    assert stack[2]._live is True  # type: ignore[attr-defined]


def test_only_the_fake_gateway_fails_open_on_a_limiter_outage():
    def spend_layer(gateway):
        return next(x for x in layers(build_gateway(Settings(gateway=gateway, cache="none")))
                    if isinstance(x, SpendLimitedGateway))

    assert spend_layer("fake")._live is False  # type: ignore[attr-defined]
    assert spend_layer("replay")._live is True  # type: ignore[attr-defined]


async def test_the_spend_check_runs_inside_the_concurrency_limit(monkeypatch):
    """Review B #2: 32 queued calls all passed the cap check before the semaphore; now <= 3 are admitted."""
    calls = 0

    class Priced:
        async def complete(self, r):
            nonlocal calls
            calls += 1
            await asyncio.sleep(0.01)
            resp = response_for(r, IntakeResult(kind="invoice", language="en", invoice_count=1, seller_trn="",
                                                buyer_trn="", confidence=0.9))
            return dataclasses.replace(resp, usage=dataclasses.replace(resp.usage, cost_micro_usd=40_000))

    monkeypatch.setattr(factory, "_provider", lambda s: Priced())
    gw = build_gateway(Settings(gateway="fake", cache="none", daily_spend_cap_micro_usd=100_000,
                                max_concurrent_llm_calls=4))
    r = dataclasses.replace(intake_req(), model=SONNET, max_tokens=4000)  # reserves 40,000 micro-USD
    results = await asyncio.gather(*(gw.complete(r) for _ in range(32)), return_exceptions=True)
    admitted = [x for x in results if not isinstance(x, BaseException)]
    assert all(isinstance(x, SpendCapExceeded) for x in results if isinstance(x, BaseException))
    assert len(admitted) <= 3 and calls == len(admitted)
    limiter = layers(gw)[1]._limiter  # type: ignore[attr-defined]
    assert sum(limiter._spent.values()) <= 100_000 + 40_000  # type: ignore[attr-defined]  (no midnight race)


async def test_the_compose_default_serves_and_caches(monkeypatch):
    monkeypatch.delenv("AI_FAKE_SCENARIO", raising=False)
    gw = build_gateway(Settings())
    first = await gw.complete(intake_req())
    second = await gw.complete(intake_req())
    assert isinstance(first.parsed, IntakeResult) and first.usage.llm_calls == 1
    assert second.usage.response_cache_hit and second.usage.llm_calls == 0


def test_fake_responses_never_share_the_live_cache_namespace():
    """A canned response cached under AI_GATEWAY=fake must not be served after switching to anthropic."""
    def cache_prefix(gateway: str) -> str:
        s = Settings(gateway=gateway, cache="valkey", valkey_url=UNREACHABLE_VALKEY)
        return layers(build_gateway(s))[0]._cache._prefix  # type: ignore[attr-defined]

    assert cache_prefix("anthropic") == cache_prefix("replay") == "llmcache:v1:"
    assert cache_prefix("fake") == "llmcache:v1:fake:"


async def test_openai_compat_builds_lazily_live_and_in_its_own_cache_namespace():
    from ai.gateway.openai_compat_gw import OpenAICompatGateway

    s = Settings(gateway="openai_compat", cache="valkey", valkey_url=UNREACHABLE_VALKEY, openai_api_key="k",
                 openai_base_url="https://x.example/v1", openai_model_fast="f", openai_model_smart="m")
    gw = build_gateway(s)
    stack = layers(gw)
    assert [type(x) for x in stack] == [CachingGateway, ConcurrencyLimitedGateway, SpendLimitedGateway,
                                        ResilientGateway, OpenAICompatGateway]
    assert stack[3]._max_attempts == s.openai_max_attempts  # type: ignore[attr-defined]
    assert stack[3]._bucket._interval == 6.0  # type: ignore[attr-defined]  default 10 requests per minute
    assert stack[4]._timeout_s == 240.0  # type: ignore[attr-defined]
    assert stack[2]._live is True  # type: ignore[attr-defined]  fails closed like anthropic
    assert stack[0]._cache._prefix.endswith("openai_compat:")  # type: ignore[attr-defined]
    assert stack[0]._cache._prefix != cache_prefix_of("anthropic")  # type: ignore[attr-defined]
    with pytest.raises(SpendLimiterUnavailable):  # the cap fails closed; no provider call
        await gw.complete(intake_req())


def cache_prefix_of(gateway: str) -> str:
    s = Settings(gateway=gateway, cache="valkey", valkey_url=UNREACHABLE_VALKEY)
    return layers(build_gateway(s))[0]._cache._prefix  # type: ignore[attr-defined]


def test_openai_compat_results_are_not_forced_nor_auto_accepted_by_the_consumer():
    """Live gateways are judged by the verifier alone (documents_consumer.synthetic_results is fake-only):
    openai_compat is a live gateway, so it gets the same verifier gate as anthropic, nothing weaker."""
    from ai.service.documents_consumer import synthetic_results

    assert synthetic_results(Settings(gateway="openai_compat")) is False
    assert synthetic_results(Settings(gateway="anthropic")) is False
    assert synthetic_results(Settings(gateway="fake")) is True
