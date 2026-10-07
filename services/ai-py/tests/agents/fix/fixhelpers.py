"""Shared helpers for the Fix agent tests: a small scripted rule engine standing in for validator-rs.

No test here talks to a real validator or model: `FakeValidatorClient` runs `mini_engine`, `FakeGateway` answers
the one prompt `fix.invoice_fields`.
"""

from __future__ import annotations

import uuid
from collections.abc import Callable

from ai.agents.fix import paths
from ai.agents.fix.config import FixConfig
from ai.agents.fix.graph import FixTaskHandler
from ai.agents.fix.types import LLMChange, LLMFixOutput
from ai.agents.fix.validator_client import FakeValidatorClient
from ai.agents.fix.verifier_profile import fix_profile
from ai.agents.verifier.agent import VerifierAgent
from ai.gateway.fake import FakeGateway
from ai.gen.compliance.v1 import agents_pb2, invoice_pb2, validator_pb2
from ai.runtime.events import MemorySink
from ai.runtime.graph import GraphExecutor
from ai.runtime.types import Budget, RunIdentity, RunOutcome

FIRM = "00000000-0000-4000-8000-0000000000f1"
INVOICE_ID = "11111111-1111-4111-8111-111111111111"
EMIRATES = frozenset({"AUH", "DXB", "SHJ", "UAQ", "FUJ", "AJM", "RAK"})
LINE_EXT, TAX_EXCL, PAYABLE = "totals.line_extension_amount", "totals.tax_exclusive_amount", "totals.payable_amount"
UNIT, BASE = "lines[0].unit_code", "lines[0].price.base_quantity_unit_code"
EMIRATE = "seller.postal_address.country_subdivision"
BUYER_COUNTRY = "buyer.postal_address.country_code"


def issue(rule: str, path: str, *, suggested: str = "", fixable: bool = True,
          severity: int = validator_pb2.SEVERITY_ERROR) -> validator_pb2.ValidationIssue:
    return validator_pb2.ValidationIssue(rule_id=rule, severity=severity, path=path, message=f"{rule} failed",
                                         business_term="IBT-000", fixable=fixable, suggested_value=suggested)


def mini_engine(inv: invoice_pb2.Invoice, _version: str = "") -> validator_pb2.ValidationRun:
    """A tiny RuleSet. The totals chain settles in exactly three rounds; the unit rules interact (a lower-case unit
    code changes what the base-unit rule suggests); an emirate that is not a code is a NEW rule id."""
    g = lambda p: paths.get_value(inv, p)
    out: list[validator_pb2.ValidationIssue] = []
    emirate = g(EMIRATE)
    if not emirate:
        out.append(issue("ibr-143-ae", EMIRATE))
    elif emirate not in EMIRATES:
        out.append(issue("AE-EMIRATE-X", EMIRATE))
    unit, base = g(UNIT), g(BASE)
    if unit != unit.upper():
        out.append(issue("ibr-cl-23", UNIT))
    if base != base.upper():
        out.append(issue("ibr-cl-23", BASE))
    if base != unit:
        out.append(issue("ibr-088", BASE, suggested=unit))
    if g(LINE_EXT) != "100.00":
        out.append(issue("ibr-co-10", LINE_EXT, suggested="100.00"))
    if g(TAX_EXCL) != g(LINE_EXT):
        out.append(issue("ibr-co-13", TAX_EXCL, suggested=g(LINE_EXT)))
    if g(PAYABLE) != g(TAX_EXCL):
        out.append(issue("ibr-co-16", PAYABLE, suggested=g(TAX_EXCL)))
    if not g("invoice_number"):
        out.append(issue("ibr-006", "invoice_number", suggested="INV-1"))  # a forbidden path: never applied
    if g(BUYER_COUNTRY) != g(BUYER_COUNTRY).upper():
        out.append(issue("ibr-cl-14", BUYER_COUNTRY))
    return validator_pb2.ValidationRun(ruleset_version="pint-ae@test", issues=out, rules_evaluated=10)


def make_invoice(**over: str) -> invoice_pb2.Invoice:
    """A valid invoice for `mini_engine`, with the given paths overridden (`__` stands for `.`)."""
    inv = invoice_pb2.Invoice()
    base = {"invoice_number": "INV-7", EMIRATE: "DXB", UNIT: "H87", BASE: "H87", LINE_EXT: "100.00",
            TAX_EXCL: "100.00", PAYABLE: "100.00", BUYER_COUNTRY: "AE"}
    base.update({k.replace("__", "."): v for k, v in over.items()})
    for p, v in base.items():
        paths.set_value(inv, p, v)
    return inv


def task_input(inv: invoice_pb2.Invoice, *, mode: str = "on_demand", run: validator_pb2.ValidationRun | None = None,
               ) -> validator_pb2.FixTaskInput:
    run = run or mini_engine(inv)
    targets = [i for i in run.issues if i.severity == validator_pb2.SEVERITY_ERROR and i.fixable]
    return validator_pb2.FixTaskInput(invoice_id=INVOICE_ID, payload_version=3, validation_run_id="run-9",
                                      ruleset_version="pint-ae@test", invoice=inv, issues=targets, mode=mode)


def request(inp: validator_pb2.FixTaskInput) -> agents_pb2.AgentTaskRequested:
    req = agents_pb2.AgentTaskRequested(task_id="t-1", firm_id=FIRM, agent="fix", subject_type="invoice",
                                        subject_id=inp.invoice_id)
    req.input.Pack(inp)
    return req


def llm_answer(*changes: tuple[str, str], confidence: float = 0.95) -> LLMFixOutput:
    return LLMFixOutput(changes=[LLMChange(path=p, new_value=v, rule_ids=[], rationale=f"set {p}")
                                 for p, v in changes], confidence=confidence)


class Harness:
    def __init__(self, gateway: FakeGateway, validator: FakeValidatorClient, sink: MemorySink,
                 handler: FixTaskHandler) -> None:
        self.gateway, self.validator, self.sink, self.handler = gateway, validator, sink, handler

    async def run(self, inp: validator_pb2.FixTaskInput, budget: Budget | None = None) -> RunOutcome:
        plan = await self.handler(request(inp))
        ident = RunIdentity(run_id=str(uuid.uuid4()), firm_id=FIRM, client_company_id="", workflow=plan.graph.workflow,
                            subject_type="invoice", subject_id=inp.invoice_id)
        from ai.agents.fix.agent import make_validate_tool
        from ai.runtime.tools import ToolRegistry

        async def no_sleep(_s: float) -> None:
            return None

        ex = GraphExecutor(gateway=self.gateway, sink=self.sink, tools=ToolRegistry([make_validate_tool(self.validator)]),
                           sleep=no_sleep)
        return await ex.run(plan.graph, ident, budget or Budget())


def make_harness(*, answer: LLMFixOutput | Callable | Exception | None = None, engine: Callable | None = None,
                 config: FixConfig | None = None) -> Harness:
    if callable(answer):
        script = answer
    else:
        script = lambda _r: answer if answer is not None else llm_answer()
    validator = FakeValidatorClient(engine or mini_engine)
    handler = FixTaskHandler(VerifierAgent([fix_profile()]), lambda: config or FixConfig())
    return Harness(FakeGateway(script), validator, MemorySink(), handler)
