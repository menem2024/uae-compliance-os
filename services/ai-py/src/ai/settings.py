"""ai-py settings, read once from the environment (plan Global Constraints, Python)."""

import os
from collections.abc import Mapping
from dataclasses import dataclass, field
from typing import Literal, cast

from ai.gateway.types import HAIKU, MODEL_IDS, OPUS, SONNET, ModelId

type GatewayMode = Literal["anthropic", "openai_compat", "replay", "fake"]
GATEWAY_MODES = ("anthropic", "openai_compat", "replay", "fake")
type CacheMode = Literal["valkey", "memory", "none"]
# What document.extracted says when the fake gateway produced it: `review` (the default) publishes it as
# needs_review / extraction_failed with every invoice escalated, so canned output never lands as a clean
# accepted invoice; `accept` keeps the scenario's verdicts, for hermetic harnesses only (the AC-E2 chaos test).
type FakeResults = Literal["review", "accept"]


def _choice[T: str](env: Mapping[str, str], key: str, default: T, allowed: tuple[str, ...]) -> T:
    value = env.get(key, "") or default
    if value not in allowed:
        raise ValueError(f"{key}={value!r}: expected one of {', '.join(allowed)}")
    return cast("T", value)


def _int(env: Mapping[str, str], key: str, default: int, *, minimum: int = 0) -> int:
    raw = env.get(key, "")
    try:
        value = int(raw) if raw else default
    except ValueError:
        raise ValueError(f"{key}={raw!r}: expected an integer") from None
    if value < minimum:
        raise ValueError(f"{key}={value}: must be >= {minimum}")
    return value


@dataclass(frozen=True, slots=True)
class Settings:
    gateway: GatewayMode = "fake"
    fake_scenario: str = ""
    fake_latency_ms: int = 0
    fake_results: FakeResults = "review"
    recordings_dir: str = "evals/recordings"
    model_intake: ModelId = HAIKU
    model_extraction: ModelId = SONNET
    model_critic: ModelId = SONNET
    model_escalation: ModelId = OPUS
    # openai_compat provider (AI_GATEWAY=openai_compat, alias AI_PROVIDER): any OpenAI-compatible
    # /chat/completions endpoint. Our abstract tiers map to two model ids: HAIKU -> fast, SONNET and OPUS ->
    # smart. Defaults are OpenRouter free vision models (they come and go: override them). For Google
    # Gemini use AI_OPENAI_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai/ and
    # AI_OPENAI_MODEL_FAST/_SMART=gemini-2.5-flash. The key is read from the environment only and is
    # hidden from repr. Prices are micro-USD per million tokens; 0 (free tier) means cost_micro_usd 0.
    openai_base_url: str = "https://openrouter.ai/api/v1"
    openai_api_key: str = field(default="", repr=False)
    openai_model_fast: str = "google/gemma-3-27b-it:free"
    openai_model_smart: str = "meta-llama/llama-4-maverick:free"
    openai_price_input_micro: int = 0
    openai_price_output_micro: int = 0
    cache: CacheMode = "memory"
    valkey_url: str = "redis://localhost:6379/0"
    daily_spend_cap_micro_usd: int = 3_000_000
    max_concurrent_llm_calls: int = 4
    docs_max_ack_pending: int = 8
    ack_wait_s: int = 60
    max_pdf_pages: int = 10
    health_port: int = 8081
    s3_endpoint: str = "localhost:9000"
    s3_access_key: str = ""
    s3_secret_key: str = ""
    s3_use_ssl: bool = False
    s3_bucket: str = "documents"
    s3_region: str = "us-east-1"
    langfuse_otlp_endpoint: str = ""
    langfuse_public_key: str = ""
    langfuse_secret_key: str = ""
    nats_url: str = ""

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> Settings:
        e = os.environ if env is None else env
        d = cls()

        def model(key: str, default: ModelId) -> ModelId:
            return _choice(e, key, default, MODEL_IDS)

        return cls(
            # AI_PROVIDER is an alias of AI_GATEWAY and wins when both are set.
            gateway=_choice(e, "AI_PROVIDER" if e.get("AI_PROVIDER") else "AI_GATEWAY", d.gateway,
                            GATEWAY_MODES),
            fake_scenario=e.get("AI_FAKE_SCENARIO", ""),
            fake_latency_ms=_int(e, "AI_FAKE_LATENCY_MS", d.fake_latency_ms),
            fake_results=_choice(e, "AI_FAKE_RESULTS", d.fake_results, ("review", "accept")),
            recordings_dir=e.get("AI_RECORDINGS_DIR", "") or d.recordings_dir,
            model_intake=model("AI_MODEL_INTAKE", d.model_intake),
            model_extraction=model("AI_MODEL_EXTRACTION", d.model_extraction),
            model_critic=model("AI_MODEL_CRITIC", d.model_critic),
            model_escalation=model("AI_MODEL_ESCALATION", d.model_escalation),
            openai_base_url=e.get("AI_OPENAI_BASE_URL", "") or d.openai_base_url,
            openai_api_key=e.get("AI_OPENAI_API_KEY", ""),
            openai_model_fast=e.get("AI_OPENAI_MODEL_FAST", "") or d.openai_model_fast,
            openai_model_smart=e.get("AI_OPENAI_MODEL_SMART", "") or d.openai_model_smart,
            openai_price_input_micro=_int(e, "AI_OPENAI_PRICE_INPUT_MICRO", 0),
            openai_price_output_micro=_int(e, "AI_OPENAI_PRICE_OUTPUT_MICRO", 0),
            cache=_choice(e, "AI_CACHE", d.cache, ("valkey", "memory", "none")),
            valkey_url=e.get("VALKEY_URL", "") or d.valkey_url,
            daily_spend_cap_micro_usd=_int(e, "AI_DAILY_SPEND_CAP_MICRO_USD", d.daily_spend_cap_micro_usd),
            max_concurrent_llm_calls=_int(e, "AI_MAX_CONCURRENT_LLM_CALLS", d.max_concurrent_llm_calls,
                                          minimum=1),
            docs_max_ack_pending=_int(e, "AI_DOCS_MAX_ACK_PENDING", d.docs_max_ack_pending, minimum=1),
            ack_wait_s=_int(e, "AI_ACK_WAIT_S", d.ack_wait_s, minimum=10),
            max_pdf_pages=_int(e, "AI_MAX_PDF_PAGES", d.max_pdf_pages, minimum=1),
            health_port=_int(e, "AI_HEALTH_PORT", d.health_port, minimum=1),
            s3_endpoint=e.get("S3_ENDPOINT", "") or d.s3_endpoint,
            s3_access_key=e.get("S3_ACCESS_KEY", ""),
            s3_secret_key=e.get("S3_SECRET_KEY", ""),
            s3_use_ssl=e.get("S3_USE_SSL", "") == "true",
            s3_bucket=e.get("S3_BUCKET", "") or d.s3_bucket,
            s3_region=e.get("S3_REGION", "") or d.s3_region,
            langfuse_otlp_endpoint=e.get("LANGFUSE_OTLP_ENDPOINT", ""),
            langfuse_public_key=e.get("LANGFUSE_PUBLIC_KEY", ""),
            langfuse_secret_key=e.get("LANGFUSE_SECRET_KEY", ""),
            nats_url=e.get("NATS_URL", ""),
        )
