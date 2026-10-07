"""`fetch`, the first node of document_ingestion@1 (spec section 5.2).

Reads the object through an injected FetchFn (Task 21 supplies the S3 one), refuses an object key outside the
Firm's prefix before reading anything, re-checks sha256 (a presigned PUT stays usable after `complete`, spec
section 5.1 step 4), sniffs the real format from magic bytes (the declared content_type is ignored) and counts
PDF pages. Every failure is a FetchError: the node is critical, so its code becomes RunOutcome.error_code and
then DocumentFailed.reason_code.
"""

from __future__ import annotations

import hashlib
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Literal

import pypdfium2 as pdfium

from ai.gen.compliance.v1 import documents_pb2
from ai.runtime.context import NodeContext
from ai.runtime.graph import Node
from ai.runtime.types import StepKind

type FetchFn = Callable[[str], Awaitable[bytes]]
type FetchCode = Literal["object_missing", "sha256_mismatch", "tenant_mismatch", "unsupported_format"]

AGENT = "orchestrator"
PDF = "application/pdf"
PNG = "image/png"
JPEG = "image/jpeg"
WEBP = "image/webp"
CSV = "text/csv"
XLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
IMAGE_TYPES: frozenset[str] = frozenset({PNG, JPEG, WEBP})
FORMAT_NAMES: dict[str, str] = {PDF: "pdf", PNG: "png", JPEG: "jpeg", WEBP: "webp", CSV: "csv", XLSX: "xlsx"}
_SNIFF_BYTES = 512  # what api-go's complete step sniffs (internal/documents/documents.go SniffMatches)


class FetchError(Exception):
    def __init__(self, code: FetchCode) -> None:
        super().__init__(code)
        self.code: FetchCode = code


@dataclass(frozen=True, slots=True)
class Fetched:
    data: bytes
    media_type: str  # one of FORMAT_NAMES, from the magic bytes
    page_count: int  # PDF pages; 1 for an image; 0 for a spreadsheet


def sniff(data: bytes) -> str | None:
    """The media type the bytes really are, by the same rules as api-go's SniffMatches; None if unknown."""
    head = data[:_SNIFF_BYTES]
    if head.startswith(b"%PDF-"):
        return PDF
    if head.startswith(b"\x89PNG\r\n\x1a\n"):
        return PNG
    if head.startswith(b"\xff\xd8\xff"):
        return JPEG
    if len(head) >= 12 and head[:4] == b"RIFF" and head[8:12] == b"WEBP":
        return WEBP
    if head.startswith(b"PK\x03\x04"):
        return XLSX  # any zip; the importer rejects one that is not a workbook (not_a_workbook)
    if head and b"\x00" not in head:
        return CSV
    return None


def pdf_page_count(data: bytes) -> int:
    try:
        doc = pdfium.PdfDocument(data)
    except pdfium.PdfiumError as exc:  # corrupt, truncated or password-protected
        raise FetchError("unsupported_format") from exc
    try:
        return len(doc)
    finally:
        doc.close()


def in_tenant(object_key: str, firm_id: str) -> bool:
    return bool(firm_id) and object_key.startswith(f"firms/{firm_id}/") and ".." not in object_key.split("/")


def fetch_node(upload: documents_pb2.DocumentUploaded, fetch_fn: FetchFn) -> Node:
    async def fn(ctx: NodeContext) -> Fetched:
        if not in_tenant(upload.object_key, upload.firm_id):
            raise FetchError("tenant_mismatch")
        try:
            data = await fetch_fn(upload.object_key)
        except FileNotFoundError as exc:
            raise FetchError("object_missing") from exc
        if hashlib.sha256(data).hexdigest() != upload.sha256:
            raise FetchError("sha256_mismatch")
        media_type = sniff(data)
        if media_type is None:
            raise FetchError("unsupported_format")
        if media_type == PDF:
            pages = pdf_page_count(data)
            if pages < 1:
                raise FetchError("unsupported_format")
        else:
            pages = 1 if media_type in IMAGE_TYPES else 0
        ctx.emit("orchestrator.fetched", format=FORMAT_NAMES[media_type], pages=str(pages))
        return Fetched(data, media_type, pages)

    return Node("fetch", AGENT, "fetch", StepKind.DETERMINISTIC, fn, critical=True)
