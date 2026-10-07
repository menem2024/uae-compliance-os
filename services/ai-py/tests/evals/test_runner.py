"""Runner behaviour on a toy suite (mode resolution, eligibility, live guards) and on the real extraction suite
(the fake-mode score sits strictly between its threshold and 1.0)."""

from collections.abc import Callable
from dataclasses import replace
from pathlib import Path
from typing import ClassVar

import pytest
from pydantic import BaseModel

from ai.evals.cli import load_thresholds
from ai.evals.core import CaseScore, EvalCase, EvalEnv, run_node
from ai.evals.manifest import PROMPT_VERSIONS, load_manifest, matches_current, write_manifest
from ai.evals.runner import LiveNotConfirmed, LiveSpendCapExceeded, run_suite
from ai.evals.suites.extraction import ExtractionSuite
from ai.gateway.errors import SpendCapExceeded
from ai.gateway.fake import fake_usage, response_for
from ai.gateway.types import SONNET, Message, ModelRequest, ModelResponse, TextPart
from ai.runtime.context import NodeContext
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.types import StepKind
from ai.settings import Settings

THRESHOLDS = Path(__file__).resolve().parents[2] / "evals" / "thresholds.toml"


class Toy(BaseModel):
    answer: str


class ToySuite:
    name: ClassVar[str] = "toy"
    prompt_ids: ClassVar[tuple[str, ...]] = ("toy.say",)

    def cases(self, subset):
        return [EvalCase(f"c{i}", frozenset({"t:a"}), f"q{i}", f"q{i}") for i in range(4)]

    async def run_case(self, case, env: EvalEnv, hook: CollectingHook) -> str:
        async def fn(ctx: NodeContext) -> str:
            req = ModelRequest(model=SONNET, prompt_id="toy.say", prompt_version=1, system="s",
                               messages=(Message("user", (TextPart(case.input),)),), output_model=Toy)
            resp = await ctx.complete(req)
            assert isinstance(resp.parsed, Toy)
            return resp.parsed.answer

        return await run_node(env, hook, suite="toy", case_id=case.case_id, agent="toy", action="say",
                              kind=StepKind.LLM, fn=fn)

    def score(self, case, output: str) -> CaseScore:
        return CaseScore(case.case_id, case.tags, {"acc": 1.0 if output == case.truth else 0.0}, {})

    def fake_script(self, case) -> Callable[[ModelRequest], Toy]:
        return lambda _req: Toy(answer=case.truth)


class NoFakeSuite(ToySuite):
    name = "nofake"
    fake_script = None  # type: ignore[assignment]


