from opentelemetry import trace
from opentelemetry.trace import NonRecordingSpan, SpanContext, TraceFlags
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from ai.carrier import getter, setter


def test_round_trip():
    sc = SpanContext(
        trace_id=0x4BF92F3577B34DA6A3CE929D0E0E4736,
        span_id=0x00F067AA0BA902B7,
        is_remote=False,
        trace_flags=TraceFlags(1),
    )
    ctx = trace.set_span_in_context(NonRecordingSpan(sc))
    headers: dict[str, str] = {}
    prop = TraceContextTextMapPropagator()
    prop.inject(headers, context=ctx, setter=setter)
    assert "traceparent" in headers
    got = trace.get_current_span(prop.extract(headers, getter=getter)).get_span_context()
    assert got.trace_id == sc.trace_id
