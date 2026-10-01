"""Extraction agent (spec section 5.2 `extraction.extract` and `extraction.revise#1`; contract section 1:
SONNET, effort low, max_tokens 8192). Structured output is ExtractionOutput; values are normalised here, so
every consumer (verifier, emit, evals) sees the stored form."""

from __future__ import annotations

import json
from typing import ClassVar, Literal

from pydantic import BaseModel, ConfigDict

from ai.agents.extraction import prompts
from ai.agents.extraction.normalize import normalize_invoice
from ai.agents.extraction.schema import ExtractionOutput, FieldConfidence, flatten
from ai.agents.source import SourceDocument
from ai.gateway.types import SONNET, Message, ModelId, ModelRequest, TextPart
from ai.runtime.agent import AgentContext, LLMAgent


class Feedback(BaseModel):
    """One reviewer note for a revision (from a critic finding that carries an expected value)."""

    model_config = ConfigDict(frozen=True, extra="forbid")

    path: str
    observed: str
    expected: str


class ExtractionInput(BaseModel):
    model_config = ConfigDict(frozen=True, extra="forbid")

    document: SourceDocument
    feedback: tuple[Feedback, ...] = ()  # empty on the first pass


def clean_output(out: ExtractionOutput) -> ExtractionOutput:
    """Normalised invoice; confidences clamped to [0, 1], rounded to 3 places, restricted to paths that exist
    in the invoice, one per path (last report wins), in field-set order."""
    inv = normalize_invoice(out.invoice)
    known = flatten(inv)
    reported: dict[str, float] = {}
    for fc in out.field_confidence:
        if fc.path in known:
            reported[fc.path] = round(min(1.0, max(0.0, fc.confidence)), 3)
    ordered = [FieldConfidence(path=p, confidence=reported[p]) for p in known if p in reported]
    return ExtractionOutput(invoice=inv, field_confidence=ordered, language=out.language)


def filled_fields(out: ExtractionOutput) -> int:
    return sum(1 for v in flatten(out.invoice).values() if v)


class ExtractionAgent(LLMAgent[ExtractionInput, ExtractionOutput]):
    name: ClassVar[str] = "extraction"
    version: ClassVar[int] = 1
    input_model: ClassVar[type[ExtractionInput]] = ExtractionInput
    output_model: ClassVar[type[ExtractionOutput]] = ExtractionOutput
    tools: ClassVar[tuple[str, ...]] = ()
    prompt_id = prompts.PROMPT_ID
    prompt_version = prompts.PROMPT_VERSION

    def __init__(self, model: ModelId = SONNET, effort: Literal["low", "medium", "high"] | None = "low",
                 max_tokens: int = 8192) -> None:
        self.model = model
        self.effort = effort
        self.max_tokens = max_tokens

    def build_request(self, inp: ExtractionInput) -> ModelRequest:
        if inp.feedback:
            notes = "\n".join(f"- {f.path}: {json.dumps(f.observed, ensure_ascii=False)} -> "
                              f"{json.dumps(f.expected, ensure_ascii=False)}" for f in inp.feedback)
            prompt_id, text = prompts.REVISE_PROMPT_ID, f"{prompts.REVISE_INSTRUCTION}\n{notes}"
        else:
            prompt_id, text = prompts.PROMPT_ID, prompts.INSTRUCTION
        return ModelRequest(model=self.model, prompt_id=prompt_id, prompt_version=self.prompt_version,
                            system=prompts.SYSTEM, messages=(Message("user", (*inp.document.parts(), TextPart(text))),),
                            output_model=ExtractionOutput, max_tokens=self.max_tokens, effort=self.effort)

    async def run(self, ctx: AgentContext, inp: ExtractionInput) -> ExtractionOutput:
        resp = await ctx.complete(self.build_request(inp))
        out = clean_output(ExtractionOutput.model_validate(resp.parsed))
        key = "extraction.revised" if inp.feedback else "extraction.extracted"
        ctx.emit(key, fields=str(filled_fields(out)), lines=str(len(out.invoice.lines)))
        return out
