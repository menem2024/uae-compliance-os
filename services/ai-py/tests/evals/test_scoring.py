"""field_accuracy is the binding spec section 5.6 definition: hand-computed fixtures."""

from ai.agents.extraction.schema import (
    ExtractedInvoice,
    InvoiceLine,
    Item,
    Price,
    TaxCategory,
    TaxSubtotal,
    flatten,
)
from ai.evals.core import CaseScore
from ai.evals.scoring import field_accuracy, micro_average, score_invoices


def _line(name: str = "Paper", net: str = "100.00") -> InvoiceLine:
    return InvoiceLine(item=Item(name=name), quantity="2", unit_code="H87", price=Price(net_price="50.00"),
                       net_amount=net, tax=TaxCategory(code="S", rate="5"))


def _invoice(number: str = "INV-1") -> ExtractedInvoice:
    # four non-empty header fields; one line (seven fields, all counted); no tax breakdown
    return ExtractedInvoice(invoice_number=number, issue_date="2026-01-02", currency="AED",
                            total_amount="105.00", lines=[_line()])


def _acc(truth: ExtractedInvoice, pred: ExtractedInvoice) -> tuple[float, dict]:
    tf, pf = flatten(truth), flatten(pred)
    acc, details = field_accuracy(tf, pf, dict.fromkeys([*tf, *pf]))
    return acc, dict(details)


def test_all_fields_matching_scores_one():
    acc, d = _acc(_invoice(), _invoice())
    assert acc == 1.0
    assert (d["counted"], d["correct"], d["wrong_paths"]) == (11, 11, ())


def test_one_wrong_header_field_and_one_extra_predicted_row():
    pred = _invoice("INV-2")  # wrong invoice_number
    pred.lines.append(InvoiceLine(item=Item(name="Extra"), quantity="1"))  # two non-empty fields
    acc, d = _acc(_invoice(), pred)
    # counted: 4 header + 7 truth-row fields + 2 non-empty fields of the extra row = 13; correct = 3 + 7
    assert (d["counted"], d["correct"]) == (13, 10)
    assert acc == 10 / 13
    assert set(d["wrong_paths"]) == {"invoice_number", "lines[1].item.name", "lines[1].quantity"}


def test_every_field_of_a_truth_row_counts_even_when_empty_and_missing_rows_are_wrong():
    truth = ExtractedInvoice(invoice_number="A", lines=[_line(), InvoiceLine()])  # second row all empty
    pred = ExtractedInvoice(invoice_number="A", lines=[_line()])  # second row missing
    acc, d = _acc(truth, pred)
    # 1 header + 7 + 7 counted; the empty truth row equals the missing predicted row, field by field
    assert (d["counted"], d["correct"], acc) == (15, 15, 1.0)
    pred2 = ExtractedInvoice(invoice_number="A", lines=[])  # first truth row missing: its 7 fields are wrong
    acc2, d2 = _acc(truth, pred2)
    assert (d2["counted"], d2["correct"]) == (15, 8)
    assert acc2 == 8 / 15


def test_tax_breakdown_rows_follow_the_same_rule():
    truth = ExtractedInvoice(tax_breakdown=[TaxSubtotal(taxable_amount="100", tax_amount="5",
                                                        category=TaxCategory(code="S", rate="5"))])
    pred = ExtractedInvoice(tax_breakdown=[
        TaxSubtotal(taxable_amount="100.00", tax_amount="5.00", category=TaxCategory(code="s", rate="5%")),
        TaxSubtotal(tax_amount="1"),  # extra row, one non-empty field
    ])
    acc, d = _acc(truth, pred)
    assert (d["counted"], d["correct"]) == (5, 4)  # decimals compare numerically, codes after normalisation
    assert acc == 4 / 5


def test_header_field_empty_on_both_sides_does_not_count_and_a_hallucinated_one_does():
    acc, d = _acc(ExtractedInvoice(invoice_number="A"), ExtractedInvoice(invoice_number="A", note="made up"))
    assert (d["counted"], d["correct"], acc) == (2, 1, 0.5)
    assert d["wrong_paths"] == ("note",)


def test_arabic_variant_folding_and_decimal_equality():
    truth = ExtractedInvoice(buyer={"name": "مؤسسة الإمارات"}, total_amount="1050.5")
    pred = ExtractedInvoice(buyer={"name": "مؤسسه الامارات"}, total_amount="1,050.50")
    assert _acc(truth, pred)[0] == 1.0


def test_nothing_to_count_is_a_perfect_score():
    assert field_accuracy({}, {}, []) [0] == 1.0


def test_score_invoices_sums_over_a_list_and_penalises_extra_and_missing_invoices():
    a, b = _invoice("A"), _invoice("B")
    acc, d = score_invoices([a, b], [a])  # the second invoice is missing: 11 fields counted, none correct
    assert (d["counted"], d["correct"], acc) == (22, 11, 0.5)
    acc, d = score_invoices([a], [a, b])  # an extra predicted invoice adds its non-empty fields as wrong
    assert (d["counted"], d["correct"]) == (11 + 11, 11)


def _score(case_id: str, metrics: dict[str, float]) -> CaseScore:
    return CaseScore(case_id=case_id, tags=frozenset(), metrics=metrics, details={})


def test_micro_average_weights_by_counted_fields_and_falls_back_to_the_mean():
    scores = [
        _score("a", {"field_accuracy": 1.0, "field_accuracy#counted": 90, "field_accuracy#correct": 90}),
        _score("b", {"field_accuracy": 0.0, "field_accuracy#counted": 10, "field_accuracy#correct": 0}),
    ]
    assert micro_average(scores, "field_accuracy") == 0.9  # not the 0.5 mean of the two ratios
    plain = [_score("a", {"kind_accuracy": 1.0}), _score("b", {"kind_accuracy": 0.0}), _score("c", {"x": 1.0})]
    assert micro_average(plain, "kind_accuracy") == 0.5
