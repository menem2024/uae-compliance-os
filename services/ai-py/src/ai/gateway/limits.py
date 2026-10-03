"""Per-Firm daily spend cap and the process-wide concurrency limit (contract section 2)."""

from __future__ import annotations

import asyncio
import logging
import threading
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any, Protocol

from ai.gateway.errors import GatewayError, SpendCapExceeded, SpendLimiterUnavailable
from ai.gateway.pricing import reservation_micro_usd
from ai.gateway.types import ModelGateway, ModelRequest, ModelResponse

log = logging.getLogger(__name__)


def utc_day(now: datetime) -> str:
    return now.astimezone(UTC).strftime("%Y%m%d")


class SpendLimiter(Protocol):
    cap_micro_usd: int

    async def spent(self, firm_id: str, day: str) -> int: ...

    async def reserve(self, firm_id: str, day: str, micro_usd: int) -> bool:
        """Atomically adds `micro_usd` unless the counter (settled spend plus open reservations) is already at
        or over the cap; True when it was added (the call is admitted)."""
        ...

    async def add(self, firm_id: str, day: str, micro_usd: int) -> int:
        """Adds a signed amount (a settlement: actual cost minus the reservation); returns the new total."""
        ...


class MemorySpendLimiter:
    """Process-local counters: tests, and the dedicated `--max-cost-usd` limiter of `evals live`.

    Each operation is one critical section under a lock, with no await inside it, so a check and its
    increment can never interleave with another caller's (another task or another thread).
    """

    def __init__(self, cap_micro_usd: int) -> None:
        self.cap_micro_usd = cap_micro_usd
        self._spent: dict[tuple[str, str], int] = {}
        self._lock = threading.Lock()

    async def spent(self, firm_id: str, day: str) -> int:
        with self._lock:
            return self._spent.get((firm_id, day), 0)

    async def reserve(self, firm_id: str, day: str, micro_usd: int) -> bool:
        with self._lock:
            current = self._spent.get((firm_id, day), 0)
            if current >= self.cap_micro_usd:
                return False
            self._spent[(firm_id, day)] = current + micro_usd
            return True

    async def add(self, firm_id: str, day: str, micro_usd: int) -> int:
        with self._lock:
            total = self._spent.get((firm_id, day), 0) + micro_usd
            self._spent[(firm_id, day)] = total
            return total


class ValkeySpendLimiter:
    """Shared counters: key llmspend:<firm_id>:<YYYYMMDD UTC> holds micro-USD (INCRBY), expiring after 3 days.

    `reserve` is INCRBY-then-check-then-rollback: INCRBY is atomic across every replica, so each caller sees
    a distinct running total and only one whose total *before* its own increment was under the cap is
    admitted; the others DECRBY their increment back. A concurrent caller may see a refused increment for an
    instant and be refused too: the race only ever errs towards refusing.
    """

    def __init__(self, cap_micro_usd: int, *, url: str = "", client: Any = None, ttl_s: int = 3 * 86400) -> None:
        if client is None:
            from redis.asyncio import Redis

            client = Redis.from_url(url)
        self.cap_micro_usd = cap_micro_usd
        self._r = client
        self._ttl = ttl_s

    @staticmethod
    def key(firm_id: str, day: str) -> str:
        return f"llmspend:{firm_id}:{day}"

    async def spent(self, firm_id: str, day: str) -> int:
        raw = await self._r.get(self.key(firm_id, day))
        return int(raw) if raw is not None else 0

    async def reserve(self, firm_id: str, day: str, micro_usd: int) -> bool:
        total = await self.add(firm_id, day, micro_usd)
        if total - micro_usd < self.cap_micro_usd:
            return True
        try:
            await self._r.decrby(self.key(firm_id, day), micro_usd)
        except Exception:  # the counter over-counts by micro_usd: errs towards refusing, never towards spending
            log.warning("spend limiter rollback failed firm_id=%s", firm_id, exc_info=True)
        return False

    async def add(self, firm_id: str, day: str, micro_usd: int) -> int:
        k = self.key(firm_id, day)
        async with self._r.pipeline(transaction=True) as p:
            p.incrby(k, micro_usd)
            p.expire(k, self._ttl)
            total, _ = await p.execute()
        return int(total)


