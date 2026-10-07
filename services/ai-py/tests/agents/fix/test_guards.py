from fixhelpers import BASE, EMIRATE, UNIT, issue, llm_answer, make_invoice

from ai.agents.fix import guards, paths
from ai.agents.fix.types import LLMChange, LLMFixOutput
from ai.gen.compliance.v1 import validator_pb2


def test_targets_are_fixable_errors_without_a_suggestion_on_allowed_paths_once_per_rule_and_path():
    warn = issue("w", "currency", severity=validator_pb2.SEVERITY_WARNING)
    issues = [issue("a", EMIRATE), issue("a", EMIRATE), issue("b", EMIRATE), issue("c", "issue_date"),
              issue("d", "nope"), issue("e", "currency", suggested="AED"), issue("f", "currency", fixable=False), warn]
    assert [(i.rule_id, i.path) for i in guards.llm_targets(issues)] == [("a", EMIRATE), ("b", EMIRATE)]


def test_the_model_input_carries_the_invoice_and_the_current_values_of_the_target_paths():
    inv = paths.apply_changes(make_invoice(), [(UNIT, "h87")])
    targets = guards.llm_targets([issue("ibr-cl-23", UNIT)])
    inp = guards.llm_input(inv, targets)
    assert [(i.path, i.current_value) for i in inp.issues] == [(UNIT, "h87")]
    assert '"unit_code":"h87"' in inp.invoice_json


def _kept(out: LLMFixOutput, inv, targets):
    kept, dropped = guards.guard_changes(out, inv, targets)
    return [(c.path, c.new_value) for c in kept], dropped


def test_guards_keep_a_good_change_with_the_target_rule_ids():
    inv = make_invoice(seller__postal_address__country_subdivision="")
    targets = guards.llm_targets([issue("ibr-143-ae", EMIRATE)])
    kept, dropped = guards.guard_changes(llm_answer((EMIRATE, "SHJ")), inv, targets)
    assert dropped == 0
    (c,) = kept
    assert (c.path, c.old_value, c.new_value, c.rule_ids, c.source) == (EMIRATE, "", "SHJ", ("ibr-143-ae",), "llm")


def test_guards_drop_off_target_forbidden_malformed_duplicate_and_no_op_changes():
    inv = make_invoice(seller__postal_address__country_subdivision="")
    targets = guards.llm_targets([issue("ibr-143-ae", EMIRATE), issue("ibr-cl-23", UNIT)])
    out = llm_answer(
        ("currency", "AED"),  # off target
        ("invoice_number", "X"),  # AgentForbidden (and off target)
        ("seller.nope", "x"),  # not in the Invoice descriptor
        (EMIRATE, "SHJ"),  # kept
        (EMIRATE, "DXB"),  # repeats a path
        (UNIT, "H87"),  # no-op: already H87
    )
    assert _kept(out, inv, targets) == ([(EMIRATE, "SHJ")], 5)


def test_a_forbidden_path_that_is_also_a_target_is_still_dropped():
    inv = make_invoice()
    forged = [issue("r", "invoice_number")]  # llm_targets would already refuse it; guard_changes refuses too
    out = llm_answer(("invoice_number", "INV-9"))
    assert _kept(out, inv, forged) == ([], 1)


def test_guards_drop_unappliable_oversized_and_non_bool_values():
    inv = make_invoice()
    forged = [issue("r", "lines[7].unit_code"), issue("r", "allowances_charges[0].is_charge"), issue("r", UNIT)]
    out = llm_answer(("lines[7].unit_code", "H87"), ("allowances_charges[0].is_charge", "maybe"),
                     (UNIT, "x" * 501))
    assert _kept(out, inv, forged) == ([], 3)


def test_at_most_twenty_changes_survive():
    inv = make_invoice()
    n = 30
    targets = [issue("r", f"lines[{i}].unit_code") for i in range(n)]
    for i in range(n):
        paths.set_value(inv, f"lines[{i}].unit_code", "bad")
    kept, dropped = guards.guard_changes(
        LLMFixOutput(changes=[LLMChange(path=f"lines[{i}].unit_code", new_value="H87", rule_ids=[], rationale="")
                              for i in range(n)], confidence=0.9), inv, targets)
    assert len(kept) == 20 and dropped == 10


def test_rationale_is_trimmed():
    inv = make_invoice(seller__postal_address__country_subdivision="")
    out = LLMFixOutput(changes=[LLMChange(path=EMIRATE, new_value="SHJ", rule_ids=[], rationale=" " + "r" * 900)],
                       confidence=0.9)
    (c,), _ = guards.guard_changes(out, inv, guards.llm_targets([issue("x", EMIRATE)]))
    assert len(c.rationale) == guards.MAX_RATIONALE_CHARS


def test_base_unit_path_is_a_valid_target():
    assert paths.is_valid_path(BASE)
