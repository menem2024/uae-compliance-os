"""The `document_ingestion@1` workflow (spec section 5.2; contract sections 3.5 and 4.2).

A pure, injectable graph factory: no S3 client and no NATS publish live here. Task 21 supplies `fetch` and
`publish` and drives GraphExecutor:

    graph = build_graph(upload, fetch=..., registry=..., verifier=..., max_pdf_pages=...)
    await executor.run(graph, identity_of(upload, attempt), BUDGET, finalize=build_finalize(upload, publish))

    fetch -> route -+-> intake.classify -> extraction.extract -+-> verify.{arithmetic,identifiers,dates_codes}
                    |                                          |     -> verify.critic? -> verify.join -+-> emit
                    |                                          +-> intake.attribute ------------------+
                    +-> import.tabular -> (fan_out) verify.inv#0..n -> verify.all --------------------> emit

The plan is static except the bounded revise/escalate expansions (the verifier's on_revise and escalation) and
the tabular fan-out; branches are `when` predicates on ctx.results["route"]. Only fetch, route and
import.tabular are critical: their failures are document.failed. LLM nodes are non-critical, so a permanent
model failure flows into emit as a review reason. The verifier's deterministic check and join nodes keep the
verifier's own default (critical): a crashing check must fail the document, never let an unchecked invoice be
accepted. A transient model or tool failure that outlives its node's retries makes `finalize` raise RetryLater,
so the caller naks the message instead of acking it (spec section 5.1).

One graph per run: emit reads a per-graph record of node failures.
"""

from __future__ import annotations

import asyncio
import uuid
from collections.abc import Awaitable, Callable, Mapping, Sequence
from dataclasses import dataclass, replace
from typing import Any, cast

from ai.agents.extraction.agent import ExtractionInput, Feedback
from ai.agents.extraction.schema import ExtractedInvoice, ExtractionOutput
from ai.agents.extraction.tabular.importer import (
    ImportedInvoice,
    ImportResult,
    TabularRejected,
    import_tabular,
)
from ai.agents.intake.schema import IntakeResult
from ai.agents.orchestrator.assemble import InvoiceOutcome, build_extracted, build_failed, mark_synthetic
from ai.agents.orchestrator.attribute import attribute_node, client_company_trn, direction_of
from ai.agents.orchestrator.fetch import AGENT, CSV, XLSX, FetchCode, Fetched, FetchFn, fetch_node
from ai.agents.orchestrator.route import route_node
from ai.agents.source import MediaType, SourceDocument
from ai.agents.verifier.agent import VerifierAgent
from ai.agents.verifier.core import StageVerdict, VerdictResult, VerifierProfile, decide, final_verdict
from ai.agents.verifier.invoice import field_confidences, invoice_findings, invoice_profile
from ai.agents.verifier.proto import to_proto
from ai.gateway.errors import ModelRefusal, SpendCapExceeded, TransientModelError
from ai.gateway.types import Part
from ai.gen.compliance.v1 import documents_pb2
from ai.runtime.context import NodeContext, error_code_of
from ai.runtime.errors import BudgetExceeded, ToolTransientError
from ai.runtime.graph import Expand, Node, TaskGraph, fan_out
from ai.runtime.registry import AgentRegistry
from ai.runtime.types import Budget, RunIdentity, RunOutcome, RunStatus, StepKind

WORKFLOW = "document_ingestion@1"
BUDGET = Budget()
MAX_VERIFY_NODES = 16  # tabular fan-out width: <= 16 verify.inv nodes, whatever the invoice count (max_nodes 64)

INTAKE, EXTRACT, REVISE = "intake.classify", "extraction.extract", "extraction.revise#1"
IMPORT, VERIFY_ALL, EMIT = "import.tabular", "verify.all", "emit"
INVOICE_KINDS = frozenset({"invoice", "credit_note"})
TRANSIENT: tuple[type[Exception], ...] = (TransientModelError, ToolTransientError)
TRANSIENT_CODES = frozenset({TransientModelError.code, ToolTransientError.code})
FETCH_CODES: frozenset[FetchCode] = frozenset({"object_missing", "sha256_mismatch", "tenant_mismatch",
                                               "unsupported_format"})
