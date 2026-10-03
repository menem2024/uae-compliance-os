"""Model prices in micro-USD per million tokens (contract section 1). Re-verify before the pilot."""

from dataclasses import dataclass
from typing import Final

from ai.gateway.types import HAIKU, OPUS, SONNET

PRICING_AS_OF: Final = "2026-06-24"


@dataclass(frozen=True, slots=True)
class Price:
    input: int
    output: int
    cache_read: int
    cache_write: int  # 5-minute ephemeral cache write


PRICING: Final[dict[str, Price]] = {
    OPUS: Price(input=4_000_000, output=20_000_000, cache_read=200_000, cache_write=5_000_000),
    SONNET: Price(input=2_000_000, output=10_000_000, cache_read=200_000, cache_write=2_500_000),
    HAIKU: Price(input=1_000_000, output=5_000_000, cache_read=100_000, cache_write=1_250_000),
}


def cost_micro_usd(model: str, *, input_tokens: int, output_tokens: int, cache_read_input_tokens: int = 0,
                   cache_creation_input_tokens: int = 0, pricing: dict[str, Price] = PRICING) -> int:
    """ceil(sum(tokens x price) / 1_000_000), in integers only. KeyError for an unpriced model."""
    p = pricing[model]
    total = (input_tokens * p.input + output_tokens * p.output
             + cache_read_input_tokens * p.cache_read + cache_creation_input_tokens * p.cache_write)
    return -(-total // 1_000_000)


def reservation_micro_usd(model: str, max_tokens: int, pricing: dict[str, Price] = PRICING) -> int:
    """What SpendLimitedGateway reserves against the daily cap before a call: `max_tokens` at the model's
    output price, the upper bound of the output side. The input side is unknown until the provider counts it
    and is charged when the call settles. An unpriced model is reserved at the dearest output price."""
    p = pricing.get(model) or max(pricing.values(), key=lambda x: x.output)
    return -(-max(0, max_tokens) * p.output // 1_000_000)
