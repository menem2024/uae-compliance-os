from pathlib import Path

from ai.evals.core import EvalEnv
from ai.evals.scoring import micro_average
from ai.evals.suites.importer import ImporterSuite
from ai.gateway.fake import FakeGateway
from ai.runtime.evalhooks import CollectingHook


async def test_importer_never_calls_the_gateway_and_scores_one(datasets_root: Path):
    suite = ImporterSuite(datasets_root=datasets_root)
    cases = list(suite.cases("full"))
    assert len(cases) == 40 and len(list(suite.cases("pr"))) == 10
    scores = []
    for case in cases:
        gw = FakeGateway(suite.fake_script(case))  # declared, and raises if ever invoked
        out = await suite.run_case(case, EvalEnv(gw, "fake"), CollectingHook())
        assert gw.calls == []
        scores.append(suite.score(case, out))
    assert [s.case_id for s in scores if s.metrics["field_accuracy"] != 1.0] == []
    assert micro_average(scores, "field_accuracy") == 1.0


async def test_the_fake_script_would_raise_if_called(datasets_root: Path):
    import pytest

    from ai.gateway.types import ModelRequest
    suite = ImporterSuite(datasets_root=datasets_root)
    script = suite.fake_script(next(iter(suite.cases("full"))))
    with pytest.raises(AssertionError):
        script(ModelRequest(model="claude-sonnet-5", prompt_id="x.y", prompt_version=1, system="", messages=()))
