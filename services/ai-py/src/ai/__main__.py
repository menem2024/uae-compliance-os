"""Entrypoint for the ai-py worker."""

import asyncio
import logging
import signal

from ai import telemetry
from ai.worker import run, serve_health

logger = logging.getLogger(__name__)


async def main() -> None:
    provider = telemetry.init("ai-py")
    stop_event = asyncio.Event()

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, stop_event.set)

    try:
        await asyncio.gather(serve_health(stop_event=stop_event), run(stop_event=stop_event))
    finally:
        # Flush any queued spans before the process exits -- otherwise a
        # SIGTERM (or a crash out of the gather above) drops them silently.
        provider.shutdown()


if __name__ == "__main__":
    asyncio.run(main())
