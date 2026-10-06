import asyncio
import random

import pytest

from ai.gateway.errors import OutputInvalid, PermanentModelError, QuotaExhausted, TransientModelError
from ai.gateway.pacing import ResilientGateway, TokenBucket
from ai.gateway.types import HAIKU, SONNET, Message, ModelRequest, ModelResponse, TextPart, Usage


class Clock:
    def __init__(self) -> None:
        self.now = 1000.0
        self.sleeps: list[float] = []

    def __call__(self) -> float:
        return self.now

    async def sleep(self, s: float) -> None:
        self.sleeps.append(s)
        self.now += s


def request(model=SONNET) -> ModelRequest:
    return ModelRequest(model=model, prompt_id="p", prompt_version=1, system="s",
                        messages=(Message("user", (TextPart("hi"),)),))


def response() -> ModelResponse:
    return ModelResponse(model="m", text="ok", parsed=None, tool_calls=(), stop_reason="end_turn",
                         usage=Usage(model=SONNET, prompt_id="p", prompt_version=1), latency_ms=1,
                         cache_key="k", provider_request_id="r")


class Inner:
    def __init__(self, *outcomes) -> None:
        self.outcomes = list(outcomes)
        self.calls = 0

    async def complete(self, req):
        self.calls += 1
        out = self.outcomes.pop(0) if self.outcomes else response()
        if isinstance(out, Exception):
            raise out
        return out


def gateway(inner, clock, *, rpm=0, **kw) -> ResilientGateway:
    bucket = TokenBucket(rpm, clock=clock, sleep=clock.sleep) if rpm else None
    return ResilientGateway(inner, bucket=bucket, clock=clock, sleep=clock.sleep, rng=random.Random(1), **kw)


async def test_bucket_spaces_calls_evenly():
    clock = Clock()
    bucket = TokenBucket(10, clock=clock, sleep=clock.sleep)
    for _ in range(4):
        await bucket.acquire()
    assert clock.sleeps == pytest.approx([6.0, 6.0, 6.0])  # 10 per minute, first call is immediate


async def test_bucket_refills_while_idle_and_disabled_bucket_never_waits():
    clock = Clock()
    bucket = TokenBucket(10, clock=clock, sleep=clock.sleep)
    await bucket.acquire()
    clock.now += 60
    await bucket.acquire()
    assert clock.sleeps == []
    off = TokenBucket(0, clock=clock, sleep=clock.sleep)
    for _ in range(50):
        await off.acquire()
    assert clock.sleeps == []


async def test_concurrent_callers_are_queued_by_the_bucket():
    clock = Clock()
    bucket = TokenBucket(10, clock=clock, sleep=clock.sleep)
    await asyncio.gather(*(bucket.acquire() for _ in range(3)))
    assert clock.now - 1000.0 == pytest.approx(12.0)


async def test_503_is_retried_with_jittered_exponential_backoff():
    clock = Clock()
    inner = Inner(TransientModelError("HTTP 503 UNAVAILABLE"), TransientModelError("HTTP 503 UNAVAILABLE"))
    out = await gateway(inner, clock).complete(request())
    assert out.text == "ok" and inner.calls == 3
    first, second = clock.sleeps
    assert 1.5 <= first <= 2.5 and 3.0 <= second <= 5.0  # 2 s then 4 s, +/-25 %
    assert first != 2.0 or second != 4.0  # jittered


async def test_retry_after_longer_than_the_node_policy_cap_is_honoured():
    clock = Clock()
    inner = Inner(TransientModelError("HTTP 429", retry_after_s=45.0))
    await gateway(inner, clock).complete(request())
    assert 45.0 <= clock.sleeps[0] <= 45.0 * 1.25 + 0.001


async def test_retry_after_beyond_the_bound_is_not_waited_for():
    clock = Clock()
    inner = Inner(TransientModelError("HTTP 429", retry_after_s=500.0))
    with pytest.raises(TransientModelError):
        await gateway(inner, clock, max_retry_wait_s=120.0).complete(request())
    assert inner.calls == 1 and clock.sleeps == []


async def test_attempts_are_bounded_and_the_last_error_surfaces():
    clock = Clock()
    inner = Inner(*(TransientModelError(f"HTTP 503 #{i}") for i in range(10)))
    with pytest.raises(TransientModelError, match="#2"):
        await gateway(inner, clock, max_attempts=3).complete(request())
    assert inner.calls == 3 and len(clock.sleeps) == 2


@pytest.mark.parametrize("exc", [PermanentModelError("HTTP 400"), OutputInvalid("bad json")])
async def test_non_transient_errors_are_not_retried(exc):
    clock = Clock()
    inner = Inner(exc)
    with pytest.raises(type(exc)):
        await gateway(inner, clock).complete(request())
    assert inner.calls == 1 and clock.sleeps == []


async def test_every_attempt_takes_a_token():
    clock = Clock()
    inner = Inner(TransientModelError("HTTP 503"), TransientModelError("HTTP 503"))
    await gateway(inner, clock, rpm=10).complete(request())
    # two backoffs (~2 s and ~4 s) are shorter than the 6 s token interval: the bucket tops each wait up
    assert clock.now - 1000.0 >= 12.0 and inner.calls == 3


async def test_daily_quota_is_not_retried_and_later_calls_fail_without_a_request():
    clock = Clock()
    inner = Inner(QuotaExhausted("HTTP 429 daily", retry_after_s=3600.0))
    gw = gateway(inner, clock)
    with pytest.raises(QuotaExhausted):
        await gw.complete(request())
    assert inner.calls == 1 and clock.sleeps == []
    with pytest.raises(QuotaExhausted, match="daily") as ei:
        await gw.complete(request())
    assert inner.calls == 1 and 3500 < ei.value.retry_after_s <= 3600
    assert (await gw.complete(request(HAIKU))).text == "ok"  # another tier maps to another model's quota
    clock.now += 3601
    assert (await gw.complete(request())).text == "ok" and inner.calls == 3


def test_attempts_must_be_positive():
    with pytest.raises(ValueError, match="max_attempts"):
        ResilientGateway(Inner(), max_attempts=0)
