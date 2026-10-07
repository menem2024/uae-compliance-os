"""S3Fetcher: the S3-backed FetchFn of document_ingestion@1 (Task 22's `fetch` node).

It only reads bytes. The fetch node still refuses an object key outside the Firm's prefix before calling it
and re-checks sha256 after it returns. Any S3 error response (NoSuchKey, NoSuchBucket, AccessDenied...) is the
permanent `object_missing`; a transport failure or a 5xx is a ToolTransientError, so the node retries and,
if the outage outlasts its RetryPolicy, the message is nak'ed instead of the Document failing.
"""

from __future__ import annotations

import asyncio
from typing import Any, Protocol

from minio import Minio
from minio.error import S3Error

from ai.agents.orchestrator.fetch import FetchError
from ai.runtime.errors import ToolTransientError
from ai.settings import Settings


class _Response(Protocol):
    def read(self) -> bytes: ...
    def close(self) -> None: ...
    def release_conn(self) -> None: ...


class ObjectReader(Protocol):
    """The sync slice of minio.Minio that S3Fetcher uses."""

    def get_object(self, bucket_name: str, object_name: str) -> Any: ...


def make_minio_client(settings: Settings) -> Minio:
    """Lazy: the constructor opens no connection."""
    return Minio(settings.s3_endpoint, access_key=settings.s3_access_key or None,
                 secret_key=settings.s3_secret_key or None, secure=settings.s3_use_ssl,
                 region=settings.s3_region)


class S3Fetcher:
    """Implements orchestrator.fetch.FetchFn: `await fetcher(object_key) -> bytes`."""

    def __init__(self, client: ObjectReader, bucket: str) -> None:
        self._client = client
        self._bucket = bucket

    def _read(self, object_key: str) -> bytes:
        resp: _Response = self._client.get_object(self._bucket, object_key)
        try:
            return resp.read()
        finally:
            resp.close()
            resp.release_conn()

    async def __call__(self, object_key: str) -> bytes:
        try:
            return await asyncio.to_thread(self._read, object_key)  # the minio client is synchronous
        except S3Error:
            raise FetchError("object_missing") from None
        except Exception as exc:  # ServerError, urllib3/OS errors: the store is down, not the object gone
            raise ToolTransientError(f"object store unavailable: {type(exc).__name__}") from exc
