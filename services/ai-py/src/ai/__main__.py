"""Entrypoint for the ai-py worker: the health server, the Phase 0 invoice loop and the Track B consumers."""

import asyncio
import logging
import signal

from ai import telemetry
from ai.gateway.factory import build_gateway
from ai.service import bootstrap, documents_consumer, tasks_consumer
from ai.service.storage import S3Fetcher, make_minio_client
from ai.settings import Settings
from ai.worker import _connect_with_retry, _shutdown_nc, run, serve_health

logger = logging.getLogger(__name__)


async def serve_track_b(settings: Settings, stop_event: asyncio.Event) -> None:
    """`ai-documents` and `ai-agent-tasks` on one NATS connection, which also carries their agent events and
    results. Everything that needs no network is built first, so a bad config or a broken agent entry point
    fails start-up; Valkey, S3 and the model provider are reached lazily, on the first message."""
    gateway = build_gateway(settings)
    registry = bootstrap.build_registry(settings)
    verifier = bootstrap.build_verifier(settings)
    fetch = S3Fetcher(make_minio_client(settings), settings.s3_bucket)

    nc = await _connect_with_retry(stop_event)
    if nc is None:  # stopped before NATS was reachable
        return
    try:
        js = nc.jetstream()
        await bootstrap.ensure_streams(js)
        publish = bootstrap.make_publish(js)
        async with asyncio.TaskGroup() as tg:  # one consumer failing stops the other: the process restarts
            tg.create_task(documents_consumer.run(js, gateway=gateway, registry=registry, verifier=verifier,
                                                  fetch=fetch, publish=publish, settings=settings,
                                                  stop_event=stop_event))
            tg.create_task(tasks_consumer.run(js, gateway=gateway, registry=registry, settings=settings,
                                              stop_event=stop_event))
    finally:
        await _shutdown_nc(nc)


async def main() -> None:
    provider = telemetry.init("ai-py")
    settings = Settings.from_env()
    stop_event = asyncio.Event()

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, stop_event.set)

    try:
        await asyncio.gather(serve_health(port=settings.health_port, stop_event=stop_event),
                             run(stop_event=stop_event), serve_track_b(settings, stop_event))
    finally:
        # Flush any queued spans before the process exits -- otherwise a
        # SIGTERM (or a crash out of the gather above) drops them silently.
        provider.shutdown()


if __name__ == "__main__":
    asyncio.run(main())
