from pathlib import Path

from ai.agents.verifier.invoice import INVOICE_CHECKS, invoice_critic_when
from ai.evals.core import EvalEnv
from ai.evals.suites.verifier import VerifierSuite, fake_false_flag, fake_miss
from ai.gateway.fake import FakeGateway
from ai.runtime.evalhooks import CollectingHook

REAL_ROOT = Path(__file__).resolve().parents[3] / "evals" / "datasets"


async def test_the_critic_runs_on_every_case_regardless_of_critic_when(datasets_root: Path):
    suite = VerifierSuite(datasets_root=datasets_root)
    cases = list(suite.cases("full"))
    assert len(cases) == 20 and sum(c.truth.corrupted for c in cases) == 10
    ungated = 0
    for case in cases:
        out = case.input.output
        checks = [f for chk in INVOICE_CHECKS for f in chk(out)]
        if not invoice_critic_when(out, checks):
            ungated += 1  # production would skip the critic here
        gw = FakeGateway(suite.fake_script(case))
        verdict = await suite.run_case(case, EvalEnv(gw, "fake"), CollectingHook())
        assert [c.prompt_id for c in gw.calls] == ["verifier.critic"]
        score = suite.score(case, verdict)
        assert set(score.metrics) == {"catch_rate" if case.truth.corrupted else "false_flag_rate"}
    assert ungated > 0  # the forcing is actually exercised


async def test_a_faithful_critic_catches_corruptions_and_accepts_clean_cases(datasets_root: Path):
    suite = VerifierSuite(datasets_root=datasets_root)
    for case in suite.cases("full"):
        if fake_miss(case.case_id) or fake_false_flag(case.case_id):
            continue  # the fake critic errs on these on purpose
        gw = FakeGateway(suite.fake_script(case))
        verdict = await suite.run_case(case, EvalEnv(gw, "fake"), CollectingHook())
        flagged = suite.score(case, verdict).metrics
        assert flagged == ({"catch_rate": 1.0} if case.truth.corrupted else {"false_flag_rate": 0.0}), case.case_id


def test_committed_cases_and_fake_rates_sit_inside_the_thresholds():
    suite = VerifierSuite(datasets_root=REAL_ROOT)
    for subset, n in (("full", 200), ("pr", 40)):
        cases = list(suite.cases(subset))
        assert len(cases) == n
        bad = [c for c in cases if c.truth.corrupted]
        clean = [c for c in cases if not c.truth.corrupted]
        assert len(bad) == len(clean) == n // 2
        catch = 1 - sum(fake_miss(c.case_id) for c in bad) / len(bad)
        flag = sum(fake_false_flag(c.case_id) for c in clean) / len(clean)
        assert 0.80 <= catch < 1.0 and 0 < flag <= 0.10
