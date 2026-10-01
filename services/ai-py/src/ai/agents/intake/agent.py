"""Intake agent: what is this document? (spec section 5.2 `intake.classify`; contract section 1: HAIKU, no
effort, max_tokens 1024). The request carries only the document and static text (contract rule 9)."""

from __future__ import annotations

from typing import ClassVar

from ai.agents.extraction.normalize import normalize_trn
from ai.agents.intake.schema import IntakeResult
from ai.agents.source import SourceDocument
from ai.gateway.types import HAIKU, Message, ModelId, ModelRequest, TextPart
from ai.runtime.agent import AgentContext, LLMAgent

PROMPT_ID = "intake.classify"
PROMPT_VERSION = 1

SYSTEM = """You are the intake clerk of a UAE e-invoicing platform used by accounting firms. You look at one \
uploaded file and classify it before any data is extracted. You never extract line items or amounts.

Decide:
- kind: "invoice" for a tax invoice or simplified tax invoice (including thermal till receipts that show a \
seller TRN and VAT); "credit_note" for a tax credit note; "contract" for agreements, letters, quotations, \
purchase orders and statements; "other" for anything else (photos, blank pages, IDs, bank slips).
- language: "ar" when the document is written in Arabic, "en" when in English, "mixed" when both carry \
substantive content (a bilingual invoice is "mixed"). Printed company names alone do not make it mixed.
- invoice_count: how many separate invoices or credit notes the file contains (0 when kind is contract or other).
- seller_trn and buyer_trn: the 15-digit UAE Tax Registration Numbers exactly as printed for the seller \
(supplier) and the buyer (customer), written with Western digits and no spaces; "" when not printed.
- confidence: your probability (0 to 1) that kind is correct.

Read only the file. Never guess a TRN that is not printed."""

INSTRUCTION = "Classify the attached file."


class IntakeAgent(LLMAgent[SourceDocument, IntakeResult]):
    name: ClassVar[str] = "intake"
    version: ClassVar[int] = 1
    input_model: ClassVar[type[SourceDocument]] = SourceDocument
    output_model: ClassVar[type[IntakeResult]] = IntakeResult
    tools: ClassVar[tuple[str, ...]] = ()
    prompt_id = PROMPT_ID
    prompt_version = PROMPT_VERSION

    def __init__(self, model: ModelId = HAIKU) -> None:
        self.model = model

    def build_request(self, inp: SourceDocument) -> ModelRequest:
        return ModelRequest(model=self.model, prompt_id=self.prompt_id, prompt_version=self.prompt_version,
                            system=SYSTEM, messages=(Message("user", (*inp.parts(), TextPart(INSTRUCTION))),),
                            output_model=IntakeResult, max_tokens=1024, effort=None)

    async def run(self, ctx: AgentContext, inp: SourceDocument) -> IntakeResult:
        resp = await ctx.complete(self.build_request(inp))
        raw = IntakeResult.model_validate(resp.parsed)
        out = raw.model_copy(update={
            "seller_trn": normalize_trn(raw.seller_trn), "buyer_trn": normalize_trn(raw.buyer_trn),
            "invoice_count": max(0, raw.invoice_count), "confidence": round(min(1.0, max(0.0, raw.confidence)), 3),
        })
        ctx.emit("intake.classified", kind=out.kind, language=out.language, count=str(out.invoice_count))
        return out