@pytest.fixture(autouse=True)
def _toy_prompt(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setitem(PROMPT_VERSIONS, "toy.say", 1)


def _settings(tmp_path: Path) -> Settings:
    return Settings(recordings_dir=str(tmp_path / "rec"))


class CostlyGateway:
    """A stand-in provider whose every call costs USD 6; answers with the question text."""

    def __init__(self) -> None:
        self.calls = 0

    async def complete(self, req: ModelRequest) -> ModelResponse:
        self.calls += 1
        text = req.messages[0].parts[0].text  # type: ignore[union-attr]
        base = response_for(req, Toy(answer=text))
        return replace(base, usage=replace(fake_usage(req), cost_micro_usd=6_000_000))


LIVE_ENV = {"EVALS_LIVE_CONFIRM": "1", "ANTHROPIC_API_KEY": "test-key-not-used"}


async def test_fake_mode_runs_every_case_through_the_executor(tmp_path: Path):
    r = await run_suite(ToySuite(), mode="fake", subset="full", settings=_settings(tmp_path))
    assert (r.mode, r.cases, r.totals, r.errors) == ("fake", 4, {"acc": 1.0}, 0)
    assert r.per_tag == {"t:a": {"acc": 1.0}} and r.models == [SONNET]
    assert r.usage["llm_calls"] == 4 and not r.exit_criterion_eligible


async def test_a_suite_without_fake_script_is_skipped_in_fake_mode(tmp_path: Path):
    r = await run_suite(NoFakeSuite(), mode="fake", subset="full", settings=_settings(tmp_path))
    assert (r.status, r.cases, r.exit_criterion_eligible) == ("skipped", 0, False)


async def test_auto_is_fake_without_a_matching_manifest_and_replay_with_one(tmp_path: Path):
    s = _settings(tmp_path)
    assert (await run_suite(ToySuite(), mode="auto", subset="full", settings=s)).mode == "fake"
    write_manifest(Path(s.recordings_dir), "toy", {"toy.say": 99})  # stale
    assert (await run_suite(ToySuite(), mode="auto", subset="full", settings=s)).mode == "fake"


async def test_live_requires_confirmation_and_a_key(tmp_path: Path):
    gw = CostlyGateway()
    for env in ({}, {"EVALS_LIVE_CONFIRM": "1"}, {"ANTHROPIC_API_KEY": "k"}, {**LIVE_ENV, "EVALS_LIVE_CONFIRM": "0"}):
        with pytest.raises(LiveNotConfirmed):
            await run_suite(ToySuite(), mode="live", subset="full", settings=_settings(tmp_path), env=env,
                            provider=lambda: gw)
    assert gw.calls == 0


async def test_live_records_then_replay_is_eligible_only_on_full(tmp_path: Path):
    s = _settings(tmp_path)
    gw = CostlyGateway()
    live = await run_suite(ToySuite(), mode="live", subset="full", settings=s, env=LIVE_ENV, max_cost_usd=100.0,
                           provider=lambda: gw)
    assert (live.mode, live.exit_criterion_eligible, gw.calls) == ("live", True, 4)
    assert matches_current(load_manifest(Path(s.recordings_dir), "toy"), {"toy.say": 1})

    full = await run_suite(ToySuite(), mode="replay", subset="full", settings=s)
    assert (full.mode, full.errors, full.missing_recordings, full.exit_criterion_eligible) == ("replay", 0, 0, True)
    assert full.totals == {"acc": 1.0} and gw.calls == 4  # replay made no provider call
    pr = await run_suite(ToySuite(), mode="replay", subset="pr", settings=s)
    assert not pr.exit_criterion_eligible
    auto = await run_suite(ToySuite(), mode="auto", subset="full", settings=s)
    assert (auto.mode, auto.exit_criterion_eligible) == ("replay", True)


async def test_replay_with_a_missing_recording_is_counted_and_never_eligible(tmp_path: Path):
    s = _settings(tmp_path)
    write_manifest(Path(s.recordings_dir), "toy", {"toy.say": 1})  # manifest but no recordings
    r = await run_suite(ToySuite(), mode="replay", subset="full", settings=s)
    assert (r.missing_recordings, r.errors, r.exit_criterion_eligible) == (4, 4, False)
    assert r.totals == {}  # errored cases without an error_score carry no metrics


async def test_live_stops_at_the_spend_cap_and_writes_no_manifest(tmp_path: Path):
    s = Settings(recordings_dir=str(tmp_path / "rec"), max_concurrent_llm_calls=1)
    gw = CostlyGateway()

    class Many(ToySuite):
        def cases(self, subset):
            return [EvalCase(f"c{i}", frozenset(), f"q{i}", f"q{i}") for i in range(10)]

    with pytest.raises(LiveSpendCapExceeded):
        await run_suite(Many(), mode="live", subset="full", settings=s, env=LIVE_ENV, max_cost_usd=10.0,
                        provider=lambda: gw)
    assert gw.calls == 2  # 6 + 6 reaches the USD 10 cap; the third call is refused
    assert load_manifest(Path(s.recordings_dir), "toy") is None


def test_the_cap_error_code_is_the_one_the_runner_watches():
    assert SpendCapExceeded.code == "spend_cap_exceeded"


async def test_extraction_fake_score_is_strictly_between_threshold_and_one(datasets_root: Path, tmp_path: Path):
    suite = ExtractionSuite(datasets_root=datasets_root)
    r = await run_suite(suite, mode="fake", subset="full", settings=_settings(tmp_path))
    floor = next(t.value for t in load_thresholds(THRESHOLDS)
                 if (t.suite, t.metric, t.tags) == ("extraction", "field_accuracy", ()))
    assert r.cases == 20 and r.errors == 0
    assert floor < r.totals["field_accuracy"] < 1.0
    assert not r.exit_criterion_eligible
    pr = await run_suite(suite, mode="fake", subset="pr", settings=_settings(tmp_path))
    assert pr.cases == 10 and not pr.exit_criterion_eligible


async def test_live_key_env_follows_the_chosen_provider(tmp_path: Path):
    gw = CostlyGateway()
    for env in ({}, {"EVALS_LIVE_CONFIRM": "1", "ANTHROPIC_API_KEY": "k"}):  # the Anthropic key is not enough
        with pytest.raises(LiveNotConfirmed, match="AI_OPENAI_API_KEY"):
            await run_suite(ToySuite(), mode="live", subset="full", settings=_settings(tmp_path), env=env,
                            provider=lambda: gw, key_env="AI_OPENAI_API_KEY")
    ok = {"EVALS_LIVE_CONFIRM": "1", "AI_OPENAI_API_KEY": "k"}
    r = await run_suite(ToySuite(), mode="live", subset="full", settings=_settings(tmp_path), env=ok,
                        max_cost_usd=100.0, provider=lambda: gw, key_env="AI_OPENAI_API_KEY")
    assert (r.mode, gw.calls) == ("live", 4)
