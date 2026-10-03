"""OpenTelemetry tracer provider setup for ai-py."""

import base64
import os

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter as OTLPHttpSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor


def init(service_name: str) -> TracerProvider:
    """Configure and register a TracerProvider exporting via OTLP/gRPC.

    The exporter's `timeout` bounds a single export call's deadline (including
    the gRPC client's own internal retries). It defaults to 10s in the SDK,
    which is far too long to sit inside `provider.shutdown()`'s flush when the
    OTLP collector is unreachable at shutdown time (e.g. it went down first) --
    that alone can eat the whole `docker stop` grace period. Bound it tighter
    by default so a stuck/unreachable collector can never block process exit;
    still overridable via OTEL_EXPORTER_OTLP_TIMEOUT for environments that
    need more slack.
    """
    provider = TracerProvider(resource=Resource.create({"service.name": service_name}))
    endpoint = os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT")
    timeout = float(os.environ.get("OTEL_EXPORTER_OTLP_TIMEOUT", "2"))
    exporter = OTLPSpanExporter(endpoint=endpoint, insecure=True, timeout=timeout)
    provider.add_span_processor(BatchSpanProcessor(exporter))
    _add_langfuse(provider, timeout)
    trace.set_tracer_provider(provider)
    return provider


def _add_langfuse(provider: TracerProvider, timeout: float) -> None:
    """Optional second processor (D2 option b): only when LANGFUSE_OTLP_ENDPOINT is set."""
    endpoint = os.environ.get("LANGFUSE_OTLP_ENDPOINT")
    if not endpoint:
        return
    pair = f"{os.environ.get('LANGFUSE_PUBLIC_KEY', '')}:{os.environ.get('LANGFUSE_SECRET_KEY', '')}"
    auth = "Basic " + base64.b64encode(pair.encode()).decode()
    provider.add_span_processor(BatchSpanProcessor(
        OTLPHttpSpanExporter(endpoint=endpoint, headers={"Authorization": auth}, timeout=timeout)))
