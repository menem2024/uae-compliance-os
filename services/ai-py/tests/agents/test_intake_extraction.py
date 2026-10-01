"""Intake and Extraction agents with FakeGateway: request shape (contract section 1), post-processing and feed
lines. No real model is ever called (AC-4)."""

import tomllib
import uuid
from pathlib import Path

import pytest

from ai.agents.extraction.agent import ExtractionAgent, ExtractionInput, Feedback, clean_output
from ai.agents.extraction.plugin import register as register_extraction
from ai.agents.extraction.schema import ExtractionOutput, FieldConfidence
from ai.agents.intake.agent import IntakeAgent
from ai.agents.intake.plugin import register as register_intake
from ai.agents.intake.schema import IntakeResult
from ai.agents.source import SourceDocument
from ai.gateway.fake import FakeGateway
from ai.gateway.keys import cache_key
from ai.gateway.types import HAIKU, OPUS, SONNET, DocumentPart, ImagePart, TextPart
from ai.runtime.clock import FakeClock
from ai.runtime.events import MemorySink
from ai.runtime.graph import GraphExecutor, Node, TaskGraph
from ai.runtime.registry import load_registry
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import Budget, RunIdentity, StepKind
from ai.synthetic.generator import generate

PDF = SourceDocument.of(b"%PDF-1.7 synthetic", "application/pdf")
JPEG = SourceDocument.of(b"\xff\xd8\xff synthetic", "image/jpeg")
RUN = RunIdentity(str(uuid.uuid4()), "firm-1", "cc-1", "t@1", "document", "doc-1")
PYPROJECT = Path(__file__).resolve().parents[2] / "pyproject.toml"


async def run_node(fn, gateway):
    sink = MemorySink()
    ex = GraphExecutor(gateway=gateway, sink=sink, tools=ToolRegistry(), clock=FakeClock())
    out = await ex.run(TaskGraph("t@1", [Node("a", "x", "y", StepKind.LLM, fn)]), RUN, Budget())
    return out, sink


def test_source_document_parts_and_integrity():
    assert isinstance(PDF.parts()[0], DocumentPart) and PDF.parts()[0].sha256 == PDF.sha256
    (img,) = JPEG.parts()
    assert isinstance(img, ImagePart) and img.media_type == "image/jpeg"
    with pytest.raises(ValueError, match="sha256"):
        SourceDocument(data=b"x", media_type="application/pdf", sha256="0" * 64)


def test_intake_request_shape():
    req = IntakeAgent().build_request(PDF)
    assert (req.model, req.prompt_id, req.prompt_version, req.max_tokens, req.effort) == (
        HAIKU, "intake.classify", 1, 1024, None)
    assert req.output_model is IntakeResult and req.cacheable
    (msg,) = req.messages
    assert isinstance(msg.parts[0], DocumentPart) and isinstance(msg.parts[-1], TextPart)


async def test_intake_normalises_and_reports():
    raw = IntakeResult(kind="invoice", language="ar", invoice_count=-1, seller_trn="١٠٠ ٢٣٤ ٥٦٧ ٨٩٠ ١٢٣",
                       buyer_trn="", confidence=1.4)
    gw = FakeGateway({"intake.classify": [raw]})

    async def fn(ctx):
        return await IntakeAgent().run(ctx, JPEG)

    out, sink = await run_node(fn, gw)
    got = out.results["a"]
    assert (got.seller_trn, got.invoice_count, got.confidence) == ("100234567890123", 0, 1.0)
    assert isinstance(gw.calls[0].messages[0].parts[0], ImagePart)
    line = next(r for r in sink.steps if r.message_key == "intake.classified")
    assert dict(line.message_args) == {"kind": "invoice", "language": "ar", "count": "0"}


def test_extraction_request_shape_and_revision_key():
    agent = ExtractionAgent()
    first = agent.build_request(ExtractionInput(document=PDF))
    assert (first.model, first.prompt_id, first.prompt_version, first.max_tokens, first.effort) == (
        SONNET, "extraction.invoice", 1, 8192, "low")
    assert first.output_model is ExtractionOutput
    revise = agent.build_request(ExtractionInput(document=PDF, feedback=(
        Feedback(path="total_amount", observed="1234.00", expected="1243.00"),)))
    assert revise.prompt_id == "extraction.revise"
    assert '- total_amount: "1234.00" -> "1243.00"' in revise.messages[0].parts[-1].text
    assert cache_key(first) != cache_key(revise)
    # Contract rule 9: only the document and static text; the same bytes give the same key for any client.
    assert cache_key(agent.build_request(ExtractionInput(document=PDF))) == cache_key(first)


def test_clean_output_normalises_and_filters_confidences():
    truth = generate(21, "en", defect_rate=0.0).truth
    messy = truth.model_copy(deep=True)
    messy.total_amount = f"AED {int(float(truth.total_amount)):,}.{truth.total_amount.split('.')[1]}"
    messy.issue_date = "/".join(reversed(truth.issue_date.split("-")))
    raw = ExtractionOutput(invoice=messy, language="en", field_confidence=[
        FieldConfidence(path="total_amount", confidence=0.4), FieldConfidence(path="lines[99].quantity", confidence=0.1),
        FieldConfidence(path="invoice_number", confidence=7.0), FieldConfidence(path="total_amount", confidence=0.9)])
    out = clean_output(raw)
    assert out.invoice == truth
    assert [(f.path, f.confidence) for f in out.field_confidence] == [("invoice_number", 1.0), ("total_amount", 0.9)]


async def test_extraction_run_emits_counts():
    truth = generate(22, "ar", defect_rate=0.0).truth
    gw = FakeGateway({"extraction.invoice": [ExtractionOutput(invoice=truth, field_confidence=[], language="ar")]})

    async def fn(ctx):
        return await ExtractionAgent(model=OPUS).run(ctx, ExtractionInput(document=PDF))

    out, sink = await run_node(fn, gw)
    assert out.results["a"].invoice == truth and gw.calls[0].model == OPUS
    line = next(r for r in sink.steps if r.message_key == "extraction.extracted")
    assert line.message_args["lines"] == str(len(truth.lines)) and int(line.message_args["fields"]) > 10


def test_plugins_register_from_settings(monkeypatch):
    monkeypatch.setenv("AI_MODEL_EXTRACTION", OPUS)
    monkeypatch.delenv("AI_MODEL_INTAKE", raising=False)

    class EP:
        def __init__(self, name, fn):
            self.name, self._fn = name, fn

        def load(self):
            return self._fn

    reg = load_registry(eps=[EP("intake", register_intake), EP("extraction", register_extraction)])
    assert reg.agents["intake"].model == HAIKU and reg.agents["extraction"].model == OPUS


def test_entry_points_are_declared():
    eps = tomllib.loads(PYPROJECT.read_text())["project"]["entry-points"]["compliance.agents"]
    assert eps["intake"] == "ai.agents.intake.plugin:register"
    assert eps["extraction"] == "ai.agents.extraction.plugin:register"
