"""NATS JetStream worker: consumes invoice.submitted, produces invoice.extracted.

Phase 0 walking skeleton — no LLM calls. Trace context is propagated over NATS
message headers using the W3C traceparent format.
"""

import asyncio
import logging
import os

import nats
from nats.js.api import ConsumerConfig
from nats.js.errors import BadRequestError, FetchTimeoutError
from opentelemetry import trace
from opentelemetry.trace import SpanKind
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from ai.agents.stub_extraction import extract
from ai.carrier import getter, setter
from ai.gen.compliance.v1 import events_pb2

logger = logging.getLogger(__name__)

STREAM_SUBJECT = "invoice.submitted"
STREAM_DURABLE = "ai-extraction"
MAX_DELIVER = 5
DLQ_SUBJECT = "dlq.invoice.submitted"

tracer = trace.get_tracer(__name__)
propagator = TraceContextTextMapPropagator()


async def _ensure_stream(js, *, name: str, subjects: list[str]) -> None:
    try:
        await js.add_stream(name=name, subjects=subjects)
    except BadRequestError as exc:
        description = (exc.description or "").lower()
        if "already in use" not in description:
            raise


async def _handle(js, msg) -> None:
    ctx = propagator.extract(dict(msg.headers or {}), getter=getter)
    with tracer.start_as_current_span("invoice.submitted process", context=ctx, kind=SpanKind.CONSUMER):
        try:
            ev = events_pb2.InvoiceSubmitted()
            ev.ParseFromString(msg.data)
            out = extract(ev)
            with tracer.start_as_current_span("invoice.extracted publish", kind=SpanKind.PRODUCER):
                headers: dict[str, str] = {}
                propagator.inject(headers, setter=setter)
                await js.publish("invoice.extracted", out.SerializeToString(), headers=headers)
            await msg.ack()
        except Exception:
            logger.exception("failed to process invoice.submitted message")
            if msg.metadata.num_delivered >= MAX_DELIVER:
                await js.publish(DLQ_SUBJECT, msg.data, headers=dict(msg.headers or {}))
                await msg.term()
            else:
                await msg.nak(delay=2)


async def run() -> None:
    nc = await nats.connect(os.environ["NATS_URL"])
    js = nc.jetstream()

    await _ensure_stream(js, name="INVOICES", subjects=["invoice.>"])
    await _ensure_stream(js, name="DLQ", subjects=["dlq.>"])

    sub = await js.pull_subscribe(
        STREAM_SUBJECT,
        durable=STREAM_DURABLE,
        config=ConsumerConfig(max_deliver=MAX_DELIVER, ack_wait=30),
    )

    try:
        while True:
            try:
                msgs = await sub.fetch(10, timeout=5)
            except (FetchTimeoutError, TimeoutError):
                continue
            for msg in msgs:
                await _handle(js, msg)
    finally:
        await nc.drain()


async def _handle_health(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
    try:
        await reader.read(1024)
        writer.write(b"HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\nok")
        await writer.drain()
    finally:
        writer.close()


async def serve_health(port: int = 8081) -> None:
    server = await asyncio.start_server(_handle_health, host="0.0.0.0", port=port)
    async with server:
        await server.serve_forever()
