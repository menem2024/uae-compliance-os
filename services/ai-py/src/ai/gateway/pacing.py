"""Client-side pacing and bounded retries for rate-limited providers (free tiers).

`ResilientGateway` wraps one provider adapter (today only openai_compat; the Anthropic path is untouched):

- a token bucket spaces calls to at most `AI_OPENAI_MAX_RPM` per minute, so calls wait instead of failing with
  429. Every attempt, retries included, takes a token (each one counts against the provider's quota);
- transient failures (429 per-minute, 5xx overload, connection and read timeouts) are retried here with a
  jittered exponential backoff, or the provider's own Retry-After when it is longer, bounded by
  `max_retry_wait_s`: the node-level RetryPolicy caps its waits at 20 s, far below a per-minute quota window;
- `QuotaExhausted` (a daily quota) is never retried and is remembered per model tier, so later calls fail at
  once, with no request, until the provider's reset hint has passed.

Everything time-related (clock, sleep, jitter) is injectable for tests. No request content is touched here.
"""

from __future__ import annotations

import asyncio
import random
import time
from collections.abc import Awaitable, Callable

from ai.gateway.errors import QuotaExhausted, TransientModelError
from ai.gateway.types import ModelGateway, ModelRequest, ModelResponse

type Sleep = Callable[[float], Awaitable[None]]


class TokenBucket:
    """Evenly spaced admissions: `rpm` per minute with room for `burst` back-to-back calls after an idle spell.
    `rpm <= 0` disables pacing. Waiters queue in arrival order."""

    def __init__(self, rpm: float, *, burst: int = 1, clock: Callable[[], float] = time.monotonic,
                 sleep: Sleep = asyncio.sleep) -> None:
        self._interval = 60.0 / rpm if rpm > 0 else 0.0
        self._burst = float(max(1, burst))
        self._clock, self._sleep = clock, sleep
        self._tokens = self._burst
        self._stamp = clock()
        self._lock = asyncio.Lock()

    async def acquire(self) -> None:
        if not self._interval:
            return
        async with self._lock:
            while True:
                now = self._clock()
                self._tokens = min(self._burst, self._tokens + (now - self._stamp) / self._interval)
                self._stamp = now
                if self._tokens >= 1.0:
                    self._tokens -= 1.0
                    return
                await self._sleep((1.0 - self._tokens) * self._interval)


class ResilientGateway:
    def __init__(self, inner: ModelGateway, *, bucket: TokenBucket | None = None, max_attempts: int = 5,
                 base_delay_s: float = 2.0, max_delay_s: float = 30.0, max_retry_wait_s: float = 120.0,
                 jitter: float = 0.25, clock: Callable[[], float] = time.monotonic, sleep: Sleep = asyncio.sleep,
                 rng: random.Random | None = None) -> None:
        if max_attempts < 1:
            raise ValueError("max_attempts must be >= 1")
        self._inner = inner
        self._bucket = bucket
        self._max_attempts = max_attempts
        self._base, self._max_delay, self._max_wait, self._jitter = base_delay_s, max_delay_s, max_retry_wait_s, jitter
        self._clock, self._sleep = clock, sleep
        self._rng = rng or random.Random()
        self._blocked: dict[str, tuple[float, str]] = {}  # model tier -> (until, message)

    async def complete(self, req: ModelRequest) -> ModelResponse:
        for attempt in range(1, self._max_attempts + 1):
            self._fail_fast_if_blocked(req)
            if self._bucket is not None:
                await self._bucket.acquire()
            try:
                return await self._inner.complete(req)
            except QuotaExhausted as exc:
                self._remember(req, exc)
                raise
            except TransientModelError as exc:
                wait = self._wait_s(attempt, exc)
                if attempt >= self._max_attempts or wait is None:
                    raise
                await self._sleep(wait)
        raise AssertionError("unreachable")  # pragma: no cover

    def _wait_s(self, attempt: int, exc: TransientModelError) -> float | None:
        """Seconds to wait before the next attempt; None when the provider asks for longer than we may wait."""
        hint = exc.retry_after_s
        if hint is not None:
            if hint > self._max_wait:
                return None
            return min(self._max_wait, max(0.0, hint) * (1 + self._rng.uniform(0, self._jitter)))
        base = min(self._max_delay, self._base * (2 ** (attempt - 1)))
        return max(0.0, base * (1 + self._rng.uniform(-self._jitter, self._jitter)))

    def _remember(self, req: ModelRequest, exc: QuotaExhausted) -> None:
        if exc.retry_after_s is not None and exc.retry_after_s > 0:
            self._blocked[req.model] = (self._clock() + exc.retry_after_s, str(exc))

    def _fail_fast_if_blocked(self, req: ModelRequest) -> None:
        entry = self._blocked.get(req.model)
        if entry is None:
            return
        until, message = entry
        remaining = until - self._clock()
        if remaining <= 0:
            del self._blocked[req.model]
            return
        raise QuotaExhausted(message, retry_after_s=remaining)
