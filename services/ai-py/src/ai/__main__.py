"""Entrypoint for the ai-py worker."""

import asyncio

from ai import telemetry
from ai.worker import run, serve_health


async def main() -> None:
    await asyncio.gather(serve_health(), run())


if __name__ == "__main__":
    telemetry.init("ai-py")
    asyncio.run(main())
