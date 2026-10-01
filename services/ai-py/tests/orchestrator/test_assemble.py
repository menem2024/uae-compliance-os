"""document.extracted / document.failed assembly (contract 4.2)."""

import pytest

from ai.agents.extraction.schema import ExtractedInvoice
from ai.agents.orchestrator.assemble import (
    InvoiceOutcome,
    build_extracted,
    build_failed,
    invoice_needs_review,
    order_reasons,
)
from ai.gen.compliance.v1 import agents_pb2

ACCEPT = agents_pb2.VerifierVerdict(verdict=agents_pb2.VERDICT_ACCEPT, confidence=0.95)
ESCALATE = agents_pb2.VerifierVerdict(verdict=agents_pb2.VERDICT_ESCALATE, confidence=0.4)
IDS = {"document_id": "d1", "firm_id": "f1", "client_company_id": "c1", "run_id": "r1"}


def _o(i: int, conf: float, verdict: agents_pb2.VerifierVerdict | None) -> InvoiceOutcome:
    return InvoiceOutcome(i, f"rows {i + 2}-{i + 2}", ExtractedInvoice(invoice_number=f"INV-{i}", total_amount="1.00"),
                          conf, {"total_amount": 0.99, "invoice_number": 0.9123}, verdict)


def test_clean_document():
    msg = build_extracted(**IDS, document_kind="invoice", direction="issued", extraction_method="csv", language="",
                          invoices=[_o(1, 0.95, ACCEPT), _o(0, 0.97, ACCEPT)])
    assert not msg.needs_review and list(msg.review_reasons) == []
    assert [e.source_ordinal for e in msg.invoices] == [0, 1]
    first = msg.invoices[0]
    assert first.invoice.invoice_number == "INV-0" and first.invoice.total_amount == "1.00"
    assert [(f.path, f.confidence) for f in first.fields] == [("invoice_number", 0.912), ("total_amount", 0.99)]
    assert first.verdict.verdict == agents_pb2.VERDICT_ACCEPT


def test_review_reasons_follow_the_invoices_and_are_ordered():
    msg = build_extracted(**IDS, document_kind="invoice", direction="unknown", extraction_method="llm", language="ar",
                          invoices=[_o(0, 0.4, ESCALATE)], review_reasons=["budget_exceeded"])
    assert msg.needs_review
    assert list(msg.review_reasons) == ["low_confidence", "verifier_escalated", "budget_exceeded"]
    assert invoice_needs_review(_o(0, 0.9, None)) and invoice_needs_review(_o(0, 0.8, ACCEPT))
    assert not invoice_needs_review(_o(0, 0.85, ACCEPT))


def test_document_without_invoices_needs_review_only_with_a_reason():
    msg = build_extracted(**IDS, document_kind="contract", direction="unknown", extraction_method="llm",
                          language="en", invoices=[], review_reasons=["not_invoice"])
    assert msg.needs_review and list(msg.review_reasons) == ["not_invoice"] and len(msg.invoices) == 0


def test_value_sets_are_enforced():
    with pytest.raises(ValueError, match="value set"):
        build_extracted(**IDS, document_kind="receipt", direction="issued", extraction_method="llm", language="en",
                        invoices=[])
    with pytest.raises(ValueError, match="unknown review reasons"):
        order_reasons(["slow"])
    with pytest.raises(ValueError, match="unknown failure reason"):
        build_failed(document_id="d", firm_id="f", run_id="r", reason_code="oops")


def test_failed_detail_is_redacted_and_bounded():
    msg = build_failed(document_id="d", firm_id="f", run_id="r", reason_code="internal",
                       detail="boom for a@b.com " + "x" * 500)
    assert "a@b.com" not in msg.detail and len(msg.detail) == 200 and msg.reason_code == "internal"
