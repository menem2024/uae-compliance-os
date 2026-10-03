"""Service wiring shared by the `ai-documents` and `ai-agent-tasks` consumers (contract section 4).

Streams, the agent registry, the Verifier, the document result publisher, and the JetStream plumbing both
consumers use: dead-lettering, the `in_progress()` heartbeat and a bounded-concurrency pull loop. The shape
mirrors ai/worker.py (Phase 0): ensure streams, pull, handle, ack / nak / dead-letter, never let one message
take the loop down. Nothing here touches Postgres (rule 3). No payload is ever logged.
"""

from __future__ import annotations

import asyncio
import contextlib
import logging
from collections.abc import AsyncIterator, Awaitable, Callable
from typing import Any

from google.protobuf.message import Message
from nats.js.api import RetentionPolicy, StorageType, StreamConfig
from nats.js.errors import BadRequestError, FetchTimeoutError
from opentelemetry.propagate import inject

from ai.agents.orchestrator.workflow import PublishFn
from ai.agents.verifier.agent import VerifierAgent
from ai.agents.verifier.critic import ESCALATION_PROMPT_ID, InvoiceCritic
from ai.agents.verifier.invoice import invoice_profile
from ai.gen.compliance.v1 import documents_pb2
from ai.runtime.registry import AgentRegistry, load_registry
from ai.settings import Settings

logger = logging.getLogger(__name__)

DAY_S = 86400.0
DUPLICATE_WINDOW_S = 120.0
# Exactly api-go's events.EnsureStreams (internal/events/nats.go, trackb.go). DLQ is created here too, so a
# dead letter never depends on which service started first.
STREAMS: tuple[StreamConfig, ...] = (
    StreamConfig(name="DOCUMENTS", subjects=["document.>"], retention=RetentionPolicy.LIMITS,
                 storage=StorageType.FILE, max_age=30 * DAY_S, duplicate_window=DUPLICATE_WINDOW_S),
    StreamConfig(name="AGENTS", subjects=["agent.>"], retention=RetentionPolicy.LIMITS,
                 storage=StorageType.FILE, max_age=7 * DAY_S, duplicate_window=DUPLICATE_WINDOW_S),
    StreamConfig(name="DLQ", subjects=["dlq.>"], retention=RetentionPolicy.LIMITS, storage=StorageType.FILE),
)

PUBLISH_TIMEOUT_S = 5.0
DLQ_PUBLISH_ATTEMPTS = 3
DLQ_PUBLISH_BACKOFF_S = 0.05
HEARTBEAT_S = 20.0  # msg.in_progress() cadence while a run is in flight (ack_wait is 60 s)
NAK_DELAY_S = 30.0  # redelivery delay after a failure that outlived the node RetryPolicy
FETCH_TIMEOUT_S = 1.0  # bounds how long the loop takes to notice stop_event

type Sleep = Callable[[float], Awaitable[None]]


# ------------------------------------------------------------------ streams and wiring
async def ensure_streams(js: Any) -> None:
    """Creates DOCUMENTS, AGENTS and DLQ. Idempotent in either order with api-go: an identical create is a
    no-op, and a stream that already exists with other settings is left as it is."""
    for cfg in STREAMS:
        try:
            await js.add_stream(config=cfg)
        except BadRequestError as exc:
            if "already in use" not in (exc.description or "").lower():
                raise


def build_registry(settings: Settings) -> AgentRegistry:
    """Every `compliance.agents` entry point (intake, extraction, and later Tracks C/D). Fails fast."""
    del settings  # the plugins read Settings.from_env() themselves (spec amendment 4)
    return load_registry()


def build_verifier(settings: Settings) -> VerifierAgent:
    """The one VerifierAgent of the service: the invoice profile with its critic and its escalation critic."""
    return VerifierAgent([invoice_profile(
        critic=InvoiceCritic(model=settings.model_critic),
        escalation_critic=InvoiceCritic(model=settings.model_escalation, prompt_id=ESCALATION_PROMPT_ID))])


# ------------------------------------------------------------------ publishing
async def publish_proto(js: Any, subject: str, msg_id: str, msg: Message, *,
                        timeout_s: float = PUBLISH_TIMEOUT_S) -> None:
    """Publishes with the W3C trace context and the contract Nats-Msg-Id. Raises on failure."""
    headers: dict[str, str] = {}
    inject(headers)
    headers["Nats-Msg-Id"] = msg_id
    await js.publish(subject, msg.SerializeToString(), timeout=timeout_s, headers=headers)


