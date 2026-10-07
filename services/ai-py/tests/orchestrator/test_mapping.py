"""Descriptor-guarded invoice mapping (contract 4.2, gate G1)."""

import pytest
from google.protobuf import descriptor_pb2, descriptor_pool, message_factory, text_format

from ai.agents.extraction.schema import (
    ExtractedInvoice,
    InvoiceLine,
    Item,
    PostalAddress,
    SellerParty,
    TaxCategory,
    TaxSubtotal,
    Totals,
)
from ai.agents.orchestrator.mapping import HAS_V2, map_invoice
from ai.gen.compliance.v1 import invoice_pb2

INV = ExtractedInvoice(
    invoice_number="INV-1", issue_date="2026-03-15", currency="AED", seller_trn="100123456789003",
    buyer_trn="100987654321003", total_amount="105.00", vat_amount="5.00", note="",
    seller=SellerParty(name="Oasis Trading LLC", postal_address=PostalAddress(country_subdivision="DXB")),
    totals=Totals(payable_amount="105.00"),
    tax_breakdown=[TaxSubtotal(taxable_amount="100.00", tax_amount="5.00", category=TaxCategory(code="S", rate="5"))],
    lines=[InvoiceLine(item=Item(name="Rice"), quantity="2", net_amount="100.00", tax=TaxCategory(code="S", rate="5"))],
)

TEST_FILE = """
name: "maptest.proto" package: "maptest" syntax: "proto3"
message_type { name: "Address" field { name: "country_subdivision" number: 1 type: TYPE_STRING label: LABEL_OPTIONAL } }
message_type { name: "Party"
  field { name: "name" number: 1 type: TYPE_STRING label: LABEL_OPTIONAL }
  field { name: "postal_address" number: 2 type: TYPE_MESSAGE label: LABEL_OPTIONAL type_name: ".maptest.Address" } }
message_type { name: "Item" field { name: "name" number: 1 type: TYPE_STRING label: LABEL_OPTIONAL } }
message_type { name: "Line"
  field { name: "item" number: 1 type: TYPE_MESSAGE label: LABEL_OPTIONAL type_name: ".maptest.Item" }
  field { name: "net_amount" number: 2 type: TYPE_STRING label: LABEL_OPTIONAL } }
message_type { name: "Invoice"
  field { name: "invoice_number" number: 1 type: TYPE_STRING label: LABEL_OPTIONAL }
  field { name: "seller" number: 2 type: TYPE_MESSAGE label: LABEL_OPTIONAL type_name: ".maptest.Party" }
  field { name: "lines" number: 3 type: TYPE_MESSAGE label: LABEL_REPEATED type_name: ".maptest.Line" }
  field { name: "currency" number: 4 type: TYPE_INT32 label: LABEL_OPTIONAL } }
"""


def _test_invoice_class():
    fdp = text_format.Parse(TEST_FILE, descriptor_pb2.FileDescriptorProto())
    pool = descriptor_pool.DescriptorPool()
    pool.Add(fdp)
    return message_factory.GetMessageClass(pool.FindMessageTypeByName("maptest.Invoice"))


def test_phase0_proto_gets_the_seven_flat_fields():
    m = map_invoice(INV)
    assert isinstance(m.message, invoice_pb2.Invoice)
    if HAS_V2:
        pytest.skip("invoice.proto v2 is generated; covered by the v2 test")
    assert m.message == invoice_pb2.Invoice(
        invoice_number="INV-1", issue_date="2026-03-15", seller_trn="100123456789003", buyer_trn="100987654321003",
        currency="AED", total_amount="105.00", vat_amount="5.00")
    assert set(m.dropped) == {"seller", "totals", "tax_breakdown", "lines"}


def test_generic_walk_copies_nested_and_repeated_fields_by_name_and_type():
    cls = _test_invoice_class()
    m = map_invoice(INV, cls)
    assert m.message.invoice_number == "INV-1"
    assert m.message.seller.name == "Oasis Trading LLC"
    assert m.message.seller.postal_address.country_subdivision == "DXB"
    assert [(ln.item.name, ln.net_amount) for ln in m.message.lines] == [("Rice", "100.00")]
    # wrong type (currency is int32 here), unknown fields and unknown nested fields are reported, never set
    assert set(m.dropped) == {"issue_date", "currency", "seller_trn", "buyer_trn", "totals", "vat_amount",
                              "total_amount", "tax_breakdown", "lines[0].quantity", "lines[0].tax"}


def test_empty_values_create_no_presence():
    m = map_invoice(ExtractedInvoice(invoice_number="X"), _test_invoice_class())
    assert not m.message.HasField("seller") and len(m.message.lines) == 0 and m.dropped == ()


@pytest.mark.skipif(not HAS_V2, reason="gate G1: invoice.proto v2 not generated yet (Task 31 fails while skipped)")
def test_v2_proto_holds_the_whole_phase1_field_set():
    m = map_invoice(INV)
    assert m.dropped == ()
    msg = m.message
    assert msg.seller.name == "Oasis Trading LLC"  # type: ignore[attr-defined]
    assert msg.seller.postal_address.country_subdivision == "DXB"  # type: ignore[attr-defined]
    assert msg.totals.payable_amount == "105.00"  # type: ignore[attr-defined]
    assert msg.tax_breakdown[0].category.rate == "5"  # type: ignore[attr-defined]
    assert msg.lines[0].item.name == "Rice" and msg.lines[0].tax.code == "S"  # type: ignore[attr-defined]
