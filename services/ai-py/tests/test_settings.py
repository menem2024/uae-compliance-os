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


def test_fake_results_default_to_review():
    assert Settings.from_env({}).fake_results == "review"
    assert Settings.from_env({"AI_FAKE_RESULTS": "accept"}).fake_results == "accept"
    with pytest.raises(ValueError):
        Settings.from_env({"AI_FAKE_RESULTS": "yes"})


def test_openai_compat_settings_and_the_provider_alias():
    d = Settings.from_env({})
    assert d.gateway == "fake" and d.openai_api_key == "" and d.openai_price_input_micro == 0
    s = Settings.from_env({"AI_PROVIDER": "openai_compat", "AI_GATEWAY": "anthropic",
                           "AI_OPENAI_BASE_URL": "https://g.example/v1/", "AI_OPENAI_API_KEY": "sekret",
                           "AI_OPENAI_MODEL_FAST": "f", "AI_OPENAI_MODEL_SMART": "m",
                           "AI_OPENAI_PRICE_OUTPUT_MICRO": "7"})
    assert (s.gateway, s.openai_base_url, s.openai_model_fast, s.openai_model_smart,
            s.openai_price_output_micro) == ("openai_compat", "https://g.example/v1/", "f", "m", 7)
    assert s.openai_api_key == "sekret" and "sekret" not in repr(s)
    assert (d.openai_max_rpm, d.openai_max_attempts, d.openai_max_retry_wait_s, d.openai_timeout_s) == (
        10, 5, 120, 240)
    tuned = Settings.from_env({"AI_OPENAI_MAX_RPM": "4", "AI_OPENAI_MAX_ATTEMPTS": "2",
                               "AI_OPENAI_MAX_RETRY_WAIT_S": "30", "AI_OPENAI_TIMEOUT_S": "300"})
    assert (tuned.openai_max_rpm, tuned.openai_max_attempts, tuned.openai_max_retry_wait_s,
            tuned.openai_timeout_s) == (4, 2, 30, 300)
    assert Settings.from_env({"AI_OPENAI_MAX_RPM": "0"}).openai_max_rpm == 0  # pacing off
    for bad in ({"AI_OPENAI_MAX_ATTEMPTS": "0"}, {"AI_OPENAI_TIMEOUT_S": "1"}, {"AI_OPENAI_MAX_RPM": "-1"}):
        with pytest.raises(ValueError):
            Settings.from_env(bad)
    assert Settings.from_env({"AI_PROVIDER": "", "AI_GATEWAY": "anthropic"}).gateway == "anthropic"
    with pytest.raises(ValueError, match="AI_PROVIDER"):
        Settings.from_env({"AI_PROVIDER": "openai"})
