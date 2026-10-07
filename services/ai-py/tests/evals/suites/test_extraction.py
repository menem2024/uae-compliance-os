from pathlib import Path

import pytest

from ai.agents.extraction.schema import ExtractionOutput
from ai.evals.core import CaseFailed, EvalEnv
from ai.evals.suites.extraction import ExtractionSuite
from ai.gateway.errors import PermanentModelError
from ai.gateway.fake import FakeGateway
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.types import RunStatus, StepStatus


def _first(suite: ExtractionSuite):
    return next(iter(suite.cases("full")))


async def test_a_faithful_extraction_scores_one_through_the_executor(datasets_root: Path):
    suite = ExtractionSuite(datasets_root=datasets_root)
    case = _first(suite)
    out = ExtractionOutput(invoice=case.truth, field_confidence=[], language="ar")
    gw, hook = FakeGateway(lambda _r: out), CollectingHook()
    got = await suite.run_case(case, EvalEnv(gw, "fake"), hook)
    s = suite.score(case, got)
    assert s.metrics["field_accuracy"] == 1.0 and s.metrics["field_accuracy#counted"] > 20
    assert [c.prompt_id for c in gw.calls] == ["extraction.invoice"]
    # it went through GraphExecutor: the hook saw the step records and the run outcome
    assert hook.outcome is not None and hook.outcome.status is RunStatus.SUCCEEDED
    run_steps = [r.status for r in hook.steps if r.node_id == "run"]
    assert run_steps[0] is StepStatus.STARTED and run_steps[-1] is StepStatus.SUCCEEDED
    assert len(hook.calls) == 1


async def test_an_errored_case_scores_zero_on_every_truth_field(datasets_root: Path):
    suite = ExtractionSuite(datasets_root=datasets_root)
    case = _first(suite)
    gw = FakeGateway(lambda _r: PermanentModelError("boom"))
    with pytest.raises(CaseFailed) as e:
        await suite.run_case(case, EvalEnv(gw, "fake"), CollectingHook())
    s = suite.error_score(case, e.value.code)
    assert (s.metrics["field_accuracy"], s.metrics["field_accuracy#correct"], s.error) == (
        0.0, 0.0, "model_permanent")
    assert s.metrics["field_accuracy#counted"] > 20


def test_subsets(datasets_root: Path):
    suite = ExtractionSuite(datasets_root=datasets_root)
    assert len(list(suite.cases("full"))) == 20 and len(list(suite.cases("pr"))) == 10
    assert all("subset:pr" in c.tags for c in suite.cases("pr"))


def test_the_committed_manifest_has_the_spec_slices():
    from tests.evals.conftest import REAL_ROOT
    cases = list(ExtractionSuite(datasets_root=REAL_ROOT).cases("full"))
    assert len(cases) == 200
    for tag in ("lang:ar", "lang:en", "format:pdf", "format:image"):
        assert sum(tag in c.tags for c in cases) == 100
    assert len(list(ExtractionSuite(datasets_root=REAL_ROOT).cases("pr"))) == 40


async def test_a_stale_binary_stops_the_run(datasets_root: Path, tmp_path: Path):
    from ai.evals.core import DatasetStale, DocRef
    ref = DocRef(tmp_path / "nope.pdf", "application/pdf", "0" * 64)
    with pytest.raises(DatasetStale):
        ref.read()
    (tmp_path / "x.pdf").write_bytes(b"abc")
    with pytest.raises(DatasetStale):
        DocRef(tmp_path / "x.pdf", "application/pdf", "0" * 64).read()
