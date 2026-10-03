"""The `ai-documents` consumer: one document_ingestion@1 run per `document.uploaded` (contract section 4).

Binding decisions (plan Task 21; AC-E2: nothing is lost, nothing duplicates):
- any terminal RunOutcome is acked: `finalize` already published document.extracted or document.failed (a
  budget stop included, which still has a result);
- an exception escaping GraphExecutor.run (finalize's RetryLater after a transient failure outlived the node
  RetryPolicy, or a failed result publish) naks with a 30 s delay, and nothing was published;
- that same failure on delivery `max_deliver` dead-letters the message (dlq.document.uploaded, then term) and
  publishes document.failed/max_deliver_exceeded, so the Document never stays `processing`;
- an undecodable message is dead-lettered at once (it can never succeed and has no document id to fail);
- while a run is in flight, msg.in_progress() every 20 s keeps ack_wait (60 s) from expiring; the beat stops
  before the message is settled;
- on shutdown, a run that outlives the grace period is cancelled and its message nak'ed for immediate
  redelivery (the orphaned agent run is abandoned by api-go when the redelivered run starts).
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any

from google.protobuf.message import DecodeError
from nats.js.api import AckPolicy, ConsumerConfig

from ai.agents.orchestrator import workflow
from ai.agents.orchestrator.assemble import build_failed
from ai.agents.orchestrator.fetch import FetchFn
from ai.agents.verifier.agent import VerifierAgent
from ai.gateway.types import ModelGateway
from ai.gen.compliance.v1 import documents_pb2
from ai.runtime.context import error_code_of
from ai.runtime.events import EventSink
from ai.runtime.graph import GraphExecutor
from ai.runtime.nats_sink import NatsSink
from ai.runtime.registry import AgentRegistry
from ai.runtime.types import RunIdentity
from ai.service import bootstrap
from ai.service.bootstrap import Sleep
from ai.settings import Settings

logger = logging.getLogger(__name__)

DOCS_DURABLE = "ai-documents"
DOCS_SUBJECT = "document.uploaded"
DOCS_STREAM = "DOCUMENTS"
DLQ_SUBJECT = "dlq.document.uploaded"
MAX_DELIVER = 5
HEARTBEAT_S = bootstrap.HEARTBEAT_S
NAK_DELAY_S = bootstrap.NAK_DELAY_S
SHUTDOWN_GRACE_S = 3.0  # AC-9: the whole process stops in < 10 s


def consumer_config(settings: Settings) -> ConsumerConfig:
    return ConsumerConfig(durable_name=DOCS_DURABLE, filter_subject=DOCS_SUBJECT,
                          ack_policy=AckPolicy.EXPLICIT, max_deliver=MAX_DELIVER,
                          ack_wait=settings.ack_wait_s, max_ack_pending=settings.docs_max_ack_pending)


class _Handler:
    def __init__(self, js: Any, executor: GraphExecutor, *, registry: AgentRegistry, verifier: VerifierAgent,
                 fetch: FetchFn, publish: workflow.PublishFn, settings: Settings, sleep: Sleep) -> None:
        self._js = js
        self._executor = executor
        self._registry = registry
        self._verifier = verifier
        self._fetch = fetch
        self._publish = publish
        self._settings = settings
        self._sleep = sleep
        self._beat_s = bootstrap.heartbeat_interval_s(settings.ack_wait_s)

    async def __call__(self, msg: Any) -> None:
        attempt = msg.metadata.num_delivered
        try:
            upload = documents_pb2.DocumentUploaded.FromString(msg.data)
        except DecodeError:
            logger.error("undecodable document.uploaded, dead-lettering attempt=%d", attempt)
            await bootstrap.dead_letter(self._js, msg, DLQ_SUBJECT)
            return
        identity = workflow.identity_of(upload, attempt)
        try:
            async with bootstrap.heartbeat(msg, interval_s=self._beat_s, sleep=self._sleep):
                graph = workflow.build_graph(upload, fetch=self._fetch, registry=self._registry,
                                             verifier=self._verifier,
                                             max_pdf_pages=self._settings.max_pdf_pages)
                await self._executor.run(graph, identity, workflow.BUDGET,
                                         finalize=workflow.build_finalize(upload, self._publish))
        except asyncio.CancelledError:  # shutdown: hand the message back now rather than after ack_wait
            await bootstrap.settle("nak", msg.nak())
            raise
        except Exception as exc:  # noqa: BLE001 - every escape is a retry, or a dead letter at max_deliver
            await self._failed(msg, upload, identity, exc)
            return
        await bootstrap.settle("ack", msg.ack())

    async def _failed(self, msg: Any, upload: documents_pb2.DocumentUploaded, identity: RunIdentity,
                      exc: Exception) -> None:
        code, attempt = error_code_of(exc), identity.delivery_attempt
        logger.debug("document run raised document_id=%s", upload.document_id, exc_info=exc)  # may echo input
        if attempt < MAX_DELIVER:
            logger.warning("document run failed, nak document_id=%s run_id=%s attempt=%d code=%s error=%s",
                           upload.document_id, identity.run_id, attempt, code, type(exc).__name__)
            await bootstrap.settle("nak", msg.nak(delay=NAK_DELAY_S))
            return
        logger.error("document run failed at max_deliver, dead-lettering document_id=%s run_id=%s code=%s",
                     upload.document_id, identity.run_id, code)
        try:
            await self._publish(build_failed(document_id=upload.document_id, firm_id=upload.firm_id,
                                             run_id=identity.run_id, reason_code="max_deliver_exceeded",
                                             detail=code))
        except Exception:
            logger.exception("document.failed publish failed document_id=%s", upload.document_id)
        await bootstrap.dead_letter(self._js, msg, DLQ_SUBJECT)


async def run(js: Any, *, gateway: ModelGateway, registry: AgentRegistry, verifier: VerifierAgent,
              fetch: FetchFn, publish: workflow.PublishFn, settings: Settings, stop_event: asyncio.Event,
              sink: EventSink | None = None, sleep: Sleep = asyncio.sleep) -> None:
    """Consumes until stop_event is set. `sink` and `sleep` (heartbeat timer and node retry backoff) are test
    seams; production uses NatsSink(js) and asyncio.sleep."""
    sub = await js.pull_subscribe(DOCS_SUBJECT, durable=DOCS_DURABLE, stream=DOCS_STREAM,
                                  config=consumer_config(settings))
    executor = GraphExecutor(gateway=gateway, sink=sink if sink is not None else NatsSink(js),
                             tools=registry.tools, hooks=(), sleep=sleep)
    handler = _Handler(js, executor, registry=registry, verifier=verifier, fetch=fetch, publish=publish,
                       settings=settings, sleep=sleep)
    await bootstrap.consume(sub, handler, max_in_flight=settings.docs_max_ack_pending, stop_event=stop_event,
                            grace_s=SHUTDOWN_GRACE_S)