TABULAR_CODES = frozenset({TabularRejected.ZIP_BOMB, TabularRejected.NOT_A_WORKBOOK, TabularRejected.TOO_MANY_ROWS,
                           TabularRejected.TOO_MANY_INVOICES, TabularRejected.EMPTY})
TABULAR_PROFILE: VerifierProfile[ExtractionOutput] = invoice_profile(critic=None)
UNVERIFIED = VerdictResult("escalate", 0.0, ())  # a verdict that could not be computed: a human looks

type PublishFn = Callable[[documents_pb2.DocumentExtracted | documents_pb2.DocumentFailed], Awaitable[None]]


class RetryLater(Exception):
    """A transient model or tool failure outlived its node's RetryPolicy: the caller naks the message."""

    def __init__(self, code: str) -> None:
        super().__init__(f"transient failure: {code}")
        self.code = code


@dataclass(frozen=True, slots=True)
class TransientFailure:
    """emit's result when the run must be retried rather than published."""

    node_id: str
    error_code: str


@dataclass(frozen=True, slots=True)
class _Failure:
    kind: StepKind
    code: str
    transient: bool
    reason: str  # review reason of a permanent LLM failure


def _review_reason(exc: Exception) -> str:
    if isinstance(exc, ModelRefusal):
        return "model_refusal"
    if isinstance(exc, SpendCapExceeded):
        return "spend_cap_exceeded"
    return "extraction_failed"


class _Watch:
    """Records the last failed attempt of every node, including nodes added by Expand; a success clears it.
    When emit runs, every upstream node is terminal, so what is left is how each failed node finally failed."""

    def __init__(self) -> None:
        self.failures: dict[str, _Failure] = {}

    def node(self, n: Node) -> Node:
        inner = n.fn

        async def fn(ctx: NodeContext) -> object:
            try:
                result = await inner(ctx)
            except BudgetExceeded:
                raise  # ends the run; finalize handles it
            except Exception as exc:
                self.failures[n.id] = _Failure(n.kind, error_code_of(exc), isinstance(exc, TRANSIENT),
                                               _review_reason(exc))
                raise
            self.failures.pop(n.id, None)
            if isinstance(result, Expand):
                return Expand(tuple(self.node(x) for x in result.nodes), result=result.result, join=result.join)
            return result

        return replace(n, fn=fn)


class _Source:
    """The fetched bytes as the SourceDocument every LLM node and the critic read (built once per run)."""

    def __init__(self) -> None:
        self._doc: SourceDocument | None = None

    def of(self, ctx: NodeContext) -> SourceDocument:
        if self._doc is None:
            f = ctx.result("fetch", Fetched)
            self._doc = SourceDocument.of(f.data, cast("MediaType", f.media_type))
        return self._doc


def identity_of(upload: documents_pb2.DocumentUploaded, delivery_attempt: int) -> RunIdentity:
    """A new run_id for every delivery attempt (contract section 3.1)."""
    return RunIdentity(run_id=str(uuid.uuid4()), firm_id=upload.firm_id, client_company_id=upload.client_company_id,
                       workflow=WORKFLOW, subject_type="document", subject_id=upload.document_id,
                       delivery_attempt=delivery_attempt)


def _route_is(branch: str) -> Callable[[Mapping[str, object]], bool]:
    return lambda r: r.get("route") == branch


def _is_invoice(r: Mapping[str, object]) -> bool:
    intake = r.get(INTAKE)
    return isinstance(intake, IntakeResult) and intake.kind in INVOICE_KINDS


def _tabular_output(inv: ExtractedInvoice) -> ExtractionOutput:
    # No field confidence: the producer confidence is the profile's "none reported" value. `language` is read
    # by neither the checks nor the verdict rule.
    return ExtractionOutput(invoice=inv, field_confidence=[], language="en")


def tabular_verdict(inv: ExtractedInvoice) -> VerdictResult:
    """Checks and the verdict rule only: no critic, no model call (AC-6)."""
    return decide(TABULAR_PROFILE, _tabular_output(inv), invoice_findings(inv), revisions=0)


