"""Per-Firm daily spend cap and the process-wide concurrency limit (contract section 2)."""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any, Protocol

from ai.gateway.errors import SpendCapExceeded, TransientModelError
from ai.gateway.types import ModelGateway, ModelRequest, ModelResponse

log = logging.getLogger(__name__)


def utc_day(now: datetime) -> str:
    return now.astimezone(UTC).strftime("%Y%m%d")


class SpendLimiter(Protocol):
    cap_micro_usd: int

    async def spent(self, firm_id: str, day: str) -> int: ...
    async def add(self, firm_id: str, day: str, micro_usd: int) -> int: ...


class MemorySpendLimiter:
    """Process-local counters: tests, and the dedicated `--max-cost-usd` limiter of `evals live`."""

    def __init__(self, cap_micro_usd: int) -> None:
        self.cap_micro_usd = cap_micro_usd
        self._spent: dict[tuple[str, str], int] = {}

    async def spent(self, firm_id: str, day: str) -> int:
        return self._spent.get((firm_id, day), 0)

    async def add(self, firm_id: str, day: str, micro_usd: int) -> int:
        total = self._spent.get((firm_id, day), 0) + micro_usd
        self._spent[(firm_id, day)] = total
        return total


class ValkeySpendLimiter:
    """Shared counters: key llmspend:<firm_id>:<YYYYMMDD UTC> holds micro-USD (INCRBY), expiring after 3 days."""

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

    async def add(self, firm_id: str, day: str, micro_usd: int) -> int:
        k = self.key(firm_id, day)
        async with self._r.pipeline(transaction=True) as p:
            p.incrby(k, micro_usd)
            p.expire(k, self._ttl)
            total, _ = await p.execute()
        return int(total)


class SpendLimitedGateway:
    """Refuses a call when the Firm's counter for today is at or over the cap; adds the cost afterwards.

    A limiter outage raises TransientModelError (the node retries, then the message is nak'ed): the cap
    fails closed but recovers by itself.
    """

    def __init__(self, inner: ModelGateway, limiter: SpendLimiter, *,
                 now: Callable[[], datetime] = lambda: datetime.now(UTC)) -> None:
        self._inner = inner
        self._limiter = limiter
        self._now = now

    async def complete(self, req: ModelRequest) -> ModelResponse:
        firm = req.meta.firm_id if req.meta else ""
        day = utc_day(self._now())
        try:
            spent = await self._limiter.spent(firm, day)
        except Exception as exc:  # noqa: BLE001
            raise TransientModelError(f"spend limiter unavailable: {type(exc).__name__}") from None
        if spent >= self._limiter.cap_micro_usd:
            raise SpendCapExceeded(f"daily LLM spend cap reached for firm {firm}")
        resp = await self._inner.complete(req)
        if resp.usage.cost_micro_usd > 0:
            try:
                await self._limiter.add(firm, day, resp.usage.cost_micro_usd)
            except Exception:  # the call already happened; never lose its result
                log.warning("spend limiter add failed firm_id=%s", firm, exc_info=True)
        return resp


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
