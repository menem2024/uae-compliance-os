"""The `fix` eval suite in fake mode: the harness plumbing, the dataset and the thresholds. Nothing here measures
the model: `fake_script` answers with the truth."""

import json
from pathlib import Path

import pytest

from ai.agents.fix import paths
from ai.agents.fix.evals import FixEvalOutput, FixSuite, load_cases
from ai.agents.fix.guards import llm_targets
from ai.agents.fix.types import LLMChange, LLMFixOutput
from ai.evals.cli import failures, load_suites, load_thresholds
from ai.evals.core import EvalEnv
from ai.evals.manifest import PROMPT_VERSIONS
from ai.evals.runner import run_suite
from ai.gateway.fake import FakeGateway
from ai.runtime.evalhooks import CollectingHook
from ai.settings import Settings

ROOT = Path(__file__).resolve().parents[3] / "evals"
CASES = ROOT / "datasets" / "fix" / "cases.jsonl"


def suite() -> FixSuite:
    return FixSuite(datasets_root=ROOT / "datasets")


def test_fix_eval_dataset_has_40_cases_and_a_10_case_pr_subset():
    cases = list(suite().cases("full"))
    pr = list(suite().cases("pr"))
    assert len(cases) == 40 and len(pr) == 10
    assert len({c.case_id for c in cases}) == 40
    assert {t for c in cases for t in c.tags if t.startswith("defect:")} == {
        f"defect:{k}" for k in ("emirate_seller", "emirate_buyer", "tax_category", "due_date", "period_start",
                                "tax_point", "exemption_reason", "unit_code", "country_code", "time_format")}
    assert len({next(t for t in c.tags if t.startswith("defect:")) for c in pr}) == 10  # one of each kind


def test_fix_eval_every_case_has_a_target_issue_on_its_truth_path_and_no_forbidden_truth():
    for c in suite().cases("full"):
        targets = llm_targets(c.input.issues)
        assert targets, c.case_id
        assert {t.path for t in targets} == {p for p, _ in c.truth}, c.case_id
        for p, v in c.truth:
            assert paths.is_valid_path(p) and not paths.is_forbidden(p), (c.case_id, p)
            assert paths.get_value(c.input.invoice, p) != v, c.case_id  # the defect is really there


def test_fix_eval_dataset_is_plain_data_with_real_rule_ids():
    for line in CASES.read_text(encoding="utf-8").splitlines():
        d = json.loads(line)
        assert set(d) == {"base", "case_id", "invoice", "issues", "tags", "truth"}
        assert all(i["rule_id"] and i["message"] for i in d["issues"])
    assert len(load_cases(CASES)) == 40


async def test_fix_eval_fake_mode_scores_the_truth_answer_perfectly_and_passes_the_thresholds():
    report = await run_suite(suite(), mode="fake", subset="full", settings=Settings())
    assert (report.mode, report.cases, report.errors, report.status) == ("fake", 40, 0, "ok")
    assert report.totals == {"harmless": 1.0, "precision": 1.0, "recall": 1.0}
    assert report.exit_criterion_eligible is False  # fake output never counts as a measurement
    assert failures(report, load_thresholds(ROOT / "thresholds.toml")) == []
    pr = await run_suite(suite(), mode="fake", subset="pr", settings=Settings())
    assert pr.cases == 10 and pr.totals["recall"] == 1.0


async def test_fix_eval_the_request_is_the_production_request():
    s = suite()
    case = next(iter(s.cases("pr")))
    gw = FakeGateway(s.fake_script(case))
    out = await s.run_case(case, EvalEnv(gw, "fake"), CollectingHook())
    (req,) = gw.calls
    assert req.prompt_id == "fix.invoice_fields" and req.output_model is LLMFixOutput and req.tools == ()
    assert out.proposed == case.truth


async def _score(case, changes: list[tuple[str, str]], s: FixSuite):
    answer = LLMFixOutput(changes=[LLMChange(path=p, new_value=v, rule_ids=[], rationale="") for p, v in changes],
                          confidence=0.9)
    gw = FakeGateway(lambda _r: answer)
    return s.score(case, await s.run_case(case, EvalEnv(gw, "fake"), CollectingHook()))


async def test_fix_eval_the_scorer_notices_wrong_missing_and_harmful_answers():
    s = suite()
    case = next(iter(s.cases("pr")))
    (path, value), = case.truth
    right = await _score(case, [(path, value)], s)
    assert (right.metrics["precision"], right.metrics["recall"], right.metrics["harmless"]) == (1.0, 1.0, 1.0)
    wrong = await _score(case, [(path, value + "x")], s)  # on target, wrong value
    assert (wrong.metrics["precision"], wrong.metrics["recall"], wrong.metrics["harmless"]) == (0.0, 0.0, 1.0)
    none = await _score(case, [], s)
    assert (none.metrics["precision#counted"], none.metrics["recall"], none.metrics["harmless"]) == (0.0, 0.0, 1.0)
    extra = await _score(case, [(path, value), ("invoice_number", "X")], s)  # right plus a forbidden path
    assert (extra.metrics["precision"], extra.metrics["recall"], extra.metrics["harmless"]) == (0.5, 1.0, 0.0)
    off = await _score(case, [(path, value), ("currency", "USD")], s)  # right plus an off-target path
    assert off.metrics["harmless"] == 0.0


def test_fix_eval_an_errored_case_misses_the_truth_and_harms_nothing():
    s = suite()
    case = next(iter(s.cases("pr")))
    sc = s.error_score(case, "model_refusal")
    assert sc.error == "model_refusal" and sc.metrics["recall"] == 0.0 and sc.metrics["harmless"] == 1.0


def test_fix_eval_the_scorer_reads_raw_output_before_the_guards():
    s = suite()
    case = next(iter(s.cases("pr")))
    out = FixEvalOutput(proposed=(("currency", "USD"),), target_paths=tuple(p for p, _ in case.truth))
    assert s.score(case, out).metrics["harmless"] == 0.0


def test_fix_eval_the_suite_is_an_entry_point_with_a_registered_prompt_and_thresholds():
    assert isinstance(load_suites()["fix"], FixSuite)
    assert FixSuite.prompt_ids == ("fix.invoice_fields",) and PROMPT_VERSIONS["fix.invoice_fields"] == 1
    got = {(t.metric, t.op, t.value) for t in load_thresholds(ROOT / "thresholds.toml") if t.suite == "fix"}
    assert got == {("precision", ">=", 0.90), ("recall", ">=", 0.80), ("harmless", ">=", 1.0)}


@pytest.mark.parametrize("subset", ["pr", "full"])
def test_fix_eval_fake_script_is_deterministic(subset):
    s = suite()
    a = [(c.case_id, s.fake_script(c)(None).model_dump_json()) for c in s.cases(subset)]  # type: ignore[arg-type]
    b = [(c.case_id, s.fake_script(c)(None).model_dump_json()) for c in s.cases(subset)]  # type: ignore[arg-type]
    assert a == b
