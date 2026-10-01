"""ModelGateway error taxonomy (agent-runtime contract v0.2, section 2)."""

from typing import ClassVar


class GatewayError(Exception):
    code: ClassVar[str] = "gateway_error"


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
