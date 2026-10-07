import base64

import pytest
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from ai import telemetry


def _processors(provider) -> list:
    return list(provider._active_span_processor._span_processors)


@pytest.fixture(autouse=True)
def _env(monkeypatch):
    for k in ("LANGFUSE_OTLP_ENDPOINT", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY"):
        monkeypatch.delenv(k, raising=False)
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317")


def test_langfuse_off_by_default():
    provider = telemetry.init("t")
    try:
        procs = _processors(provider)
        assert len(procs) == 1 and isinstance(procs[0], BatchSpanProcessor)
    finally:
        provider.shutdown()


def test_langfuse_adds_second_processor(monkeypatch):
    monkeypatch.setenv("LANGFUSE_OTLP_ENDPOINT", "http://127.0.0.1:3000/api/public/otel/v1/traces")
    monkeypatch.setenv("LANGFUSE_PUBLIC_KEY", "pk-lf-1")
    monkeypatch.setenv("LANGFUSE_SECRET_KEY", "sk-lf-2")
    seen: dict = {}
    real = telemetry.OTLPHttpSpanExporter

    def spy(**kw):
        seen.update(kw)
        return real(**kw)

    monkeypatch.setattr(telemetry, "OTLPHttpSpanExporter", spy)
    provider = telemetry.init("t")
    try:
        procs = _processors(provider)
        assert len(procs) == 2 and all(isinstance(p, BatchSpanProcessor) for p in procs)
        assert procs[1].span_exporter._endpoint == "http://127.0.0.1:3000/api/public/otel/v1/traces"
        assert seen["endpoint"] == "http://127.0.0.1:3000/api/public/otel/v1/traces"
        want = "Basic " + base64.b64encode(b"pk-lf-1:sk-lf-2").decode()
        assert seen["headers"] == {"Authorization": want}
    finally:
        provider.shutdown()
