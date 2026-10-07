import pytest

from ai.agents.fix import paths
from ai.gen.compliance.v1 import invoice_pb2

FORBIDDEN = [
    "invoice_number", "uuid", "issue_date", "seller_trn", "buyer_trn", "seller.tax_registration_identifier",
    "seller.electronic_address.id", "buyer.electronic_address.id", "seller.legal_registration.id",
    "buyer.legal_registration.id", "principal_id", "beneficiary_id", "tax_representative.vat_identifier",
]


def test_the_thirteen_forbidden_paths_match_api_go():
    assert sorted(paths.AGENT_FORBIDDEN) == sorted(FORBIDDEN)
    assert all(paths.is_forbidden(p) for p in FORBIDDEN)
    assert not paths.is_forbidden("payment_due_date")


@pytest.mark.parametrize("path", ["issue_date", "seller.postal_address.city", "lines[0].tax.code",
                                  "lines[12].price.base_quantity_unit_code", "allowances_charges[1].is_charge"])
def test_valid_paths(path: str):
    assert paths.is_valid_path(path)


@pytest.mark.parametrize("path", ["", "invoice.issue_date", "lines", "lines[0]", "lines[x].tax.code", "Seller.name",
                                  "seller", "nope", "seller.nope", "issue_date[0]", "lines.tax.code", "seller..name"])
def test_invalid_paths(path: str):
    assert not paths.is_valid_path(path)


def test_get_returns_empty_for_absent_parents_and_does_not_create_them():
    inv = invoice_pb2.Invoice()
    assert paths.get_value(inv, "seller.postal_address.city") == ""
    assert paths.get_value(inv, "lines[3].tax.code") == ""
    assert not inv.HasField("seller") and len(inv.lines) == 0


def test_set_creates_parents_and_appends_at_the_list_length_only():
    inv = invoice_pb2.Invoice()
    paths.set_value(inv, "seller.postal_address.city", "Dubai")
    paths.set_value(inv, "lines[0].unit_code", "H87")
    paths.set_value(inv, "lines[1].unit_code", "KGM")
    assert paths.get_value(inv, "seller.postal_address.city") == "Dubai"
    assert [ln.unit_code for ln in inv.lines] == ["H87", "KGM"]
    with pytest.raises(ValueError, match="beyond"):
        paths.set_value(inv, "lines[5].unit_code", "X")


def test_bool_fields_take_true_or_false_only():
    inv = invoice_pb2.Invoice()
    paths.set_value(inv, "allowances_charges[0].is_charge", "true")
    assert paths.get_value(inv, "allowances_charges[0].is_charge") == "true"
    assert paths.is_bool_path("allowances_charges[0].is_charge")
    with pytest.raises(ValueError, match="bool"):
        paths.set_value(inv, "allowances_charges[0].is_charge", "yes")


def test_apply_changes_works_on_a_copy():
    inv = invoice_pb2.Invoice(currency="aed")
    out = paths.apply_changes(inv, [("currency", "AED")])
    assert (inv.currency, out.currency) == ("aed", "AED")
