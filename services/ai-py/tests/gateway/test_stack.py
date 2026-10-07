"""Pricing, response cache, spend cap, concurrency, record/replay (contract section 2)."""

import asyncio
import dataclasses
import json
from datetime import UTC, datetime

import pytest
from fakeredis import FakeAsyncRedis
from pydantic import BaseModel

from ai.gateway.cache import CachingGateway, MemoryCache, ValkeyCache
from ai.gateway.errors import (
    ModelRefusal,
    OutputInvalid,
    RecordingMissing,
    SpendCapExceeded,
    SpendLimiterUnavailable,
    TransientModelError,
)
from ai.gateway.fake import FakeGateway, response_for
from ai.gateway.limits import (
    ConcurrencyLimitedGateway,
    MemorySpendLimiter,
    SpendLimitedGateway,
    ValkeySpendLimiter,
)
from ai.gateway.pricing import cost_micro_usd
from ai.gateway.recorded import RecordingGateway, ReplayGateway
from ai.gateway.types import (
    HAIKU,
    OPUS,
    SONNET,
    DocumentPart,
    Message,
    ModelRequest,
    RequestMeta,
    TextPart,
    Usage,
)


class Out(BaseModel):
    number: str = ""


PDF = b"%PDF-1.7 secret-bytes-never-stored"


def req(firm="f1", cacheable=True, model=SONNET) -> ModelRequest:
    return ModelRequest(model=model, prompt_id="extraction.invoice", prompt_version=1, system="sys",
                        messages=(Message("user", (DocumentPart.of(PDF), TextPart("t"))),), output_model=Out,
                        cacheable=cacheable, meta=RequestMeta(firm, "run", "step"))


def test_pricing_table_and_ceil():
    assert cost_micro_usd(SONNET, input_tokens=1000, output_tokens=200) == 4000
    assert cost_micro_usd(OPUS, input_tokens=1, output_tokens=0) == 4  # ceil(4_000_000 / 1_000_000)
    assert cost_micro_usd(HAIKU, input_tokens=1, output_tokens=0, cache_read_input_tokens=1) == 2  # ceil(1.1)


async def test_cache_hit_is_free_and_skips_the_provider():
    inner = FakeGateway({"extraction.invoice": [Out(number="7")]})
    gw = CachingGateway(inner, MemoryCache())
    first = await gw.complete(req())
    second = await gw.complete(dataclasses.replace(req(), meta=RequestMeta("f1", "run2", "step2")))
    assert len(inner.calls) == 1 and first.usage.llm_calls == 1 and not first.usage.response_cache_hit
    u = second.usage
    assert second.parsed == Out(number="7") and u.response_cache_hit and u.llm_calls == 0
    assert (u.cost_micro_usd, u.input_tokens, u.output_tokens) == (0, 0, 0)
    assert second.cache_key == first.cache_key


async def test_cache_is_per_firm_and_respects_cacheable():
    inner = FakeGateway({"extraction.invoice": [Out(number="7")]})
    gw = CachingGateway(inner, MemoryCache())
    await gw.complete(req())
    await gw.complete(req(firm="f2"))
    await gw.complete(req(cacheable=False))
    await gw.complete(req(cacheable=False))
    assert len(inner.calls) == 4


class BrokenCache:
    async def get(self, key):
        raise ConnectionError("valkey down")

    async def set(self, key, value):
        raise ConnectionError("valkey down")


async def test_cache_outage_and_corrupt_entries_are_misses():
    inner = FakeGateway({"extraction.invoice": [Out(number="7")]})
    assert (await CachingGateway(inner, BrokenCache()).complete(req())).parsed == Out(number="7")
    cache = MemoryCache()
    gw = CachingGateway(inner, cache)
    await gw.complete(req())
    for key in list(cache._data):
        await cache.set(key, b"{not json")
    await gw.complete(req())
    assert len(inner.calls) == 3


async def test_valkey_cache_key_prefix_and_ttl():
    r = FakeAsyncRedis()
    gw = CachingGateway(FakeGateway({"extraction.invoice": [Out(number="7")]}), ValkeyCache(client=r, ttl_s=60))
    resp = await gw.complete(req())
    key = f"llmcache:v1:{resp.cache_key}"
    stored = await r.get(key)
    assert stored is not None and PDF not in stored and 0 < await r.ttl(key) <= 60


async def test_valkey_cache_prefix_namespaces_entries():
    r = FakeAsyncRedis()
    gw = CachingGateway(FakeGateway({"extraction.invoice": [Out(number="7")]}),
                        ValkeyCache(client=r, prefix="llmcache:v1:fake:"))
    resp = await gw.complete(req())
    assert await r.get(f"llmcache:v1:fake:{resp.cache_key}") is not None
    assert await r.get(f"llmcache:v1:{resp.cache_key}") is None


