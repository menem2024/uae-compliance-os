"""AC-4: no test can open a socket to a non-loopback host (the guard lives in tests/conftest.py)."""

import asyncio
import socket

import pytest

BLOCKED = "tests may not connect"


def test_non_loopback_connect_is_blocked():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s, pytest.raises(RuntimeError, match=BLOCKED):
        s.connect(("203.0.113.10", 443))  # TEST-NET-3, never routable


async def test_asyncio_connect_is_blocked():
    with pytest.raises(RuntimeError, match=BLOCKED):
        await asyncio.open_connection("203.0.113.10", 443)


async def test_loopback_is_allowed():
    async def echo(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        writer.write(await reader.read(2))
        await writer.drain()
        writer.close()

    server = await asyncio.start_server(echo, "127.0.0.1", 0)
    port = server.sockets[0].getsockname()[1]
    async with server:
        reader, writer = await asyncio.open_connection("127.0.0.1", port)
        writer.write(b"ok")
        await writer.drain()
        assert await reader.read(2) == b"ok"
        writer.close()
