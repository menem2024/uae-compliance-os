"""`fetch` node (spec section 5.2): tenant prefix, sha256 re-check, magic-byte sniffing, PDF page count.

Every failure is a FetchError whose code becomes the critical run's error_code (contract section 3.5 rule 7).
"""

import hashlib
import io
import uuid
import zipfile
from collections.abc import Mapping

import pypdfium2 as pdfium
import pytest

from ai.agents.orchestrator.fetch import (
    CSV,
    JPEG,
    PDF,
    PNG,
    WEBP,
    XLSX,
    Fetched,
    FetchError,
    fetch_node,
    sniff,
)
from ai.gen.compliance.v1 import documents_pb2
from ai.runtime.clock import FakeClock
from ai.runtime.events import MemorySink
from ai.runtime.graph import GraphExecutor, TaskGraph
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import Budget, RunIdentity, RunStatus, StepKind

FIRM = "00000000-0000-4000-8000-00000000f001"
OTHER_FIRM = "00000000-0000-4000-8000-00000000f002"
DOC = "00000000-0000-4000-8000-00000000d001"
KEY = f"firms/{FIRM}/docs/{DOC}"


def pdf(pages: int) -> bytes:
    doc = pdfium.PdfDocument.new()
    for _ in range(pages):
        doc.new_page(595, 842)
    buf = io.BytesIO()
    doc.save(buf)
    doc.close()
    return buf.getvalue()


def upload(data: bytes, *, object_key: str = KEY, sha256: str | None = None) -> documents_pb2.DocumentUploaded:
    return documents_pb2.DocumentUploaded(document_id=DOC, firm_id=FIRM, client_company_id="cc",
                                          sha256=sha256 or hashlib.sha256(data).hexdigest(), object_key=object_key,
                                          content_type="application/pdf", size_bytes=len(data))


class Store:
    """A FetchFn over a dict; records every key it was asked for."""

    def __init__(self, objects: Mapping[str, bytes]) -> None:
        self.objects = dict(objects)
        self.asked: list[str] = []

    async def __call__(self, key: str) -> bytes:
        self.asked.append(key)
        try:
            return self.objects[key]
        except KeyError:
            raise FileNotFoundError("not found") from None


class Ctx:
    """The slice of NodeContext the fetch node uses."""

    def __init__(self) -> None:
        self.lines: list[tuple[str, dict[str, str]]] = []

    def emit(self, message_key: str, **args: str) -> None:
        self.lines.append((message_key, args))


async def fetched(up: documents_pb2.DocumentUploaded, store: Store) -> Fetched:
    return await fetch_node(up, store).fn(Ctx())  # type: ignore[arg-type]


async def fetch_error(up: documents_pb2.DocumentUploaded, store: Store) -> str:
    with pytest.raises(FetchError) as exc:
        await fetched(up, store)
    return exc.value.code


async def test_good_pdf_is_read_hashed_and_counted():
    data = pdf(3)
    store = Store({KEY: data})
    ctx = Ctx()
    node = fetch_node(upload(data), store)
    got = await node.fn(ctx)  # type: ignore[arg-type]
    assert got == Fetched(data, PDF, 3) and store.asked == [KEY]
    assert ctx.lines == [("orchestrator.fetched", {"format": "pdf", "pages": "3"})]
    assert (node.id, node.agent, node.kind, node.critical, node.depends_on) == (
        "fetch", "orchestrator", StepKind.DETERMINISTIC, True, ())


async def test_sha256_mismatch():
    data = pdf(1)
    assert await fetch_error(upload(data, sha256=hashlib.sha256(b"other").hexdigest()), Store({KEY: data})) \
        == "sha256_mismatch"


@pytest.mark.parametrize("key", [
    f"firms/{OTHER_FIRM}/docs/{DOC}",
    f"firms/{FIRM}/../{OTHER_FIRM}/docs/{DOC}",
    f"firms/{FIRM}{DOC}",  # the prefix must end at a path separator
    f"documents/{FIRM}/docs/{DOC}",
])
async def test_tenant_mismatch_never_reads_the_object(key):
    data = pdf(1)
    store = Store({key: data})
    assert await fetch_error(upload(data, object_key=key), store) == "tenant_mismatch"
    assert store.asked == []


@pytest.mark.parametrize("data", [b"\x00\x01\x02 binary blob", b"%PDF-1.7 not really a pdf", pdf(0)])
async def test_unrecognised_or_unreadable_format(data):
    assert await fetch_error(upload(data), Store({KEY: data})) == "unsupported_format"


async def test_missing_object():
    data = pdf(1)
    assert await fetch_error(upload(data), Store({})) == "object_missing"


async def test_a_fetch_error_from_the_fetcher_passes_through():
    async def fetcher(_key: str) -> bytes:
        raise FetchError("object_missing")

    with pytest.raises(FetchError) as exc:
        await fetch_node(upload(b"x"), fetcher).fn(Ctx())  # type: ignore[arg-type]
    assert exc.value.code == "object_missing"


def _xlsx_like() -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        zf.writestr("[Content_Types].xml", "<Types/>")
    return buf.getvalue()


@pytest.mark.parametrize(("data", "media_type", "pages"), [
    (b"\x89PNG\r\n\x1a\n" + b"\x00" * 16, PNG, 1),
    (b"\xff\xd8\xff\xe0" + b"\x00" * 16, JPEG, 1),
    (b"RIFF\x24\x00\x00\x00WEBPVP8 " + b"\x00" * 16, WEBP, 1),
    (_xlsx_like(), XLSX, 0),
    ("رقم الفاتورة,التاريخ\nINV-1,2026-01-01\n".encode(), CSV, 0),
])
async def test_format_comes_from_magic_bytes_not_the_declared_type(data, media_type, pages):
    assert sniff(data) == media_type
    assert await fetched(upload(data), Store({KEY: data})) == Fetched(data, media_type, pages)


async def test_a_fetch_failure_is_the_runs_error_code():
    data = pdf(1)
    up = upload(data, object_key=f"firms/{OTHER_FIRM}/docs/{DOC}")
    ex = GraphExecutor(gateway=None, sink=MemorySink(), tools=ToolRegistry(), clock=FakeClock())  # type: ignore[arg-type]
    run = RunIdentity(str(uuid.uuid4()), FIRM, "cc", "fetch_test@1", "document", DOC)
    out = await ex.run(TaskGraph("fetch_test@1", [fetch_node(up, Store({}))]), run, Budget())
    assert out.status is RunStatus.FAILED and out.error_code == "tenant_mismatch"
