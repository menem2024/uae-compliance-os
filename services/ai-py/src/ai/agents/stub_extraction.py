"""Phase 0 stand-in for the Extraction agent: deterministic pass-through, no LLM."""

from ai.gen.compliance.v1 import events_pb2


def extract(ev: events_pb2.InvoiceSubmitted) -> events_pb2.InvoiceExtracted:
    out = events_pb2.InvoiceExtracted(invoice_id=ev.invoice_id, firm_id=ev.firm_id, confidence=1.0)
    out.invoice.CopyFrom(ev.invoice)
    return out