def _chunks[T](items: Sequence[T], n: int) -> list[Sequence[T]]:
    size = -(-len(items) // n)
    return [items[i:i + size] for i in range(0, len(items), size)]


def _verify_chunk(node_id: str, chunk: Sequence[ImportedInvoice]) -> Node:
    async def fn(ctx: NodeContext) -> tuple[VerdictResult, ...]:
        verdicts = tuple(tabular_verdict(i.invoice) for i in chunk)
        ctx.emit("verifier.batch.checked", invoices=str(len(verdicts)),
                 flagged=str(sum(1 for v in verdicts if v.verdict != "accept")))
        return verdicts

    return Node(node_id, "verifier", "check.invoices", StepKind.DETERMINISTIC, fn, depends_on=(IMPORT,),
                critical=False)


def _verify_all(chunks: Sequence[Sequence[ImportedInvoice]]) -> Node:
    async def fn(ctx: NodeContext) -> tuple[VerdictResult, ...]:
        out: list[VerdictResult] = []
        for i, chunk in enumerate(chunks):
            got = ctx.results.get(f"verify.inv#{i}")
            ok = isinstance(got, tuple) and len(got) == len(chunk)
            out.extend(cast("tuple[VerdictResult, ...]", got) if ok else [UNVERIFIED] * len(chunk))
        ctx.emit("verifier.batch.joined", invoices=str(len(out)),
                 accepted=str(sum(1 for v in out if v.verdict == "accept")))
        return tuple(out)

    return Node(VERIFY_ALL, "verifier", "join", StepKind.DETERMINISTIC, fn, depends_on=(IMPORT,),
                trigger="all_done", critical=False)


async def _import(ctx: NodeContext) -> ImportResult | Expand:
    f = ctx.result("fetch", Fetched)
    fmt = "xlsx" if f.media_type == XLSX else "csv"
    res = await asyncio.to_thread(import_tabular, f.data, fmt)  # openpyxl is CPU-bound: keep the loop free
    ctx.emit("import.imported", method=fmt, invoices=str(len(res.invoices)),
             missing=",".join(res.missing_columns))
    if not res.invoices:
        return res
    chunks = _chunks(res.invoices, MAX_VERIFY_NODES)
    exp = fan_out("verify.inv", chunks, _verify_chunk, join=_verify_all(chunks))
    return Expand(exp.nodes, result=res, join=exp.join)


def build_graph(upload: documents_pb2.DocumentUploaded, *, fetch: FetchFn, registry: AgentRegistry,
                verifier: VerifierAgent, max_pdf_pages: int) -> TaskGraph:
    intake = registry.agents["intake"]
    extraction = registry.agents["extraction"]
    profile = verifier.profile(ExtractionOutput)  # fails fast when the invoice profile is not registered
    watch, source = _Watch(), _Source()
    chosen_trn = client_company_trn(upload.candidates, upload.client_company_id)

    def evidence(ctx: NodeContext) -> Sequence[Part]:
        return source.of(ctx).parts()

    async def classify(ctx: NodeContext) -> Any:
        return await intake.run(ctx, source.of(ctx))

    async def extract(ctx: NodeContext) -> Any:
        return await extraction.run(ctx, ExtractionInput(document=source.of(ctx)))

    def on_revise(v: VerdictResult, _ctx: NodeContext) -> Expand:
        feedback = tuple(Feedback(path=f.path, observed=f.observed, expected=f.expected)
                         for f in v.findings if f.source == "critic" and f.expected)

        async def revise(ctx: NodeContext) -> Any:
            return await extraction.run(ctx, ExtractionInput(document=source.of(ctx), feedback=feedback))

        node = Node(REVISE, extraction.name, "revise", StepKind.LLM, revise, depends_on=("verify.join",),
                    critical=False)
        stage2 = verifier.checks_as_nodes("verify2", output_type=ExtractionOutput, output_node=REVISE,
                                          evidence=evidence, stage=2, revisions=1)
        return Expand((node, *stage2), join="verify2.join")

    stage1 = verifier.checks_as_nodes("verify", output_type=ExtractionOutput, output_node=EXTRACT,
                                      evidence=evidence, stage=1, revisions=0, on_revise=on_revise)
    # verify.join (trigger all_done) would otherwise escalate a missing extraction on the tabular,
    # too_many_pages and not_invoice paths; emit already explains why there is no invoice.
    stage1 = tuple(replace(n, when=lambda r: EXTRACT in r) if n.id == "verify.join" else n for n in stage1)

    async def emit(ctx: NodeContext) -> documents_pb2.DocumentExtracted | TransientFailure:
        res = assemble_document(upload, ctx.run.run_id, ctx.results, watch.failures, chosen_trn, profile)
        if isinstance(res, TransientFailure):
            ctx.emit("orchestrator.retry_later", code=res.error_code)
        else:
            ctx.emit("orchestrator.emitted", invoices=str(len(res.invoices)),
                     reasons=",".join(res.review_reasons))
        return res

    nodes = [
        fetch_node(upload, fetch),
        route_node(max_pdf_pages),
        Node(INTAKE, intake.name, "classify", StepKind.LLM, classify, depends_on=("route",),
             when=_route_is("llm"), critical=False),
        Node(EXTRACT, extraction.name, "extract", StepKind.LLM, extract, depends_on=(INTAKE,), when=_is_invoice,
             critical=False),
        *stage1,
        replace(attribute_node(upload.candidates, upload.client_company_id, upload.document_id),
                when=_route_is("llm")),
        Node(IMPORT, extraction.name, "import", StepKind.DETERMINISTIC, _import, depends_on=("route",),
             when=_route_is("tabular"), critical=True),
        Node(EMIT, AGENT, "emit", StepKind.DETERMINISTIC, emit,
             depends_on=("verify.join", "intake.attribute", IMPORT), trigger="all_done", critical=False),
    ]
    return TaskGraph(WORKFLOW, [watch.node(n) for n in nodes])


# ------------------------------------------------------------------ emit (pure)
def _final_extraction(results: Mapping[str, object]) -> tuple[str, ExtractionOutput, VerdictResult] | None:
    """The extraction the final verdict is about. If that output is missing (the revision failed), the latest
    extraction that exists, with its latest stage verdict forced to escalate."""
    fv = final_verdict(results)
    if fv is not None and isinstance(out := results.get(fv.output_node), ExtractionOutput):
        return fv.output_node, out, fv.verdict
    for node_id in (REVISE, EXTRACT):
        out = results.get(node_id)
        if isinstance(out, ExtractionOutput):
            stages = [v for v in results.values() if isinstance(v, StageVerdict) and v.output_node == node_id]
            base = max(stages, key=lambda v: v.stage).verdict if stages else UNVERIFIED
            return node_id, out, replace(base, verdict="escalate")
    return None


def assemble_document(upload: documents_pb2.DocumentUploaded, run_id: str, results: Mapping[str, object],
                      failures: Mapping[str, _Failure], chosen_trn: str,
                      profile: VerifierProfile[ExtractionOutput]) -> documents_pb2.DocumentExtracted | TransientFailure:
    transient = next(((nid, f) for nid, f in failures.items() if f.transient), None)
    if transient is not None:
        return TransientFailure(transient[0], transient[1].code)
    reasons = {f.reason for f in failures.values() if f.kind is StepKind.LLM}
    ids = {"document_id": upload.document_id, "firm_id": upload.firm_id,
           "client_company_id": upload.client_company_id, "run_id": run_id}
    route = results.get("route")
    if route == "tabular":
        imp = results.get(IMPORT)
        if not isinstance(imp, ImportResult):
            raise TypeError("import.tabular has no ImportResult")
        if imp.missing_columns:
            reasons.add("import_mapping_incomplete")
        verdicts = results.get(VERIFY_ALL)
        if not (isinstance(verdicts, tuple) and len(verdicts) == len(imp.invoices)):
            verdicts = (UNVERIFIED,) * len(imp.invoices)
        invoices = [InvoiceOutcome(i.source_ordinal, i.source_ref, i.invoice,
                                   TABULAR_PROFILE.producer_confidence(_tabular_output(i.invoice)), {}, to_proto(v))
                    for i, v in zip(imp.invoices, cast("tuple[VerdictResult, ...]", verdicts), strict=True)]
        directions = {direction_of(i.invoice, chosen_trn) for i in imp.invoices}
        return build_extracted(**ids, document_kind="invoice",
                               direction=directions.pop() if len(directions) == 1 else "unknown",
                               extraction_method=imp.method, language="", invoices=invoices, review_reasons=reasons)
    if route == "too_many_pages":
        reasons.add("too_many_pages")
        return build_extracted(**ids, document_kind="invoice", direction="unknown", extraction_method="llm",
                               language="", invoices=(), review_reasons=reasons)
    if route != "llm":
        raise TypeError(f"route has no branch result: {route!r}")
    intake = results.get(INTAKE)
    intake = intake if isinstance(intake, IntakeResult) else None
    kind = intake.kind if intake else "invoice"  # unknown kind stays a reviewable document, not not_invoice
    if kind not in INVOICE_KINDS:
        reasons.add("not_invoice")
    if intake and intake.invoice_count > 1:
        reasons.add("multi_invoice_pdf")
    picked = _final_extraction(results)
    if picked is None:
        if kind in INVOICE_KINDS and not reasons:
            reasons.add("extraction_failed")
        return build_extracted(**ids, document_kind=kind, direction="unknown", extraction_method="llm",
                               language=intake.language if intake else "", invoices=(), review_reasons=reasons)
    _node, out, verdict = picked
    invoice = InvoiceOutcome(0, "", out.invoice, profile.producer_confidence(out), field_confidences(out),
                             to_proto(verdict))
    return build_extracted(**ids, document_kind=kind, direction=direction_of(out.invoice, chosen_trn),
                           extraction_method="llm", language=out.language, invoices=[invoice],
                           review_reasons=reasons)


# ------------------------------------------------------------------ finalize
def result_message(upload: documents_pb2.DocumentUploaded,
                   outcome: RunOutcome) -> documents_pb2.DocumentExtracted | documents_pb2.DocumentFailed:
    """The one message a finished run publishes; raises RetryLater instead when the run must be redelivered."""
    run_id = outcome.identity.run_id

    def failed(code: str, detail: str = "") -> documents_pb2.DocumentFailed:
        return build_failed(document_id=upload.document_id, firm_id=upload.firm_id, run_id=run_id,
                            reason_code=code, detail=detail)

    if outcome.status is RunStatus.SUCCEEDED:
        res = outcome.results.get(EMIT)
        if isinstance(res, TransientFailure):
            raise RetryLater(res.error_code)
        if isinstance(res, documents_pb2.DocumentExtracted):
            return res
        return failed("internal", "emit")
    if outcome.status is RunStatus.BUDGET_EXCEEDED:
        return _budget_stop(upload, outcome)
    code = outcome.error_code
    if code in TRANSIENT_CODES:
        raise RetryLater(code)
    if code in FETCH_CODES:
        return failed(code)
    if code in TABULAR_CODES:
        return failed("unsupported_format", code)
    return failed("internal", code)


def _budget_stop(upload: documents_pb2.DocumentUploaded, outcome: RunOutcome) -> documents_pb2.DocumentExtracted:
    """A budget stop cancels emit (contract 3.5 rule 7) but still has a result: needs_review/budget_exceeded,
    reprocessable (spec section 5.2)."""
    intake = outcome.results.get(INTAKE)
    intake = intake if isinstance(intake, IntakeResult) else None
    fetched = outcome.results.get("fetch")
    media_type = fetched.media_type if isinstance(fetched, Fetched) else ""
    kind = intake.kind if intake else "invoice"
    reasons = ["budget_exceeded"] + ([] if kind in INVOICE_KINDS else ["not_invoice"])
    return build_extracted(document_id=upload.document_id, firm_id=upload.firm_id,
                           client_company_id=upload.client_company_id, run_id=outcome.identity.run_id,
                           document_kind=kind, direction="unknown",
                           extraction_method={XLSX: "xlsx", CSV: "csv"}.get(media_type, "llm"),
                           language=intake.language if intake else "", invoices=(), review_reasons=reasons)


def build_finalize(upload: documents_pb2.DocumentUploaded, publish: PublishFn, *,
                   synthetic: bool = False) -> Callable[[RunOutcome], Awaitable[None]]:
    """GraphExecutor's finalize hook: publishes exactly one of document.extracted / document.failed before
    agent.run.finished, or raises RetryLater (nothing published) so the caller naks.

    `synthetic` (AI_GATEWAY=fake, AI_FAKE_RESULTS=review): a document.extracted that used any model response
    (a call or a cache hit) is published through `mark_synthetic`; one made without the model (a spreadsheet,
    a too-long PDF) is real and published as is."""

    async def finalize(outcome: RunOutcome) -> None:
        msg = result_message(upload, outcome)
        used_model = outcome.totals.llm_calls + outcome.totals.response_cache_hits > 0
        if synthetic and used_model and isinstance(msg, documents_pb2.DocumentExtracted):
            msg = mark_synthetic(msg)
        await publish(msg)

    return finalize
