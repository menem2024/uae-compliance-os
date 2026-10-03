"""build_gateway: Caching -> SpendLimited -> ConcurrencyLimited -> provider, chosen by AI_GATEWAY."""

from pathlib import Path

import pytest

from ai.agents.intake import agent as intake_agent
from ai.agents.intake.schema import IntakeResult
from ai.gateway.anthropic_gw import AnthropicGateway
from ai.gateway.cache import CachingGateway, MemoryCache, ValkeyCache
from ai.gateway.errors import TransientModelError
from ai.gateway.factory import build_gateway
from ai.gateway.fake import DEFAULT_SCENARIO, ScenarioGateway
from ai.gateway.limits import (
    ConcurrencyLimitedGateway,
    MemorySpendLimiter,
    SpendLimitedGateway,
    ValkeySpendLimiter,
)
from ai.gateway.recorded import ReplayGateway
from ai.gateway.types import HAIKU, Message, ModelRequest, RequestMeta, TextPart
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
    assert [type(x) for x in stack] == [CachingGateway, SpendLimitedGateway, ConcurrencyLimitedGateway,
                                        ScenarioGateway]
    caching, spend, conc, provider = stack
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
    assert [type(x) for x in stack] == [CachingGateway, SpendLimitedGateway, ConcurrencyLimitedGateway,
                                        ReplayGateway]
    assert stack[-1]._root == Path("some/recordings")  # type: ignore[attr-defined]


def test_cache_none_drops_only_the_cache_layer():
    stack = layers(build_gateway(Settings(gateway="fake", cache="none")))
    assert [type(x) for x in stack] == [SpendLimitedGateway, ConcurrencyLimitedGateway, ScenarioGateway]


async def test_anthropic_builds_without_a_key_or_valkey_and_fails_only_on_first_call(monkeypatch):
    monkeypatch.delenv("ANTHROPIC_API_KEY", raising=False)
    s = Settings(gateway="anthropic", cache="valkey", valkey_url=UNREACHABLE_VALKEY)
    gw = build_gateway(s)  # lazy: no connection, no key check
    stack = layers(gw)
    assert [type(x) for x in stack] == [CachingGateway, SpendLimitedGateway, ConcurrencyLimitedGateway,
                                        AnthropicGateway]
    cache = stack[0]._cache  # type: ignore[attr-defined]
    limiter = stack[1]._limiter  # type: ignore[attr-defined]
    assert isinstance(cache, ValkeyCache) and isinstance(limiter, ValkeySpendLimiter)
    assert cache._r is limiter._r  # one Valkey client for the cache and the spend counters
    assert limiter.cap_micro_usd == s.daily_spend_cap_micro_usd
    with pytest.raises(TransientModelError):  # the cache miss is tolerated; the cap fails closed
        await gw.complete(intake_req())


async def test_the_compose_default_serves_and_caches(monkeypatch):
    monkeypatch.delenv("AI_FAKE_SCENARIO", raising=False)
    gw = build_gateway(Settings())
    first = await gw.complete(intake_req())
    second = await gw.complete(intake_req())
    assert isinstance(first.parsed, IntakeResult) and first.usage.llm_calls == 1
    assert second.usage.response_cache_hit and second.usage.llm_calls == 0
