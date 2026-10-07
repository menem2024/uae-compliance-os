"""The `fix` agent: its one model step, plus the read-only `validate_invoice` tool every node uses.

The model never gets tools (spec 5.7.1); `FixAgent.tools` is empty. The graph nodes that talk to validator-rs
hold the `validate_invoice` allowlist themselves (`Node.tools`).
"""

from __future__ import annotations

import json
from typing import ClassVar

from pydantic import BaseModel

from ai.agents.fix.convert import invoice_from_dict, run_json
from ai.agents.fix.prompts import build_request
from ai.agents.fix.types import LLMFixInput, LLMFixOutput
from ai.agents.fix.validator_client import ValidatorLike
from ai.gateway.types import SONNET, ModelId
from ai.runtime.agent import AgentContext
from ai.runtime.tools import Tool, ToolContext, tool

AGENT_NAME = "fix"
TOOL_NAME = "validate_invoice"


class FixAgent:
    name: ClassVar[str] = AGENT_NAME
    version: ClassVar[int] = 1
    input_model: ClassVar[type[BaseModel]] = LLMFixInput
    output_model: ClassVar[type[BaseModel]] = LLMFixOutput
    tools: ClassVar[tuple[str, ...]] = ()

    def __init__(self, model: ModelId = SONNET) -> None:
        self.model = model

    async def run(self, ctx: AgentContext, inp: LLMFixInput) -> LLMFixOutput:
        resp = await ctx.complete(build_request(inp, self.model))
        return LLMFixOutput.model_validate(resp.parsed)


class ValidateInvoiceArgs(BaseModel):
    invoice_json: str  # protojson of compliance.v1.Invoice, proto field names
    ruleset_version: str  # "" = the validator's default RuleSet


def make_validate_tool(validator: ValidatorLike) -> Tool:
    """`validate_invoice` (side effects: read): runs the RuleSet and returns the ValidationRun as protojson."""

    @tool(name=TOOL_NAME, version=1, side_effects="read", timeout_s=15.0,
          description="Validate one invoice with validator-rs and return its ValidationRun.")
    async def validate_invoice(ctx: ToolContext, args: ValidateInvoiceArgs) -> str:
        run = await validator.validate(invoice_from_dict(json.loads(args.invoice_json)), args.ruleset_version)
        return run_json(run)

    return validate_invoice
