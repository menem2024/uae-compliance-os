"""document_ingestion@1 end to end through the real GraphExecutor (spec section 5.2, contract section 3.5):
FakeGateway, MemorySink, FakeClock and a fake fetch/publish; Tasks 14 and 18's real agents and verifier."""

import hashlib
import io
import uuid
import zipfile
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from decimal import Decimal

import pypdfium2 as pdfium
import pytest

from ai.agents.extraction.agent import ExtractionAgent
from ai.agents.extraction.schema import ExtractedInvoice, ExtractionOutput, FieldConfidence, flatten
from ai.agents.intake.agent import IntakeAgent
from ai.agents.intake.schema import IntakeResult
from ai.agents.orchestrator.workflow import (
    BUDGET,
    WORKFLOW,
    RetryLater,
    build_finalize,
    build_graph,
    identity_of,
)
from ai.agents.verifier.agent import VerifierAgent
from ai.agents.verifier.critic import ESCALATION_PROMPT_ID, CriticOutput, CriticReview, InvoiceCritic
from ai.agents.verifier.invoice import CRITICAL_PATHS, invoice_profile
from ai.gateway.errors import ModelRefusal, SpendCapExceeded, TransientModelError
from ai.gateway.fake import FakeGateway
from ai.gateway.types import OPUS, SONNET, ModelRequest, TextPart
from ai.gen.compliance.v1 import agents_pb2, documents_pb2
from ai.runtime.clock import FakeClock
from ai.runtime.events import MemorySink
from ai.runtime.graph import GraphExecutor
from ai.runtime.registry import AgentRegistry
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import Budget, RunOutcome, RunStatus, StepStatus
from ai.synthetic.generator import generate
from ai.synthetic.tabular import write_csv, write_xlsx

FIRM = "00000000-0000-4000-8000-00000000e001"
OTHER_FIRM = "00000000-0000-4000-8000-00000000e002"
CC = "00000000-0000-4000-8000-00000000c001"
OTHER_CC = "00000000-0000-4000-8000-00000000c002"
DOC = "00000000-0000-4000-8000-00000000d001"
KEY = f"firms/{FIRM}/docs/{DOC}"
CANDIDATE_NAME = "Zebra Holdings Private Name LLC"  # Firm data: must never reach a model request
UNRELATED_TRN = "100000000000077"
STATIC_PLAN = ["fetch", "route", "intake.classify", "extraction.extract", "verify.arithmetic",
               "verify.identifiers", "verify.dates_codes", "verify.critic", "verify.join", "intake.attribute",
               "import.tabular", "emit"]


def registry() -> AgentRegistry:
    reg = AgentRegistry()
    reg.add_agent(IntakeAgent())
    reg.add_agent(ExtractionAgent())
    return reg


def verifier() -> VerifierAgent:
    return VerifierAgent([invoice_profile(InvoiceCritic(model=SONNET),
                                          InvoiceCritic(model=OPUS, prompt_id=ESCALATION_PROMPT_ID))])


def pdf(pages: int = 1) -> bytes:
    doc = pdfium.PdfDocument.new()
    for _ in range(pages):
        doc.new_page(595, 842)
    buf = io.BytesIO()
    doc.save(buf)
    doc.close()
    return buf.getvalue()


def upload(data: bytes, *, cc_trn: str = "", other_trn: str = UNRELATED_TRN, object_key: str = KEY,
           sha256: str | None = None) -> documents_pb2.DocumentUploaded:
    return documents_pb2.DocumentUploaded(
        document_id=DOC, firm_id=FIRM, client_company_id=CC, sha256=sha256 or hashlib.sha256(data).hexdigest(),
        object_key=object_key, content_type="application/pdf", size_bytes=len(data), filename="scan.pdf",
        candidates=[documents_pb2.ClientCompanyRef(client_company_id=CC, name="Chosen Co", trn=cc_trn),
                    documents_pb2.ClientCompanyRef(client_company_id=OTHER_CC, name=CANDIDATE_NAME, trn=other_trn)],
        reprocess_nonce="n1")


def intake(kind: str = "invoice", count: int = 1) -> IntakeResult:
    return IntakeResult(kind=kind, language="en", invoice_count=count if kind in ("invoice", "credit_note") else 0,
                        seller_trn="", buyer_trn="", confidence=0.97)


