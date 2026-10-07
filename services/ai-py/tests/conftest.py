"""Shared test setup.

1. Blocks every non-loopback socket connect for the whole session (contract rule 2, spec AC-4): no test
   can reach a model provider, S3 or NATS outside this machine.
2. Installs one in-memory span exporter so tests can assert span attributes.
"""

import socket
from collections.abc import Iterator

import pytest
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

_SPANS = InMemorySpanExporter()
_provider = TracerProvider()
_provider.add_span_processor(SimpleSpanProcessor(_SPANS))
trace.set_tracer_provider(_provider)

_real_connect = socket.socket.connect
_real_connect_ex = socket.socket.connect_ex


class NetworkBlockedError(RuntimeError):
    """Raised when a test tries to open a non-loopback connection."""


def _loopback(sock: socket.socket, address: object) -> bool:
    if hasattr(socket, "AF_UNIX") and sock.family == socket.AF_UNIX:
        return True
    host = address[0] if isinstance(address, tuple) else address
    return isinstance(host, str) and (host in {"localhost", "::1"} or host.startswith("127."))


def _guarded_connect(self: socket.socket, address: object) -> None:
    if not _loopback(self, address):
        raise NetworkBlockedError(f"tests may not connect to {address!r}")
    return _real_connect(self, address)


def _guarded_connect_ex(self: socket.socket, address: object) -> int:
    if not _loopback(self, address):
        raise NetworkBlockedError(f"tests may not connect to {address!r}")
    return _real_connect_ex(self, address)


def pytest_configure(config: pytest.Config) -> None:
    socket.socket.connect = _guarded_connect  # type: ignore[method-assign]
    socket.socket.connect_ex = _guarded_connect_ex  # type: ignore[method-assign]


@pytest.fixture
def spans() -> Iterator[InMemorySpanExporter]:
    _SPANS.clear()
    yield _SPANS
    _SPANS.clear()
