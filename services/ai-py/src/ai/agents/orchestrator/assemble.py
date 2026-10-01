"""Builds the document.extracted / document.failed payloads (contract section 4.2). Pure; no I/O.

Per-invoice status rule (api-go Task 17 applies it; stated here so both sides cite one place): an invoice is
`needs_review` when its verdict is not ACCEPT or its confidence is below LOW_CONFIDENCE, else `extracted`.
The Document is `needs_review` when `review_reasons` is non-empty.
"""

from __future__ import annotations

from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass, field

from ai.agents.extraction.schema import ExtractedInvoice
from ai.agents.orchestrator.mapping import map_invoice
from ai.gateway.redact import redact
from ai.gen.compliance.v1 import agents_pb2, documents_pb2

LOW_CONFIDENCE = 0.85
REVIEW_REASONS: tuple[str, ...] = (
    "low_confidence", "verifier_escalated", "multi_invoice_pdf", "too_many_pages", "import_mapping_incomplete",
    "budget_exceeded", "model_refusal", "spend_cap_exceeded", "not_invoice", "extraction_failed",
)
FAILURE_REASONS: frozenset[str] = frozenset({
    "unsupported_format", "object_missing", "sha256_mismatch", "tenant_mismatch", "max_deliver_exceeded", "internal",
})
KINDS = frozenset({"invoice", "credit_note", "contract", "other"})
DIRECTIONS = frozenset({"issued", "received", "unknown"})
METHODS = frozenset({"llm", "xlsx", "csv"})
LANGUAGES = frozenset({"ar", "en", "mixed", ""})  # "" = not determined (for example a spreadsheet)
_DETAIL_MAX = 200


@dataclass(frozen=True, slots=True)
class InvoiceOutcome:
    source_ordinal: int
    source_ref: str
    invoice: ExtractedInvoice
    confidence: float
    fields: Mapping[str, float] = field(default_factory=dict)  # path -> confidence
    verdict: agents_pb2.VerifierVerdict | None = None


def invoice_needs_review(o: InvoiceOutcome) -> bool:
    verdict = o.verdict.verdict if o.verdict is not None else agents_pb2.VERDICT_UNSPECIFIED
    return verdict != agents_pb2.VERDICT_ACCEPT or o.confidence < LOW_CONFIDENCE


def order_reasons(reasons: Iterable[str]) -> list[str]:
    """Dedupes and sorts review reasons in REVIEW_REASONS order; an unknown reason is a bug (ValueError)."""
    got = set(reasons)
    unknown = got - set(REVIEW_REASONS)
    if unknown:
        raise ValueError(f"unknown review reasons {sorted(unknown)}")
    return [r for r in REVIEW_REASONS if r in got]


def build_extracted(*, document_id: str, firm_id: str, client_company_id: str, run_id: str, document_kind: str,
                    direction: str, extraction_method: str, language: str, invoices: Sequence[InvoiceOutcome],
                    review_reasons: Iterable[str] = ()) -> documents_pb2.DocumentExtracted:
    if document_kind not in KINDS or direction not in DIRECTIONS or extraction_method not in METHODS \
            or language not in LANGUAGES:
        raise ValueError("document_kind, direction, extraction_method or language outside its value set")
    reasons = set(review_reasons)
    if any(invoice_needs_review(o) for o in invoices):
        reasons.add("verifier_escalated" if any(
            o.verdict is not None and o.verdict.verdict == agents_pb2.VERDICT_ESCALATE for o in invoices)
            else "low_confidence")
    if any(o.confidence < LOW_CONFIDENCE for o in invoices):
        reasons.add("low_confidence")
    msg = documents_pb2.DocumentExtracted(
        document_id=document_id, firm_id=firm_id, client_company_id=client_company_id, run_id=run_id,
        document_kind=document_kind, direction=direction, extraction_method=extraction_method, language=language)
    for o in sorted(invoices, key=lambda x: x.source_ordinal):
        ext = msg.invoices.add(source_ordinal=o.source_ordinal, source_ref=o.source_ref,
                               confidence=round(o.confidence, 3))
        ext.invoice.CopyFrom(map_invoice(o.invoice).message)
        for path in sorted(o.fields):
            ext.fields.add(path=path, confidence=round(o.fields[path], 3))
        if o.verdict is not None:
            ext.verdict.CopyFrom(o.verdict)
    ordered = order_reasons(reasons)
    msg.review_reasons.extend(ordered)
    msg.needs_review = bool(ordered)
    return msg


def build_failed(*, document_id: str, firm_id: str, run_id: str, reason_code: str,
                 detail: str = "") -> documents_pb2.DocumentFailed:
    if reason_code not in FAILURE_REASONS:
        raise ValueError(f"unknown failure reason {reason_code!r}")
    return documents_pb2.DocumentFailed(document_id=document_id, firm_id=firm_id, run_id=run_id,
                                        reason_code=reason_code, detail=redact(detail)[:_DETAIL_MAX])
