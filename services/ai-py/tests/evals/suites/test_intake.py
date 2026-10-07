from pathlib import Path

from ai.evals.core import EvalEnv
from ai.evals.suites.intake import IntakeSuite, fake_wrong_kind
from ai.gateway.fake import FakeGateway
from ai.runtime.evalhooks import CollectingHook

REAL_ROOT = Path(__file__).resolve().parents[3] / "evals" / "datasets"


async def test_fake_script_classifies_and_scores_kind(datasets_root: Path):
    suite = IntakeSuite(datasets_root=datasets_root)
    cases = list(suite.cases("full"))
    assert len(cases) == 24  # 20 extraction documents and 4 letters
    assert {c.truth["kind"] for c in cases} == {"invoice", "contract"}
    for case in (cases[0], cases[-1]):
        gw = FakeGateway(suite.fake_script(case))
        out = await suite.run_case(case, EvalEnv(gw, "fake"), CollectingHook())
        assert [c.prompt_id for c in gw.calls] == ["intake.classify"]
        assert suite.score(case, out).metrics["kind_accuracy"] == (0.0 if fake_wrong_kind(case.case_id) else 1.0)


def test_committed_cases_are_the_200_plus_20_and_the_fake_rate_is_inside_the_threshold():
    suite = IntakeSuite(datasets_root=REAL_ROOT)
    full = [c.case_id for c in suite.cases("full")]
    pr = [c.case_id for c in suite.cases("pr")]
    assert (len(full), len(pr)) == (220, 44)
    for ids in (full, pr):
        accuracy = 1 - sum(fake_wrong_kind(i) for i in ids) / len(ids)
        assert 0.95 < accuracy < 1.0
