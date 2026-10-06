"""validator-rs over gRPC (spec 5.7.4). The Fix agent re-validates every candidate through this client: only
validator-rs decides whether an invoice is valid (ADR 006).

`VALIDATOR_ADDR` is read lazily, at the first call, never at import or in `register()` (finding F12).
"""

from __future__ import annotations

import os
from collections.abc import Callable, Mapping, Sequence
from typing import Protocol

import grpc

from ai.gen.compliance.v1 import invoice_pb2, validator_pb2, validator_pb2_grpc
from ai.runtime.errors import ToolTransientError

ADDR_ENV = "VALIDATOR_ADDR"
DEFAULT_ADDR = "validator-rs:50051"  # the compose service name
_TRANSIENT = frozenset({grpc.StatusCode.UNAVAILABLE, grpc.StatusCode.DEADLINE_EXCEEDED,
                        grpc.StatusCode.RESOURCE_EXHAUSTED})


class ValidatorLike(Protocol):
    async def validate(self, invoice: invoice_pb2.Invoice, ruleset_version: str) -> validator_pb2.ValidationRun: ...


class ValidatorFailed(RuntimeError):
    """validator-rs answered with a status that is neither transient nor a bad request."""


class ValidatorClient:
    """`grpc.aio` client of `compliance.v1.ValidatorService/Validate`.

    UNAVAILABLE, DEADLINE_EXCEEDED and RESOURCE_EXHAUSTED raise ToolTransientError (the node's RetryPolicy
    retries); INVALID_ARGUMENT raises ValueError (the task fails as a bad request).
    """

    def __init__(self, addr: str, timeout_s: float = 5.0) -> None:
        self._addr = addr
        self._timeout_s = timeout_s
        self._channel: grpc.aio.Channel | None = None
        self._stub: validator_pb2_grpc.ValidatorServiceStub | None = None

    def _client(self) -> validator_pb2_grpc.ValidatorServiceStub:
        if self._stub is None:  # created on first use, inside the running event loop
            self._channel = grpc.aio.insecure_channel(self._addr)
            self._stub = validator_pb2_grpc.ValidatorServiceStub(self._channel)
        return self._stub

    async def validate(self, invoice: invoice_pb2.Invoice, ruleset_version: str) -> validator_pb2.ValidationRun:
        req = validator_pb2.ValidateRequest(invoice=invoice, ruleset_version=ruleset_version)
        try:
            resp = await self._client().Validate(req, timeout=self._timeout_s)
        except grpc.aio.AioRpcError as exc:
            code = exc.code()
            if code in _TRANSIENT:
                raise ToolTransientError(f"validator-rs {code.name}") from exc
            if code == grpc.StatusCode.INVALID_ARGUMENT:
                raise ValueError(f"validator-rs rejected the request: {exc.details()}") from exc
            raise ValidatorFailed(f"validator-rs {code.name}") from exc
        return resp.run

    async def close(self) -> None:
        if self._channel is not None:
            await self._channel.close()
            self._channel = self._stub = None


class LazyValidator:
    """The production `ValidatorLike`: builds a ValidatorClient from `VALIDATOR_ADDR` on first use."""

    def __init__(self, env: Callable[[], Mapping[str, str]] = lambda: os.environ,
                 timeout_s: float = 5.0) -> None:
        self._env = env
        self._timeout_s = timeout_s
        self._client: ValidatorClient | None = None

    async def validate(self, invoice: invoice_pb2.Invoice, ruleset_version: str) -> validator_pb2.ValidationRun:
        if self._client is None:
            self._client = ValidatorClient(self._env().get(ADDR_ENV) or DEFAULT_ADDR, self._timeout_s)
        return await self._client.validate(invoice, ruleset_version)


type FakeResult = validator_pb2.ValidationRun | Exception


class FakeValidatorClient:
    """Scripted in-memory stand-in for tests and evals: no Python test talks to a real validator.

    `script` is a callable `(invoice, ruleset_version) -> ValidationRun | Exception` or a sequence of results
    used in order (the last one repeats). Every call is recorded in `.calls` as (invoice copy, version).
    """

    def __init__(self, script: Callable[[invoice_pb2.Invoice, str], FakeResult] | Sequence[FakeResult]) -> None:
        self._script = script
        self._n = 0
        self.calls: list[tuple[invoice_pb2.Invoice, str]] = []

    async def validate(self, invoice: invoice_pb2.Invoice, ruleset_version: str) -> validator_pb2.ValidationRun:
        snapshot = invoice_pb2.Invoice()
        snapshot.CopyFrom(invoice)
        self.calls.append((snapshot, ruleset_version))
        if callable(self._script):
            out = self._script(invoice, ruleset_version)
        else:
            out = self._script[min(self._n, len(self._script) - 1)]
            self._n += 1
        if isinstance(out, Exception):
            raise out
        return out
