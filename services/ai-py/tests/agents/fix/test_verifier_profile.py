import ast
import inspect

from ai.agents.fix import verifier_profile
from ai.agents.fix.types import CandidateChange, FixCandidate
from ai.agents.fix.verifier_profile import fix_profile
from ai.agents.verifier.core import decide


def change(path: str = "currency", source: str = "deterministic") -> CandidateChange:
    return CandidateChange(path=path, old_value="a", new_value="b", rule_ids=["r"], source=source, rationale="")


def cand(**over) -> FixCandidate:
    base = {"changes": [change()], "errors_before": 2, "errors_after": 1, "before_rule_ids": ["r1", "r2"],
            "after_rule_ids": ["r2"], "target_rule_ids": ["r1"], "target_paths": ["currency"],
            "target_keys": ["r1@currency"], "after_keys": ["r2@x"], "confidence": 1.0}
    base.update(over)
    return FixCandidate(**base)


def codes(c: FixCandidate) -> list[str]:
    return [f.code for chk in fix_profile().checks for f in chk(c) if f.severity == "block"]


def verdict(c: FixCandidate) -> str:
    p = fix_profile()
    return decide(p, c, [f for chk in p.checks for f in chk(c)], revisions=0).verdict


def test_a_good_candidate_passes_every_check_and_is_accepted():
    assert codes(cand()) == [] and verdict(cand()) == "accept"


def test_no_new_errors_blocks_an_error_rule_id_the_original_did_not_have():
    c = cand(after_rule_ids=["r2", "r9"])
    assert codes(c) == ["fix.no_new_errors"] and verdict(c) == "escalate"


def test_improves_blocks_when_the_error_count_does_not_drop():
    assert "fix.improves" in codes(cand(errors_after=2))
    assert "fix.improves" in codes(cand(errors_after=3))


def test_resolves_target_blocks_when_every_target_issue_is_still_there():
    c = cand(after_keys=["r1@currency", "r2@x"])
    assert "fix.resolves_target" in codes(c)
    assert "fix.resolves_target" not in codes(cand(target_keys=["r1@currency", "r2@x"], after_keys=["r2@x"]))


def test_paths_targeted_blocks_only_llm_changes_off_the_target_paths():
    assert "fix.paths_targeted" in codes(cand(changes=[change("seller.name", "llm")]))
    assert "fix.paths_targeted" not in codes(cand(changes=[change("totals.payable_amount", "deterministic")]))
    assert "fix.paths_targeted" not in codes(cand(changes=[change("currency", "llm")]))


def test_forbidden_and_malformed_paths_block_for_any_source():
    assert "fix.forbidden_paths" in codes(cand(changes=[change("invoice_number")]))
    assert "fix.forbidden_paths" in codes(cand(changes=[change("not.a.path")]))


def test_producer_confidence_is_the_llms_only_when_an_llm_change_is_present():
    assert verifier_profile.producer_confidence(cand(confidence=0.4)) == 1.0
    assert verifier_profile.producer_confidence(cand(changes=[change("currency", "llm")], confidence=0.4)) == 0.4
    low = cand(changes=[change("currency", "llm")], confidence=0.8)
    assert verdict(low) == "escalate"  # below accept_threshold 0.85: a human is not asked


def test_the_profile_has_no_critic():
    assert fix_profile().critic is None and fix_profile().output_type is FixCandidate


def test_checks_are_pure_the_module_does_no_io():
    """AR section 6: a Check is pure. The profile module imports no network, file, process or runtime I/O."""
    tree = ast.parse(inspect.getsource(verifier_profile))
    mods = {n.module or "" for n in ast.walk(tree) if isinstance(n, ast.ImportFrom)}
    mods |= {a.name for n in ast.walk(tree) if isinstance(n, ast.Import) for a in n.names}
    banned = ("grpc", "socket", "os", "pathlib", "subprocess", "asyncio", "httpx", "ai.runtime", "ai.gateway",
              "ai.agents.fix.validator_client", "ai.agents.fix.nodes")
    assert [m for m in mods if m == "os" or m.startswith(banned)] == []
    names = {n.id for n in ast.walk(tree) if isinstance(n, ast.Name)}
    assert not names & {"open", "print", "await"} and not any(isinstance(n, ast.Await) for n in ast.walk(tree))
    assert not any(isinstance(n, ast.AsyncFunctionDef) for n in ast.walk(tree))
