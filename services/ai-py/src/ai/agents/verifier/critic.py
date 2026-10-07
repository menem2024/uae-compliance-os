"""The LLM critic for extracted invoices (contract section 6 `Critic`; spec section 5.2 `verify.critic`,
`verify2.escalate`).

It re-reads the document for a list of paths and reports disagreements with `source="critic"`. Whether a value
agrees is decided here, deterministically, by comparing the normalised values (`values_equal`), not by the
model's own `matches` flag. The request carries only the document and values extracted from it (rule 9).
"""

from __future__ import annotations

import json
from collections.abc import Sequence
from typing import Literal

from pydantic import BaseModel, ConfigDict

from ai.agents.extraction.compare import values_equal
from ai.agents.extraction.normalize import normalize_value
from ai.agents.extraction.schema import ExtractionOutput, flatten
from ai.agents.verifier.core import Finding
from ai.agents.verifier.invoice import CRITICAL_PATHS, low_confidence_paths
from ai.gateway.types import Message, ModelId, ModelRequest, Part, TextPart
from ai.runtime.agent import AgentContext

CRITIC_PROMPT_ID = "verifier.critic"
ESCALATION_PROMPT_ID = "verifier.escalate"
PROMPT_VERSION = 1
MAX_SAMPLED_PATHS = 24

SYSTEM = """You are the verification reviewer of a UAE e-invoicing platform. Another model extracted values \
from the attached invoice. Your job is to re-read the document and report, for every listed field path, the \
value exactly as the document prints it.

Rules:
- Read only the attached document. Never guess, never compute, never correct the document.
- Copy digits as Western digits (0-9). Write amounts without currency symbols or thousands separators, keeping \
the decimals as printed. Write dates as printed.
- If the document does not print a value for a path, return an empty string for it.
- Set matches to true only when the extracted value is the value the document prints.
- Return exactly one review per listed path, using the path text unchanged.
- Paths use this grammar: seller.name, lines[0].net_amount (0-based line index), tax_breakdown[1].category.rate."""


class _Strict(BaseModel):
    model_config = ConfigDict(extra="forbid")


class CriticReview(_Strict):
    path: str
    document_value: str
    matches: bool


class CriticOutput(_Strict):
    reviews: list[CriticReview]


class InvoiceCritic:
    """Implements `Critic[ExtractionOutput]`. One instance per role (critic on SONNET, escalation on OPUS)."""

    def __init__(self, *, model: ModelId, prompt_id: str = CRITIC_PROMPT_ID, prompt_version: int = PROMPT_VERSION,
                 effort: Literal["low", "medium", "high"] | None = "medium", max_tokens: int = 2048) -> None:
        self.model: ModelId = model
        self.prompt_id = prompt_id
        self.prompt_version = prompt_version
        self.effort = effort
        self.max_tokens = max_tokens

    @staticmethod
    def review_paths(output: ExtractionOutput, focus: Sequence[Finding]) -> list[str]:
        """Focus paths first (never capped: critic confirmation relies on them), then the low-confidence and
        critical header paths up to MAX_SAMPLED_PATHS in total. Deterministic order (cache keys)."""
        known = flatten(output.invoice)
        paths: list[str] = []
        for f in focus:
            for p in (f.path, *f.related):
                if p in known and p not in paths:
                    paths.append(p)
        for p in (*low_confidence_paths(output), *CRITICAL_PATHS):
            if len(paths) >= MAX_SAMPLED_PATHS:
                break
            if p in known and p not in paths:
                paths.append(p)
        return paths

    def build_request(self, output: ExtractionOutput, evidence: Sequence[Part], paths: Sequence[str]) -> ModelRequest:
        values = flatten(output.invoice)
        listing = "\n".join(f"- {p}: {json.dumps(values[p], ensure_ascii=False)}" for p in paths)
        text = ("Re-read the document and review these extracted values (path: extracted value):\n"
                f"{listing}\n\nReturn one review per path.")
        return ModelRequest(model=self.model, prompt_id=self.prompt_id, prompt_version=self.prompt_version,
                            system=SYSTEM, messages=(Message("user", (*evidence, TextPart(text))),),
                            output_model=CriticOutput, max_tokens=self.max_tokens, effort=self.effort)

    @staticmethod
    def findings(output: ExtractionOutput, paths: Sequence[str], parsed: CriticOutput) -> list[Finding]:
        values = flatten(output.invoice)
        got: dict[str, CriticReview] = {}
        for r in parsed.reviews:
            got.setdefault(r.path, r)
        out: list[Finding] = []
        for p in paths:
            extracted = values[p]
            r = got.get(p)
            if r is None:
                out.append(Finding(p, "critic.unreviewed", "warn", observed=extracted, source="critic"))
                continue
            printed = normalize_value(p, r.document_value)
            if values_equal(p, extracted, printed):
                continue
            if not printed:
                out.append(Finding(p, "critic.not_found", "warn", observed=extracted, source="critic"))
            else:
                out.append(Finding(p, "critic.mismatch", "block", observed=extracted, expected=printed,
                                   source="critic"))
        return out

    async def review(self, ctx: AgentContext, output: ExtractionOutput, evidence: Sequence[Part],
                     focus: Sequence[Finding]) -> list[Finding]:
        paths = self.review_paths(output, focus)
        if not paths:
            return []
        resp = await ctx.complete(self.build_request(output, evidence, paths))
        return self.findings(output, paths, CriticOutput.model_validate(resp.parsed))