async def test_spend_cap_blocks_before_the_call():
    inner = FakeGateway(lambda r: response_for(r, Out(number="1")))
    limiter = MemorySpendLimiter(cap_micro_usd=100)
    gw = SpendLimitedGateway(inner, limiter, now=lambda: datetime(2026, 9, 29, 23, 0, tzinfo=UTC))
    await limiter.add("f1", "20260929", 100)
    with pytest.raises(SpendCapExceeded):
        await gw.complete(req())
    assert inner.calls == []
    await gw.complete(req(firm="f2"))  # another Firm is unaffected
    assert len(inner.calls) == 1


async def test_spend_is_counted_per_firm_and_day_in_valkey():
    r = FakeAsyncRedis()
    priced = dataclasses.replace  # FakeGateway usage costs 0; give it a price
    inner = FakeGateway(lambda q: priced(response_for(q, Out()), usage=dataclasses.replace(
        response_for(q, Out()).usage, cost_micro_usd=40)))
    limiter = ValkeySpendLimiter(100, client=r)
    gw = SpendLimitedGateway(inner, limiter, now=lambda: datetime(2026, 9, 29, tzinfo=UTC))
    for _ in range(3):
        await gw.complete(req())
    assert int(await r.get("llmspend:f1:20260929")) == 120 and await r.ttl("llmspend:f1:20260929") > 0
    with pytest.raises(SpendCapExceeded):
        await gw.complete(req())


class BrokenLimiter:
    cap_micro_usd = 10

    async def spent(self, firm_id, day):
        raise ConnectionError("down")

    async def reserve(self, firm_id, day, micro_usd):
        raise ConnectionError("down")

    async def add(self, firm_id, day, micro_usd):
        raise ConnectionError("down")


async def test_limiter_outage_is_transient():
    with pytest.raises(TransientModelError):
        await SpendLimitedGateway(FakeGateway({}), BrokenLimiter()).complete(req())


async def test_concurrency_limit():
    active = peak = 0

    class Slow:
        async def complete(self, r):
            nonlocal active, peak
            active += 1
            peak = max(peak, active)
            await asyncio.sleep(0.01)
            active -= 1
            return response_for(r, Out())

    gw = ConcurrencyLimitedGateway(Slow(), 2)
    await asyncio.gather(*(gw.complete(req()) for _ in range(6)))
    assert peak == 2


async def test_record_then_replay(tmp_path):
    rec = RecordingGateway(FakeGateway({"extraction.invoice": [Out(number="9")]}), tmp_path)
    live = await rec.complete(req())
    files = list((tmp_path / "extraction.invoice").glob("*.json"))
    assert [f.stem for f in files] == [live.cache_key]
    raw = files[0].read_bytes()
    assert PDF not in raw and b"secret-bytes" not in raw
    assert json.loads(raw)["usage"]["input_tokens"] == 1000
    replayed = await ReplayGateway(tmp_path).complete(req())
    assert replayed.parsed == Out(number="9") and replayed.usage.cost_micro_usd == 4000
    with pytest.raises(RecordingMissing):
        await ReplayGateway(tmp_path).complete(req(firm="other"))


def _billed(exc: Exception, cost: int) -> Exception:
    usage = Usage(model=SONNET, prompt_id="extraction.invoice", prompt_version=1, input_tokens=10,
                  output_tokens=5, cost_micro_usd=cost, llm_calls=1)
    exc.usage = usage  # type: ignore[attr-defined]
    return exc


async def test_a_billed_failure_counts_toward_the_cap_and_is_reraised():
    """Concern P2 (Task 6): refusals, max_tokens stops and schema failures are billed; the cap counts them."""
    day = "20260929"
    inner = FakeGateway({"extraction.invoice": [_billed(ModelRefusal("cyber"), 60)]})
    limiter = MemorySpendLimiter(cap_micro_usd=100)
    gw = SpendLimitedGateway(inner, limiter, now=lambda: datetime(2026, 9, 29, 12, 0, tzinfo=UTC))
    for _ in range(2):
        with pytest.raises(ModelRefusal):
            await gw.complete(req())
    assert await limiter.spent("f1", day) == 120
    with pytest.raises(SpendCapExceeded):
        await gw.complete(req())
    assert len(inner.calls) == 2


async def test_an_unbilled_failure_adds_nothing():
    limiter = MemorySpendLimiter(cap_micro_usd=100)
    gw = SpendLimitedGateway(FakeGateway({"extraction.invoice": [TransientModelError("529")]}), limiter,
                             now=lambda: datetime(2026, 9, 29, tzinfo=UTC))
    with pytest.raises(TransientModelError):
        await gw.complete(req())
    assert await limiter.spent("f1", "20260929") == 0


DAY = "20260929"


def at_noon() -> datetime:
    return datetime(2026, 9, 29, 12, 0, tzinfo=UTC)