def make_publish(js: Any) -> PublishFn:
    """workflow.PublishFn: document.extracted / document.failed with their contract section 4 msg ids.
    A failure raises, so finalize raises and the upload is nak'ed rather than acked without a result."""

    async def publish(msg: documents_pb2.DocumentExtracted | documents_pb2.DocumentFailed) -> None:
        if isinstance(msg, documents_pb2.DocumentExtracted):
            subject = "document.extracted"
        elif isinstance(msg, documents_pb2.DocumentFailed):
            subject = "document.failed"
        else:
            raise TypeError(f"not a document result: {type(msg).__name__}")
        await publish_proto(js, subject, f"{subject}:{msg.document_id}:{msg.run_id}", msg)

    return publish


async def dead_letter(js: Any, msg: Any, subject: str, *, backoff_s: float = DLQ_PUBLISH_BACKOFF_S) -> None:
    """Publishes the raw message to `subject` (a few attempts), then term()s it whatever happened: a
    message at max_deliver that is neither acked nor termed would be stranded. Never raises; never logs the
    payload."""
    try:
        for attempt in range(1, DLQ_PUBLISH_ATTEMPTS + 1):
            try:
                await js.publish(subject, msg.data, timeout=PUBLISH_TIMEOUT_S,
                                 headers=dict(msg.headers or {}))
                break
            except Exception:
                logger.debug("dlq publish attempt %d failed subject=%s", attempt, subject, exc_info=True)
                if attempt < DLQ_PUBLISH_ATTEMPTS:
                    await asyncio.sleep(backoff_s * attempt)
        else:
            logger.error("dead-letter publish failed subject=%s stream_seq=%s", subject, _stream_seq(msg))
    finally:
        try:
            await msg.term()
        except Exception:
            logger.exception("failed to term() a dead-lettered message subject=%s", subject)


def _stream_seq(msg: Any) -> object:
    try:
        return msg.metadata.sequence.stream
    except AttributeError:
        return None


async def settle(what: str, op: Awaitable[None]) -> None:
    """ack/nak/term without letting a lost connection escape: JetStream redelivers after ack_wait anyway."""
    try:
        await op
    except Exception:
        logger.warning("message %s failed", what, exc_info=True)


# ------------------------------------------------------------------ heartbeat and pull loop
@contextlib.asynccontextmanager
async def heartbeat(msg: Any, *, interval_s: float = HEARTBEAT_S,
                    sleep: Sleep = asyncio.sleep) -> AsyncIterator[None]:
    """Calls msg.in_progress() every `interval_s` while the block runs. The beat task is cancelled and awaited
    before the block's exit returns, so it can never fire after the caller acks, naks or terms."""

    async def beat() -> None:
        while True:
            await sleep(interval_s)
            try:
                await msg.in_progress()
            except Exception:
                logger.warning("in_progress() failed", exc_info=True)

    task = asyncio.create_task(beat())
    try:
        yield
    finally:
        task.cancel()
        await asyncio.wait({task})  # never raises the beat's CancelledError; our own cancellation propagates


def heartbeat_interval_s(ack_wait_s: float) -> float:
    """HEARTBEAT_S, or a third of ack_wait when AI_ACK_WAIT_S is lower: a run must never outlive its ack."""
    return min(HEARTBEAT_S, ack_wait_s / 3)


def _log_crash(task: asyncio.Task[None]) -> None:
    if not task.cancelled() and task.exception() is not None:
        logger.error("message handler crashed", exc_info=task.exception())


async def consume(sub: Any, handle: Callable[[Any], Awaitable[None]], *, max_in_flight: int,
                  stop_event: asyncio.Event, grace_s: float) -> None:
    """Pulls at most `max_in_flight` messages at a time and handles each in its own task. On stop it fetches
    nothing more, gives in-flight handlers `grace_s` to finish, then cancels the rest (each handler naks its
    message on cancellation, so another worker picks it up at once)."""
    inflight: set[asyncio.Task[None]] = set()
    try:
        while not stop_event.is_set():
            room = max_in_flight - len(inflight)
            if room <= 0:
                await asyncio.wait(inflight, timeout=FETCH_TIMEOUT_S, return_when=asyncio.FIRST_COMPLETED)
                continue
            try:
                msgs = await sub.fetch(room, timeout=FETCH_TIMEOUT_S)
            except (FetchTimeoutError, TimeoutError):
                continue
            for m in msgs:
                task = asyncio.create_task(handle(m))
                inflight.add(task)
                task.add_done_callback(inflight.discard)
                task.add_done_callback(_log_crash)
    finally:
        if inflight:
            _, pending = await asyncio.wait(set(inflight), timeout=grace_s)
            for task in pending:
                task.cancel()
            if pending:
                await asyncio.wait(pending)
        # Drop the pull inbox (the durable stays): nc.drain() otherwise waits out its whole timeout on it.
        try:
            await sub.unsubscribe()
        except Exception:
            logger.debug("pull subscription unsubscribe failed", exc_info=True)
