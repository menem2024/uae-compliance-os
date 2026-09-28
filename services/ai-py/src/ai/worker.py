"""NATS JetStream worker: consumes invoice.submitted, produces invoice.extracted.

Phase 0 walking skeleton — no LLM calls. Trace context is propagated over NATS
message headers using the W3C traceparent format.
"""

import asyncio
import contextlib
import logging
import os

import nats
from google.protobuf import message
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

FETCH_TIMEOUT = 1.0
DRAIN_TIMEOUT = 3.0
DLQ_PUBLISH_ATTEMPTS = 3
DLQ_PUBLISH_BACKOFF = 0.05

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
            await _fail(js, msg)


async def _fail(js, msg) -> None:
    """Route a failed message to the DLQ or nak it for redelivery.

    Deliberately isolated in its own try/except: a bad message must not take
    down the worker loop even if *this* recovery path also fails (e.g. the
    NATS connection is gone so the DLQ publish, msg.term() or msg.nak() all
    raise). Any such failure is logged and swallowed so the fetch loop in
    run() keeps going and other fetched messages are still handled.
    """
    try:
        if msg.metadata.num_delivered >= MAX_DELIVER:
            await _dead_letter(js, msg)
        else:
            await msg.nak(delay=2)
    except Exception:
        logger.exception("failed to route failed invoice.submitted message to DLQ/nak")


def _safe_invoice_id(data: bytes) -> str:
    """Best-effort invoice_id for error logs. Never raises: a message that
    failed processing may also fail to parse.
    """
    try:
        ev = events_pb2.InvoiceSubmitted()
        ev.ParseFromString(data)
        return ev.invoice_id
    except message.DecodeError as exc:
        logger.debug("could not parse invoice_id for error log", exc_info=exc)
        return "<unparseable>"


def _safe_stream_seq(msg) -> object:
    """Best-effort stream sequence number for error logs. Never raises."""
    try:
        return msg.metadata.sequence.stream
    except AttributeError as exc:
        logger.debug("could not read stream sequence for error log", exc_info=exc)
        return None


async def _dead_letter(js, msg) -> None:
    """Publish a permanently-failed message to the DLQ, then term() it.

    msg.term() is attempted unconditionally in a `finally`, regardless of
    whether the DLQ publish ultimately succeeded: at MAX_DELIVER, JetStream
    will not redeliver the message again, so skipping term() would strand it
    forever (neither dead-lettered, acked, nor termed). The DLQ publish is
    retried a few times with a short backoff before being treated as failed;
    if it never succeeds, that is logged at ERROR with enough to locate the
    message (subject, stream sequence, invoice_id) but never the payload
    itself, which contains invoice data.
    """
    published = False
    last_exc: Exception | None = None
    try:
        for attempt in range(DLQ_PUBLISH_ATTEMPTS):
            try:
                await js.publish(DLQ_SUBJECT, msg.data, headers=dict(msg.headers or {}))
                published = True
                break
            except Exception as exc:
                last_exc = exc
                logger.debug("dlq publish attempt %d failed", attempt + 1, exc_info=exc)
                if attempt < DLQ_PUBLISH_ATTEMPTS - 1:
                    await asyncio.sleep(DLQ_PUBLISH_BACKOFF * (attempt + 1))

        if not published:
            try:
                logger.error(
                    "dead-letter publish failed after %d attempts subject=%s stream_seq=%s invoice_id=%s",
                    DLQ_PUBLISH_ATTEMPTS,
                    DLQ_SUBJECT,
                    _safe_stream_seq(msg),
                    _safe_invoice_id(msg.data),
                    exc_info=last_exc,
                )
            except Exception:
                logger.exception("failed to log dead-letter publish failure")
    finally:
        try:
            await msg.term()
        except Exception:
            logger.exception("failed to term() dead-lettered message")


async def _connect_with_retry(
    stop_event: asyncio.Event, *, base_delay: float = 1.0, max_delay: float = 30.0
):
    """Connect to NATS, retrying with backoff instead of raising.

    A NATS outage at startup must not crash the process -- the health server
    has to keep serving while we retry. The nats client's own default
    reconnect settings can keep a single `nats.connect()` call retrying
    internally for minutes, which would make the process ignore SIGTERM for
    just as long; each attempt is therefore bounded and, more importantly,
    raced against stop_event so a shutdown signal cancels an in-flight
    attempt immediately instead of waiting it out. Returns None if
    stop_event is set before a connection succeeds.
    """
    delay = base_delay
    while not stop_event.is_set():
        connect_task = asyncio.ensure_future(
            nats.connect(
                os.environ["NATS_URL"],
                connect_timeout=5,
                max_reconnect_attempts=3,
                reconnect_time_wait=2,
                drain_timeout=int(DRAIN_TIMEOUT),
            )
        )
        stop_wait = asyncio.ensure_future(stop_event.wait())
        try:
            done, _ = await asyncio.wait({connect_task, stop_wait}, return_when=asyncio.FIRST_COMPLETED)

            if connect_task not in done:
                connect_task.cancel()
                with contextlib.suppress(BaseException):
                    await connect_task
                return None

            try:
                return connect_task.result()
            except Exception:
                logger.exception("failed to connect to NATS, retrying in %.1fs", delay)
                try:
                    await asyncio.wait_for(stop_event.wait(), timeout=delay)
                except TimeoutError:
                    pass
                delay = min(delay * 2, max_delay)
        finally:
            if not stop_wait.done():
                stop_wait.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await stop_wait
    return None


async def run(stop_event: asyncio.Event | None = None) -> None:
    stop_event = stop_event if stop_event is not None else asyncio.Event()

    nc = await _connect_with_retry(stop_event)
    if nc is None:
        return

    js = nc.jetstream()
    try:
        await _ensure_stream(js, name="INVOICES", subjects=["invoice.>"])
        await _ensure_stream(js, name="DLQ", subjects=["dlq.>"])

        sub = await js.pull_subscribe(
            STREAM_SUBJECT,
            durable=STREAM_DURABLE,
            config=ConsumerConfig(max_deliver=MAX_DELIVER, ack_wait=30),
        )

        while not stop_event.is_set():
            try:
                msgs = await sub.fetch(10, timeout=FETCH_TIMEOUT)
            except (FetchTimeoutError, TimeoutError):
                continue
            for msg in msgs:
                await _handle(js, msg)
    finally:
        await _shutdown_nc(nc)


async def _shutdown_nc(nc) -> None:
    """Bound NATS shutdown so it can never block process exit under SIGTERM.

    A plain `await nc.drain()` on a connection with an active JetStream pull
    subscription has been observed to block for 20+ seconds (well past the
    orchestrator's stop grace period), which stops the caller from ever
    reaching `provider.shutdown()`. Give drain a short budget and fall back
    to a hard close if it doesn't finish in time.
    """
    try:
        await asyncio.wait_for(nc.drain(), timeout=DRAIN_TIMEOUT)
    except Exception:
        logger.exception("nc.drain() failed or timed out; closing connection instead")
        with contextlib.suppress(Exception):
            await nc.close()


async def _handle_health(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
    try:
        await reader.read(1024)
        writer.write(b"HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\nok")
        await writer.drain()
    finally:
        writer.close()


async def serve_health(port: int = 8081, stop_event: asyncio.Event | None = None) -> None:
    server = await asyncio.start_server(_handle_health, host="0.0.0.0", port=port)
    async with server:
        if stop_event is not None:
            await stop_event.wait()
        else:
            await server.serve_forever()
