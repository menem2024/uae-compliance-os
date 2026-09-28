from ai.agents.stub_extraction import extract
from ai.gen.compliance.v1 import events_pb2, invoice_pb2


def test_passes_invoice_through_with_full_confidence():
    inv = invoice_pb2.Invoice(invoice_number="INV-1", seller_trn="123", total_amount="1050.00")
    ev = events_pb2.InvoiceSubmitted(invoice_id="i-1", firm_id="f-1", invoice=inv)
    out = extract(ev)
    assert isinstance(out, events_pb2.InvoiceExtracted)
    assert out.invoice_id == "i-1"
    assert out.firm_id == "f-1"
    assert out.invoice == inv
    assert out.confidence == 1.0