def extraction(inv: ExtractedInvoice, low: tuple[str, ...] = ()) -> ExtractionOutput:
    return ExtractionOutput(invoice=inv, language="en", field_confidence=[
        FieldConfidence(path=p, confidence=0.5 if p in low else 0.95) for p in CRITICAL_PATHS])


def reader(printed: ExtractedInvoice) -> Callable[[ModelRequest], CriticOutput]:
    """A critic that reads `printed` from the document for every path listed in its request."""
    values = flatten(printed)

    def script(req: ModelRequest) -> CriticOutput:
        text = req.messages[0].parts[-1].text  # type: ignore[union-attr]
        paths = [line[2:].split(": ", 1)[0] for line in text.splitlines() if line.startswith("- ")]
        return CriticOutput(reviews=[CriticReview(path=p, document_value=values.get(p, ""), matches=True)
                                     for p in paths])

    return script


def script(**by_prompt: object) -> Callable[[ModelRequest], object]:
    """prompt_id (dots as underscores) -> a value, an exception, or a callable(req)."""
    table = {k.replace("_", ".", 1): v for k, v in by_prompt.items()}

    def fn(req: ModelRequest) -> object:
        value = table[req.prompt_id]
        return value(req) if callable(value) and not isinstance(value, type) else value

    return fn


class Store:
    def __init__(self, objects: Mapping[str, bytes]) -> None:
        self.objects = dict(objects)
        self.asked: list[str] = []

    async def __call__(self, key: str) -> bytes:
        self.asked.append(key)
        return self.objects[key]


@dataclass
class Run:
    outcome: RunOutcome | None
    gateway: FakeGateway
    sink: MemorySink
    store: Store
    published: list[object] = field(default_factory=list)
    finalized: list[RunOutcome] = field(default_factory=list)
    finished_before_publish: list[int] = field(default_factory=list)
    error: Exception | None = None

    @property
    def prompts(self) -> list[str]:
        return [c.prompt_id for c in self.gateway.calls]

    def extracted(self) -> documents_pb2.DocumentExtracted:
        assert len(self.published) == 1
        msg = self.published[0]
        assert isinstance(msg, documents_pb2.DocumentExtracted)
        return msg

    def failed(self) -> documents_pb2.DocumentFailed:
        assert len(self.published) == 1
        msg = self.published[0]
        assert isinstance(msg, documents_pb2.DocumentFailed)
        return msg


async def _no_sleep(_s: float) -> None:
    return None


async def ingest(data: bytes, gw_script: object, *, up: documents_pb2.DocumentUploaded | None = None,
                 max_pdf_pages: int = 10, budget: Budget = BUDGET) -> Run:
    up = up or upload(data)
    run = Run(None, FakeGateway(gw_script), MemorySink(), Store({KEY: data}))  # type: ignore[arg-type]

    async def publish(msg: documents_pb2.DocumentExtracted | documents_pb2.DocumentFailed) -> None:
        run.finished_before_publish.append(len(run.sink.finished))
        run.published.append(msg)

    finalize = build_finalize(up, publish)

    async def spy(outcome: RunOutcome) -> None:
        run.finalized.append(outcome)
        await finalize(outcome)

    graph = build_graph(up, fetch=run.store, registry=registry(), verifier=verifier(), max_pdf_pages=max_pdf_pages)
    ex = GraphExecutor(gateway=run.gateway, sink=run.sink, tools=ToolRegistry(), clock=FakeClock(), sleep=_no_sleep)
    try:
        run.outcome = await ex.run(graph, identity_of(up, 1), budget, finalize=spy)
    except RetryLater as exc:
        run.error = exc
    return run


def request_texts(gw: FakeGateway) -> str:
    return "\n".join(p.text for c in gw.calls for m in c.messages for p in m.parts if isinstance(p, TextPart)) + \
        "\n".join(c.system for c in gw.calls)


