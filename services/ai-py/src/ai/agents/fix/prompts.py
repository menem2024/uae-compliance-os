"""The `fix.invoice_fields` prompt (spec 5.7.1: SONNET, effort low, max_tokens 2048, structured output, no tools).

The request carries only the invoice's own content plus static text (agent-runtime contract rule 9). Any change
to SYSTEM, the output schema or the model bumps PROMPT_VERSION (rule 8).
"""

from __future__ import annotations

import json

from ai.agents.fix.types import LLMFixInput, LLMFixOutput
from ai.gateway.types import SONNET, Message, ModelId, ModelRequest, TextPart

PROMPT_ID = "fix.invoice_fields"
PROMPT_VERSION = 1
MAX_TOKENS = 2048

SYSTEM = """You repair single data fields of one UAE e-invoice (PINT-AE, UBL based). You receive the invoice as \
JSON (canonical field names) and a list of validation issues. Each issue names the rule that failed, the field \
path it failed on, the current value of that field ("" when the field is absent) and the rule's message.

For each issue decide whether the correct value of exactly that field follows with certainty from the invoice \
itself, or from the format the rule demands, and if so give it. Typical repairs: a code written in the wrong \
case or spelling (unit codes are UN/ECE Recommendation 20 codes in upper case such as H87; country codes are \
ISO 3166-1 alpha-2 in upper case; VAT category codes are S, Z, E, AE, O or N), a date or time in the wrong \
format (dates are YYYY-MM-DD, times are hh:mm:ss with an optional zone such as +04:00), an emirate code \
(AUH, DXB, SHJ, UAQ, FUJ, AJM or RAK) that the address city names, or a reason code that the line already \
describes.

Rules:
- Change only paths that appear in the issue list, at most one change per path, using the path exactly as given.
- new_value is the complete new value of the field, in the exact format the rule demands.
- Never invent identifiers, registration or tax numbers, names, amounts, quantities or dates that the invoice \
does not already show. When the right value is not certain, leave that issue out.
- rule_ids lists the rule ids from the issue list that your change resolves; rationale is one short sentence.
- confidence is your probability (0 to 1) that every change you return is correct; 0 when you return none.

An empty changes list is a valid answer."""

INSTRUCTION = "Return the field changes that resolve these issues."


def build_request(inp: LLMFixInput, model: ModelId = SONNET) -> ModelRequest:
    issues = json.dumps([i.model_dump() for i in inp.issues], ensure_ascii=False, indent=1)
    text = f"Invoice:\n{inp.invoice_json}\n\nIssues:\n{issues}\n\n{INSTRUCTION}"
    return ModelRequest(model=model, prompt_id=PROMPT_ID, prompt_version=PROMPT_VERSION, system=SYSTEM,
                        messages=(Message("user", (TextPart(text),)),), output_model=LLMFixOutput,
                        max_tokens=MAX_TOKENS, effort="low")
