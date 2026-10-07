"""S3Fetcher: the S3-backed FetchFn of document_ingestion@1 (the fetch node re-checks sha256 and tenant)."""

import threading

import pytest
from minio import Minio
from minio.error import S3Error, ServerError

from ai.agents.orchestrator.fetch import FetchError
from ai.runtime.errors import ToolTransientError
from ai.service.storage import S3Fetcher, make_minio_client
from ai.settings import Settings

KEY = "firms/f1/docs/d1"


class FakeResponse:
    def __init__(self, data: bytes) -> None:
        self._data = data
        self.closed = self.released = False

    def read(self) -> bytes:
        return self._data

    def close(self) -> None:
        self.closed = True

    def release_conn(self) -> None:
        self.released = True


def s3_error(code: str) -> S3Error:
    return S3Error(None, code, "msg", KEY, "req-1", "host-1", bucket_name="documents", object_name=KEY)  # type: ignore[arg-type]


class FakeMinio:
    """The sync slice of minio.Minio that S3Fetcher uses."""

    def __init__(self, objects: dict[str, bytes], error: Exception | None = None) -> None:
        self.objects = objects
        self.error = error
        self.asked: list[tuple[str, str]] = []
        self.threads: list[str] = []
        self.responses: list[FakeResponse] = []

    def get_object(self, bucket: str, key: str) -> FakeResponse:
        self.asked.append((bucket, key))
        self.threads.append(threading.current_thread().name)
        if self.error is not None:
            raise self.error
        if key not in self.objects:
            raise s3_error("NoSuchKey")
        resp = FakeResponse(self.objects[key])
        self.responses.append(resp)
        return resp


async def test_present_object_returns_its_bytes_off_the_event_loop():
    client = FakeMinio({KEY: b"%PDF-1.7 bytes"})
    data = await S3Fetcher(client, "documents")(KEY)
    assert data == b"%PDF-1.7 bytes"
    assert client.asked == [("documents", KEY)]
    assert client.threads != [threading.main_thread().name]  # the sync client runs in a worker thread
    assert client.responses[0].closed and client.responses[0].released


@pytest.mark.parametrize("code", ["NoSuchKey", "NoSuchBucket", "AccessDenied"])
async def test_any_s3_error_is_object_missing(code):
    with pytest.raises(FetchError) as ei:
        await S3Fetcher(FakeMinio({}, s3_error(code)), "documents")(KEY)
    assert ei.value.code == "object_missing"


async def test_absent_key_is_object_missing():
    with pytest.raises(FetchError) as ei:
        await S3Fetcher(FakeMinio({}), "documents")("firms/f1/docs/missing")
    assert ei.value.code == "object_missing"


@pytest.mark.parametrize("exc", [ServerError("server failed with HTTP status code 503", 503),
                                 ConnectionRefusedError("refused"), OSError("network down")])
async def test_an_outage_is_transient_so_the_node_retries_and_the_message_is_naked(exc):
    with pytest.raises(ToolTransientError):
        await S3Fetcher(FakeMinio({}, exc), "documents")(KEY)


def test_make_minio_client_is_lazy_and_uses_the_settings():
    s = Settings(s3_endpoint="127.0.0.1:1", s3_access_key="minio", s3_secret_key="minio_dev_pw",
                 s3_use_ssl=False, s3_region="us-east-1")
    client = make_minio_client(s)  # no connection is made here
    assert isinstance(client, Minio)
    assert client._base_url.host == "127.0.0.1:1" and not client._base_url.is_https  # type: ignore[attr-defined]
    assert client._base_url.region == "us-east-1"  # type: ignore[attr-defined]
