import pytest

from ai.gateway.types import HAIKU, OPUS, SONNET
from ai.settings import Settings


def test_defaults_match_plan():
    s = Settings.from_env({})
    assert (s.gateway, s.cache, s.fake_scenario, s.recordings_dir) == ("fake", "memory", "", "evals/recordings")
    assert (s.model_intake, s.model_extraction, s.model_critic, s.model_escalation) == (HAIKU, SONNET, SONNET, OPUS)
    assert s.daily_spend_cap_micro_usd == 3_000_000 and s.max_concurrent_llm_calls == 4
    assert (s.docs_max_ack_pending, s.ack_wait_s, s.max_pdf_pages, s.health_port) == (8, 60, 10, 8081)
    assert (s.s3_bucket, s.s3_region, s.s3_use_ssl) == ("documents", "us-east-1", False)


def test_overrides_and_validation():
    s = Settings.from_env({"AI_GATEWAY": "replay", "AI_MODEL_EXTRACTION": HAIKU, "AI_CACHE": "none",
                           "AI_DAILY_SPEND_CAP_MICRO_USD": "5", "S3_USE_SSL": "true"})
    assert (s.gateway, s.model_extraction, s.cache, s.daily_spend_cap_micro_usd, s.s3_use_ssl) == (
        "replay", HAIKU, "none", 5, True)
    for bad in ({"AI_GATEWAY": "openai"}, {"AI_MODEL_INTAKE": "gpt-5"}, {"AI_MAX_CONCURRENT_LLM_CALLS": "0"},
                {"AI_ACK_WAIT_S": "abc"}):
        with pytest.raises(ValueError):
            Settings.from_env(bad)