# ------------------------------------------------------------------ LLM branch
async def test_clean_pdf_end_to_end():
    truth = generate(11, "en", defect_rate=0.0).truth
    data = pdf()
    run = await ingest(data, script(intake_classify=intake(), extraction_invoice=extraction(truth)),
                       up=upload(data, cc_trn=truth.seller_trn))
    assert run.outcome is not None and run.outcome.status is RunStatus.SUCCEEDED
    assert run.prompts == ["intake.classify", "extraction.invoice"]
    msg = run.extracted()
    assert (msg.document_id, msg.firm_id, msg.client_company_id, msg.run_id) == (
        DOC, FIRM, CC, run.outcome.identity.run_id)
    assert (msg.document_kind, msg.direction, msg.extraction_method, msg.language) == ("invoice", "issued", "llm", "en")
    assert not msg.needs_review and list(msg.review_reasons) == []
    assert len(msg.invoices) == 1
    inv = msg.invoices[0]
    assert inv.invoice.invoice_number == truth.invoice_number and inv.verdict.verdict == agents_pb2.VERDICT_ACCEPT
    assert inv.confidence == 0.95 and {f.path for f in inv.fields} == set(CRITICAL_PATHS)
    # the plan is static and complete before anything runs; document.extracted precedes agent.run.finished
    assert [n.node_id for n in run.sink.started[0][2]] == STATIC_PLAN
    assert run.finished_before_publish == [0] and len(run.sink.finished) == 1
    assert run.sink.proposals == []
    # contract rule 9: Firm and ClientCompany data never reach a model request
    texts = request_texts(run.gateway)
    assert CANDIDATE_NAME not in texts and UNRELATED_TRN not in texts and "Chosen Co" not in texts


async def test_one_revision_and_the_revised_extraction_wins():
    truth = generate(12, "en", defect_rate=0.0).truth
    bad = truth.model_copy(deep=True)
    bad.total_amount = str(Decimal(truth.total_amount) + 100)
    run = await ingest(pdf(), script(intake_classify=intake(),
                                     extraction_invoice=extraction(bad, low=("total_amount",)),
                                     verifier_critic=reader(truth), extraction_revise=extraction(truth)))
    assert run.prompts == ["intake.classify", "extraction.invoice", "verifier.critic", "extraction.revise"]
    revise = run.gateway.calls[3]
    assert f'total_amount: "{bad.total_amount}" -> "{truth.total_amount}"' in revise.messages[0].parts[-1].text  # type: ignore[union-attr]
    assert run.outcome is not None and run.outcome.node_status["extraction.revise#1"] is StepStatus.SUCCEEDED
    msg = run.extracted()
    assert msg.invoices[0].invoice.total_amount == truth.total_amount
    assert msg.invoices[0].verdict.verdict == agents_pb2.VERDICT_ACCEPT and msg.invoices[0].verdict.revisions == 1
    assert not msg.needs_review


async def test_still_wrong_after_one_revision_escalates_to_opus_once():
    truth = generate(12, "en", defect_rate=0.0).truth
    bad = truth.model_copy(deep=True)
    bad.total_amount = str(Decimal(truth.total_amount) + 100)
    run = await ingest(pdf(), script(intake_classify=intake(),
                                     extraction_invoice=extraction(bad, low=("total_amount",)),
                                     verifier_critic=reader(truth), extraction_revise=extraction(bad),
                                     verifier_escalate=reader(truth)))
    assert run.prompts == ["intake.classify", "extraction.invoice", "verifier.critic", "extraction.revise",
                           "verifier.critic", "verifier.escalate"]
    assert [c.model for c in run.gateway.calls].count(OPUS) == 1 and run.gateway.calls[-1].model == OPUS
    msg = run.extracted()
    assert msg.needs_review and list(msg.review_reasons) == ["verifier_escalated"]
    assert msg.invoices[0].verdict.verdict == agents_pb2.VERDICT_ESCALATE
    assert msg.invoices[0].verdict.critic_model == OPUS


async def test_a_contract_is_not_an_invoice_and_is_never_extracted():
    run = await ingest(pdf(), script(intake_classify=intake("contract")))
    assert run.prompts == ["intake.classify"]
    msg = run.extracted()
    assert msg.document_kind == "contract" and msg.needs_review and list(msg.review_reasons) == ["not_invoice"]
    assert len(msg.invoices) == 0
    assert run.outcome is not None
    assert run.outcome.node_status["extraction.extract"] is StepStatus.SKIPPED
    assert run.outcome.node_status["verify.join"] is StepStatus.SKIPPED


async def test_multi_invoice_pdf_is_flagged():
    truth = generate(11, "en", defect_rate=0.0).truth
    run = await ingest(pdf(), script(intake_classify=intake(count=3), extraction_invoice=extraction(truth)))
    msg = run.extracted()
    assert len(msg.invoices) == 1 and list(msg.review_reasons) == ["multi_invoice_pdf"]