class SpendLimitedGateway:
    """Per-Firm daily cap: reserve before the call, settle after it.

    Admission reserves `reservation_micro_usd(req)` (max_tokens at the model's output price) atomically, and
    refuses with SpendCapExceeded when the Firm's counter, open reservations included, is already at or over
    the cap. Concurrent callers therefore see each other's reservations: the cap can be passed by at most one
    call's reservation plus the input cost of the calls in flight, never by every queued call. The factory
    puts this layer inside the concurrency limit, so at most AI_MAX_CONCURRENT_LLM_CALLS calls per replica
    hold a reservation; queued calls hold none.

    Settlement replaces the reservation with the actual cost: the response's usage, or the usage a billed
    failure carries (refusal, max_tokens, context overflow, schema mismatch); a GatewayError without usage
    (429, 5xx, connection) was not billed and releases it. Anything else that interrupts the call (a node
    timeout or run cancellation arriving as CancelledError, a bug after the provider answered) leaves the
    reservation in place as the charge, because the provider may have billed the request and its real usage
    is lost: over-counting by at most one reservation is preferred to a call the cap never sees. A failed
    settlement is logged and leaves the reservation as the charge too.

    `live=False` is only for the fake gateway, whose calls cost nothing: a limiter outage then admits the
    call (local stacks keep working without Valkey) and an interrupted call releases its reservation. A live
    gateway (anthropic, replay) fails closed on a limiter outage with SpendLimiterUnavailable, a
    TransientModelError: no provider call is made, the node retries, the message is nak'ed and redelivered,
    and ingestion resumes by itself when the store is back.
    """

    def __init__(self, inner: ModelGateway, limiter: SpendLimiter, *,
                 now: Callable[[], datetime] = lambda: datetime.now(UTC), live: bool = True) -> None:
        self._inner = inner
        self._limiter = limiter
        self._now = now
        self._live = live

    async def complete(self, req: ModelRequest) -> ModelResponse:
        firm = req.meta.firm_id if req.meta else ""
        day = utc_day(self._now())
        reserved = reservation_micro_usd(req.model, req.max_tokens)
        try:
            admitted = await self._limiter.reserve(firm, day, reserved)
        except Exception as exc:  # noqa: BLE001
            if self._live:
                raise SpendLimiterUnavailable(
                    f"spend limiter unavailable ({type(exc).__name__}): live model calls are refused until "
                    "it recovers") from None
            log.warning("spend limiter unavailable, fake gateway call admitted firm_id=%s", firm)
            return await self._inner.complete(req)
        if not admitted:
            raise SpendCapExceeded(f"daily LLM spend cap reached for firm {firm}")
        try:
            resp = await self._inner.complete(req)
        except GatewayError as exc:
            billed = exc.usage.cost_micro_usd if exc.usage is not None else 0
            await self._settle(firm, day, billed - reserved)
            raise
        except BaseException:
            if not self._live:  # nothing was billed: give the reservation back, even while being cancelled
                await asyncio.shield(self._settle(firm, day, -reserved))
            raise
        await self._settle(firm, day, resp.usage.cost_micro_usd - reserved)
        return resp

    async def _settle(self, firm: str, day: str, delta_micro_usd: int) -> None:
        if delta_micro_usd == 0:
            return
        try:
            await self._limiter.add(firm, day, delta_micro_usd)
        except Exception:  # the call already happened; never lose its result or its error
            log.warning("spend limiter settlement failed firm_id=%s; the reservation stays charged", firm,
                        exc_info=True)


class ConcurrencyLimitedGateway:
    """Process-wide semaphore around provider calls (AI_MAX_CONCURRENT_LLM_CALLS)."""

    def __init__(self, inner: ModelGateway, max_concurrent: int) -> None:
        if max_concurrent < 1:
            raise ValueError("max_concurrent must be >= 1")
        self._inner = inner
        self._sem = asyncio.Semaphore(max_concurrent)

    async def complete(self, req: ModelRequest) -> ModelResponse:
        async with self._sem:
            return await self._inner.complete(req)