class Priced:
    """A provider whose every call costs `cost` and takes a moment, so concurrent callers overlap."""

    def __init__(self, cost: int, on_call=None) -> None:
        self.cost = cost
        self.calls = 0
        self._on_call = on_call

    async def complete(self, r):
        self.calls += 1
        if self._on_call is not None:
            await self._on_call()
        await asyncio.sleep(0.01)
        resp = response_for(r, Out())
        return dataclasses.replace(resp, usage=dataclasses.replace(resp.usage, cost_micro_usd=self.cost))


def priced_req(firm="f1") -> ModelRequest:
    """Sonnet output is 10 micro-USD per token: max_tokens 4000 reserves 40,000 micro-USD."""
    return dataclasses.replace(req(firm), max_tokens=4000)


async def burst(gws, n: int = 32) -> int:
    """n concurrent calls spread over `gws` (replicas); returns how many were admitted."""
    results = await asyncio.gather(*(gws[i % len(gws)].complete(priced_req()) for i in range(n)),
                                   return_exceptions=True)
    unexpected = [r for r in results if isinstance(r, BaseException) and not isinstance(r, SpendCapExceeded)]
    assert unexpected == []
    return sum(1 for r in results if not isinstance(r, BaseException))


async def test_concurrent_calls_cannot_overshoot_the_cap_in_memory():
    """Review B #2: cap 100k, 32 concurrent 40k calls -> all 32 were admitted (6.4M spent)."""
    inner, limiter = Priced(40_000), MemorySpendLimiter(100_000)
    admitted = await burst([SpendLimitedGateway(inner, limiter, now=at_noon)])
    assert admitted <= 3 and inner.calls == admitted
    assert await limiter.spent("f1", DAY) <= 100_000 + 40_000


async def test_concurrent_replicas_cannot_overshoot_the_cap_in_valkey():
    r = FakeAsyncRedis()  # one Valkey shared by two replicas
    inners = [Priced(40_000), Priced(40_000)]
    gws = [SpendLimitedGateway(i, ValkeySpendLimiter(100_000, client=r), now=at_noon) for i in inners]
    admitted = await burst(gws)
    assert admitted <= 3 and sum(i.calls for i in inners) == admitted
    assert int(await r.get("llmspend:f1:20260929")) <= 100_000 + 40_000


async def test_the_reservation_is_held_during_the_call_and_settled_to_the_actual_cost():
    limiter = MemorySpendLimiter(100_000)
    seen: list[int] = []

    async def peek():
        seen.append(await limiter.spent("f1", DAY))

    await SpendLimitedGateway(Priced(1_234, on_call=peek), limiter, now=at_noon).complete(priced_req())
    assert seen == [40_000]  # max_tokens x output price, reserved before the provider is called
    assert await limiter.spent("f1", DAY) == 1_234


def _hangs(started: asyncio.Event):
    class Hangs:
        async def complete(self, r):
            started.set()
            await asyncio.sleep(3600)

    return Hangs()


async def _cancel_mid_call(gw: SpendLimitedGateway) -> None:
    started = asyncio.Event()
    gw._inner = _hangs(started)  # type: ignore[attr-defined]
    task = asyncio.create_task(gw.complete(priced_req()))
    await started.wait()
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task


async def test_a_cancelled_live_call_is_charged_its_reservation():
    """A node timeout or run cancel mid-call: the provider may have billed it, so the upper bound stays."""
    limiter = MemorySpendLimiter(100_000)
    await _cancel_mid_call(SpendLimitedGateway(FakeGateway({}), limiter, now=at_noon))
    assert await limiter.spent("f1", DAY) == 40_000


async def test_a_cancelled_fake_call_releases_its_reservation():
    limiter = MemorySpendLimiter(100_000)
    await _cancel_mid_call(SpendLimitedGateway(FakeGateway({}), limiter, now=at_noon, live=False))
    assert await limiter.spent("f1", DAY) == 0


async def test_a_limiter_outage_fails_closed_for_live_and_open_for_the_fake():
    inner = Priced(0)
    with pytest.raises(SpendLimiterUnavailable) as caught:
        await SpendLimitedGateway(inner, BrokenLimiter()).complete(req())
    assert isinstance(caught.value, TransientModelError) and inner.calls == 0
    assert "spend limiter unavailable" in str(caught.value)
    resp = await SpendLimitedGateway(inner, BrokenLimiter(), live=False).complete(req())
    assert resp.parsed == Out() and inner.calls == 1


async def test_a_limiter_outage_while_charging_a_failure_keeps_the_original_error():
    class AddFails(MemorySpendLimiter):
        async def add(self, firm_id, day, micro_usd):
            raise ConnectionError("down")

    gw = SpendLimitedGateway(FakeGateway({"extraction.invoice": [_billed(OutputInvalid("schema"), 30)]}),
                             AddFails(100))
    with pytest.raises(OutputInvalid):
        await gw.complete(req())