async def test_too_many_pages_makes_no_model_call():
    run = await ingest(pdf(3), script(), max_pdf_pages=2)
    assert run.gateway.calls == []
    msg = run.extracted()
    assert msg.needs_review and list(msg.review_reasons) == ["too_many_pages"] and len(msg.invoices) == 0
    assert msg.document_kind == "invoice"  # api-go keeps it a reviewable document, not not_invoice
    assert run.outcome is not None and run.outcome.node_status["intake.classify"] is StepStatus.SKIPPED


async def test_a_refusal_becomes_a_review_reason():
    run = await ingest(pdf(), script(intake_classify=intake(), extraction_invoice=ModelRefusal("cyber")))
    assert run.prompts == ["intake.classify", "extraction.invoice"]  # permanent: not retried
    assert run.outcome is not None and run.outcome.status is RunStatus.SUCCEEDED
    msg = run.extracted()
    assert list(msg.review_reasons) == ["model_refusal"] and len(msg.invoices) == 0


async def test_spend_cap_on_intake_is_a_review_reason_not_a_failed_run():
    run = await ingest(pdf(), script(intake_classify=SpendCapExceeded("firm cap reached")))
    assert run.prompts == ["intake.classify"]
    assert run.outcome is not None and run.outcome.status is RunStatus.SUCCEEDED
    msg = run.extracted()
    assert list(msg.review_reasons) == ["spend_cap_exceeded"] and len(msg.invoices) == 0
    assert msg.document_kind == "invoice"  # kind unknown: still a reviewable, reprocessable document


async def test_attribution_proposal_for_another_candidate():
    truth = generate(11, "en", defect_rate=0.0).truth
    data = pdf()
    run = await ingest(data, script(intake_classify=intake(), extraction_invoice=extraction(truth)),
                       up=upload(data, cc_trn=UNRELATED_TRN, other_trn=truth.buyer_trn))
    assert len(run.sink.proposals) == 1
    draft = run.sink.proposals[0][1]
    assert draft.kind == "document.attribution" and draft.target_id == DOC
    assert [(c.path, c.old_value, c.new_value) for c in draft.changes] == [
        ("client_company_id", CC, OTHER_CC), ("direction", "unknown", "received")]
    assert run.extracted().direction == "unknown"  # relative to the chosen ClientCompany until a human accepts


async def test_a_27_digit_quantity_is_reviewed_not_a_failed_document():
    """Review B #3: the verifier's quantize overflow turned this into DocumentFailed(internal). Here the model
    misread the quantity twice; the critic reads the printed one. (A value the critic confirms as printed
    follows the binding critic-confirmation rule: validity is validator-rs's question.)"""
    truth = generate(11, "en", defect_rate=0.0).truth
    bad = truth.model_copy(deep=True)
    bad.lines[0].quantity = "1" * 27
    run = await ingest(pdf(), script(intake_classify=intake(), extraction_invoice=extraction(bad),
                                     verifier_critic=reader(truth), extraction_revise=extraction(bad),
                                     verifier_escalate=reader(truth)))
    assert run.outcome is not None and run.outcome.status is RunStatus.SUCCEEDED
    assert "verify.arithmetic" in run.outcome.node_status
    assert run.outcome.node_status["verify.arithmetic"] is StepStatus.SUCCEEDED
    msg = run.extracted()
    assert msg.needs_review and "verifier_escalated" in msg.review_reasons
    assert msg.invoices[0].verdict.verdict == agents_pb2.VERDICT_ESCALATE


# ------------------------------------------------------------------ tabular branch
@pytest.mark.parametrize(("method", "writer"), [("xlsx", write_xlsx), ("csv", write_csv)])
async def test_four_invoice_spreadsheet_makes_no_model_call(method, writer):
    truths = [generate(600 + k, "en").truth for k in range(4)]
    data = writer(truths, "en")
    run = await ingest(data, script())
    assert run.gateway.calls == []  # AC-6
    msg = run.extracted()
    assert msg.extraction_method == method and msg.language == "" and msg.document_kind == "invoice"
    assert [e.invoice.invoice_number for e in msg.invoices] == [t.invoice_number for t in truths]
    assert [e.source_ordinal for e in msg.invoices] == [0, 1, 2, 3]
    assert all(e.HasField("verdict") for e in msg.invoices)
    assert run.outcome is not None
    assert {f"verify.inv#{i}" for i in range(4)} | {"verify.all"} <= set(run.outcome.node_status)
    assert run.outcome.node_status["intake.classify"] is StepStatus.SKIPPED
    assert run.outcome.node_status["verify.join"] is StepStatus.SKIPPED
    verdicts = [e.verdict.verdict for e in msg.invoices]
    assert verdicts[:3] == [agents_pb2.VERDICT_ACCEPT] * 3  # clean rows
    assert verdicts[3] == agents_pb2.VERDICT_ESCALATE and msg.needs_review  # printed line-sum defect, no critic


