"""ModelGateway error taxonomy (agent-runtime contract v0.2, section 2)."""

from typing import ClassVar

from ai.gateway.types import Usage


class GatewayError(Exception):
    code: ClassVar[str] = "gateway_error"
    # Provider usage of a call that was billed but still failed (refusal, max_tokens, context overflow, schema
    # mismatch); None when no billable call completed. SpendLimitedGateway charges it before re-raising.
    usage: Usage | None = None


class TransientModelError(GatewayError):  # 429, 5xx, overloaded, connection, timeout
    code = "model_transient"

    def __init__(self, message: str, retry_after_s: float | None = None) -> None:
        super().__init__(message)
        self.retry_after_s = retry_after_s


class PermanentModelError(GatewayError):  # 400/401/403/404, bad request shape
    code = "model_permanent"


class ModelRefusal(PermanentModelError):  # stop_reason == "refusal"
    code = "model_refusal"

    def __init__(self, category: str | None) -> None:
        super().__init__(f"model refused (category={category or 'unknown'})")
        self.category = category


class OutputInvalid(GatewayError):  # schema parse failed or stop_reason == "max_tokens"
    code = "output_invalid"  # the message never contains the raw output (PII)


class RecordingMissing(GatewayError):  # ReplayGateway has no recording for the key
    code = "recording_missing"

    def __init__(self, prompt_id: str, cache_key: str) -> None:
        super().__init__(f"no recording for {prompt_id} key {cache_key}")
        self.prompt_id = prompt_id
        self.cache_key = cache_key


class SpendCapExceeded(GatewayError):  # per-Firm daily cap (SpendLimiter)
    code = "spend_cap_exceeded"


class SpendLimiterUnavailable(TransientModelError):
    """The spend-cap store (Valkey) is unreachable, so a live call cannot be admitted: the cap fails closed.

    Transient on purpose (code `model_transient`): the node retries, then the message is nak'ed and
    redelivered, so ingestion resumes by itself once the store is back. No provider call was made.
    """


class QuotaExhausted(TransientModelError):
    """A provider's daily (or other long-window) quota is used up, so waiting seconds cannot help.

    A TransientModelError (the node still retries cheaply and the message is redelivered later, so ingestion
    resumes once the quota resets) with its own code, so eval harnesses and operators can tell "provider is
    out of quota for hours" from "provider blinked". `retry_after_s` is the provider's own reset hint when it
    gave one. Gateways that pace and retry (ResilientGateway) never retry it.
    """

    code = "quota_exhausted"
