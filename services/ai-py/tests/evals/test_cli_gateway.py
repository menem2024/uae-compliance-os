"""`evals run --gateway`: the default stays the Anthropic path; another provider is built through the factory."""

from pathlib import Path

import pytest

from ai.evals import cli
from ai.gateway import openai_compat_gw
from ai.gateway.fake import FakeGateway
from ai.settings import Settings


def test_default_gateway_changes_nothing():
    assert cli.live_provider_args("anthropic", Settings()) == {}


def test_openai_compat_is_built_through_the_factory(monkeypatch: pytest.MonkeyPatch):
    seen: dict[str, object] = {}

    def fake_make(**kw: object) -> FakeGateway:
        seen.update(kw)
        return FakeGateway(lambda _req: None)  # type: ignore[arg-type,return-value]

    monkeypatch.setattr(openai_compat_gw, "make_gateway", fake_make)
    s = Settings(openai_base_url="http://x/v1", openai_api_key="k", openai_model_fast="f", openai_model_smart="m")
    extra = cli.live_provider_args("openai_compat", s)
    assert extra["key_env"] == "AI_OPENAI_API_KEY"
    extra["provider"]()
    assert (seen["base_url"], seen["model_fast"], seen["model_smart"]) == ("http://x/v1", "f", "m")


def test_run_parser_accepts_gateway_and_rejects_unknown(tmp_path: Path):
    with pytest.raises(SystemExit):
        cli.main(["run", "extraction", "--gateway", "nope"])