async def test_a_1e30_cell_is_reviewed_not_a_failed_sheet():
    truths = [generate(600 + k, "en", defect_rate=0.0).truth.model_copy(deep=True) for k in range(2)]
    truths[1].lines[0].net_amount = "1" + "0" * 30  # an amount cell, '#,##0.00'
    run = await ingest(write_xlsx(truths, "en"), script())
    assert run.outcome is not None and run.outcome.status is RunStatus.SUCCEEDED
    msg = run.extracted()
    assert [e.verdict.verdict for e in msg.invoices] == [agents_pb2.VERDICT_ACCEPT, agents_pb2.VERDICT_ESCALATE]
    assert msg.needs_review and "verifier_escalated" in msg.review_reasons


async def test_a_large_spreadsheet_is_verified_in_bounded_chunks():
    truths = [generate(700 + k, "en", defect_rate=0.0).truth for k in range(40)]
    run = await ingest(write_csv(truths, "en"), script())
    msg = run.extracted()
    assert len(msg.invoices) == 40 and run.gateway.calls == []
    assert run.outcome is not None and run.outcome.status is RunStatus.SUCCEEDED
    assert sum(1 for n in run.outcome.node_status if n.startswith("verify.inv#")) <= 16


def _zip_bomb() -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        for i in range(1001):
            zf.writestr(f"x{i}.xml", b"\0")
    return buf.getvalue()


async def test_a_rejected_spreadsheet_is_unsupported_format():
    run = await ingest(_zip_bomb(), script())
    assert run.outcome is not None and run.outcome.status is RunStatus.FAILED
    msg = run.failed()
    assert (msg.reason_code, msg.detail, msg.run_id) == ("unsupported_format", "zip_bomb", run.outcome.identity.run_id)


# ------------------------------------------------------------------ failures
async def test_tenant_mismatch_is_document_failed():
    data = pdf()
    run = await ingest(data, script(), up=upload(data, object_key=f"firms/{OTHER_FIRM}/docs/{DOC}"))
    assert run.failed().reason_code == "tenant_mismatch" and run.store.asked == []


async def test_sha256_mismatch_is_document_failed():
    data = pdf()
    run = await ingest(data, script(), up=upload(data, sha256=hashlib.sha256(b"older bytes").hexdigest()))
    assert run.failed().reason_code == "sha256_mismatch" and run.gateway.calls == []


async def test_budget_stop_still_publishes_a_reviewable_result():
    run = await ingest(pdf(), script(intake_classify=intake()), budget=Budget(max_llm_calls=1))
    assert run.outcome is not None and run.outcome.status is RunStatus.BUDGET_EXCEEDED
    msg = run.extracted()
    assert list(msg.review_reasons) == ["budget_exceeded"] and len(msg.invoices) == 0
    assert msg.document_kind == "invoice"


async def test_transient_failure_after_retries_naks_and_publishes_nothing():
    run = await ingest(pdf(), script(intake_classify=intake(),
                                     extraction_invoice=TransientModelError("overloaded")))
    assert run.prompts == ["intake.classify"] + ["extraction.invoice"] * 3  # RetryPolicy.max_attempts
    assert isinstance(run.error, RetryLater) and run.error.code == "model_transient"
    assert len(run.finalized) == 1 and run.finalized[0].status is RunStatus.SUCCEEDED
    assert run.published == []
    assert len(run.sink.finished) == 1
    assert run.sink.finished[0].status is RunStatus.FAILED and run.sink.finished[0].error_code == "finalize_failed"


def test_identity_is_new_per_delivery():
    up = upload(b"x")
    a, b = identity_of(up, 1), identity_of(up, 2)
    assert a.run_id != b.run_id and uuid.UUID(a.run_id).version == 4
    assert (b.firm_id, b.client_company_id, b.workflow, b.subject_type, b.subject_id, b.delivery_attempt) == (
        FIRM, CC, WORKFLOW, "document", DOC, 2)
    assert WORKFLOW == "document_ingestion@1" and BUDGET == Budget()
