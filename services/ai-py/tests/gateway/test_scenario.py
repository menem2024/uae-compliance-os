"""ScenarioGateway: AI_GATEWAY=fake's provider (compose default, chaos test worker)."""

import json

import pytest
from pydantic import BaseModel

from ai.agents.extraction import prompts as extraction_prompts
from ai.agents.extraction.schema import ExtractionOutput, flatten
from ai.agents.intake import agent as intake_agent
from ai.agents.intake.schema import IntakeResult
from ai.agents.verifier.core import decide
from ai.agents.verifier.critic import CRITIC_PROMPT_ID, ESCALATION_PROMPT_ID, CriticOutput
from ai.agents.verifier.invoice import invoice_findings, invoice_profile
from ai.evals.manifest import PROMPT_VERSIONS
from ai.gateway.errors import OutputInvalid, PermanentModelError
from ai.gateway.fake import DEFAULT_SCENARIO, ScenarioGateway
from ai.gateway.types import HAIKU, Message, ModelRequest, TextPart
from ai.settings import Settings

# Every Phase 1 prompt id and the structured output its agent asks for.
OUTPUT_MODELS: dict[str, type[BaseModel]] = {
    intake_agent.PROMPT_ID: IntakeResult,
    extraction_prompts.PROMPT_ID: ExtractionOutput,
    extraction_prompts.REVISE_PROMPT_ID: ExtractionOutput,
    CRITIC_PROMPT_ID: CriticOutput,
    ESCALATION_PROMPT_ID: CriticOutput,
}


class A(BaseModel):
    x: str = ""


def req(pid: str = "p.a", model: type[BaseModel] | None = A) -> ModelRequest:
    return ModelRequest(model=HAIKU, prompt_id=pid, prompt_version=1, system="s",
                        messages=(Message("user", (TextPart("t"),)),), output_model=model)


def write(tmp_path, script: object):
    path = tmp_path / "scenario.json"
    path.write_text(json.dumps(script), encoding="utf-8")
    return path


async def test_responses_are_consumed_in_order_and_the_last_repeats(tmp_path):
    path = write(tmp_path, {"p.a": [{"x": "1"}, '{"x": "2"}']})  # structured, then raw JSON text
    gw = ScenarioGateway.from_file(path)
    got = [(await gw.complete(req())).parsed for _ in range(4)]
    assert got == [A(x="1"), A(x="2"), A(x="2"), A(x="2")]


async def test_each_prompt_id_has_its_own_cursor_and_usage_is_free():
    gw = ScenarioGateway({"p.a": [{"x": "1"}, {"x": "2"}], "p.b": [{"x": "b"}]})
    assert (await gw.complete(req("p.b"))).parsed == A(x="b")
    first = await gw.complete(req("p.a"))
    assert first.parsed == A(x="1") and first.text == '{"x": "1"}'
    assert (first.usage.cost_micro_usd, first.usage.llm_calls) == (0, 1)
    assert len(first.cache_key) == 64


async def test_text_without_an_output_model_is_returned_verbatim():
    gw = ScenarioGateway({"p.a": ["plain words"]})
    resp = await gw.complete(req(model=None))
    assert resp.text == "plain words" and resp.parsed is None


async def test_unknown_prompt_and_schema_mismatch():
    gw = ScenarioGateway({"p.a": [{"x": 98765}]})  # x must be a string
    with pytest.raises(PermanentModelError):
        await gw.complete(req("p.zzz"))
    with pytest.raises(OutputInvalid) as ei:
        await gw.complete(req())
    assert "98765" not in str(ei.value)  # the message never echoes output


def test_a_malformed_scenario_fails_at_load(tmp_path):
    for bad in ({"p.a": []}, {"p.a": {"x": "1"}}, [{"x": "1"}], {"p.a": [1]}, {}):
        with pytest.raises(ValueError):
            ScenarioGateway.from_file(write(tmp_path, bad))
    with pytest.raises(ValueError):
        ScenarioGateway({"p.a": [{"x": "1"}]}, latency_ms=-1)


async def test_latency_is_slept_before_every_call():
    slept: list[float] = []

    async def fake_sleep(s: float) -> None:
        slept.append(s)

    gw = ScenarioGateway({"p.a": [{"x": "1"}]}, latency_ms=250, sleep=fake_sleep)
    await gw.complete(req())
    await gw.complete(req())
    assert slept == [0.25, 0.25]
    no_latency = ScenarioGateway({"p.a": [{"x": "1"}]}, sleep=fake_sleep)
    await no_latency.complete(req())
    assert slept == [0.25, 0.25]


def test_from_env_reads_the_scenario_path_and_latency(tmp_path, monkeypatch):
    path = write(tmp_path, {"p.a": [{"x": "env"}]})
    monkeypatch.setenv("AI_FAKE_SCENARIO", str(path))
    monkeypatch.setenv("AI_FAKE_LATENCY_MS", "40")
    gw = ScenarioGateway.from_env()
    assert gw.source == path and gw.latency_ms == 40
    explicit = ScenarioGateway.from_env(Settings(fake_scenario=str(path), fake_latency_ms=7))
    assert explicit.source == path and explicit.latency_ms == 7


async def test_the_packaged_default_answers_every_phase1_prompt_and_always_accepts(monkeypatch):
    monkeypatch.delenv("AI_FAKE_SCENARIO", raising=False)
    monkeypatch.delenv("AI_FAKE_LATENCY_MS", raising=False)
    gw = ScenarioGateway.from_env()
    assert gw.source == DEFAULT_SCENARIO and gw.latency_ms == 0
    assert set(OUTPUT_MODELS) == set(PROMPT_VERSIONS)  # the eval manifest lists every Phase 1 prompt id
    assert set(gw.prompt_ids) == set(OUTPUT_MODELS)
    parsed = {pid: (await gw.complete(req(pid, model))).parsed for pid, model in OUTPUT_MODELS.items()}
    for pid, model in OUTPUT_MODELS.items():
        assert isinstance(parsed[pid], model), pid

    intake = parsed[intake_agent.PROMPT_ID]
    assert isinstance(intake, IntakeResult)
    assert intake.kind == "invoice" and intake.invoice_count == 1 and intake.confidence >= 0.9

    for pid in (extraction_prompts.PROMPT_ID, extraction_prompts.REVISE_PROMPT_ID):
        out = parsed[pid]
        assert isinstance(out, ExtractionOutput)
        assert invoice_findings(out.invoice) == []  # no defects
        verdict = decide(invoice_profile(), out, [], revisions=0)
        assert verdict.verdict == "accept" and verdict.confidence >= 0.9

    values = flatten(parsed[extraction_prompts.PROMPT_ID].invoice)  # type: ignore[union-attr]
    for pid in (CRITIC_PROMPT_ID, ESCALATION_PROMPT_ID):
        critic = parsed[pid]
        assert isinstance(critic, CriticOutput)
        assert {r.path: r.document_value for r in critic.reviews} == values  # the critic agrees everywhere
        assert all(r.matches for r in critic.reviews)
