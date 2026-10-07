"""`extraction` suite: the Extraction agent alone on 200 SyntheticInvoices (spec section 5.6; E1 measures
extraction, so Intake and the Verifier are not in the path)."""

from __future__ import annotations

import random
from collections.abc import Callable, Iterable
from pathlib import Path
from typing import ClassVar

from ai.agents.extraction import prompts
from ai.agents.extraction.agent import ExtractionAgent, ExtractionInput
from ai.agents.extraction.schema import ExtractedInvoice, ExtractionOutput, flatten
from ai.agents.source import SourceDocument
from ai.evals.core import CaseScore, DocRef, EvalCase, EvalEnv, Subset, run_node
from ai.evals.scoring import accuracy_metrics, score_invoices
from ai.gateway.types import ModelRequest
from ai.runtime.context import NodeContext
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.types import StepKind
from ai.settings import Settings
from ai.synthetic import datasets as ds
from ai.synthetic.corrupt import set_path

DEFAULT_ROOT = Path("evals/datasets")
FAKE_FIELD_CORRUPTION = 0.05  # spec section 5.6: truth with a seeded 5 % corruption


def source_of(doc: DocRef) -> SourceDocument:
    return SourceDocument.of(doc.read(), doc.media_type)  # type: ignore[arg-type]


class ExtractionSuite:
    name: ClassVar[str] = "extraction"
    prompt_ids: ClassVar[tuple[str, ...]] = (prompts.PROMPT_ID,)

    def __init__(self, settings: Settings | None = None, datasets_root: Path = DEFAULT_ROOT) -> None:
        self._settings = settings or Settings.from_env()
        self._root = datasets_root

    def cases(self, subset: Subset) -> Iterable[EvalCase[DocRef, ExtractedInvoice]]:
        truth = ds.load_truth(self._root, ds.EXTRACTION)
        for e in ds.load_manifest(self._root, ds.EXTRACTION):
            tags = frozenset(e["tags"])  # type: ignore[arg-type]
            if subset == "pr" and "subset:pr" not in tags:
                continue
            case_id = str(e["case_id"])
            doc = DocRef(self._root / ds.EXTRACTION / str(e["file"]), str(e["media_type"]), str(e["sha256"]))
            yield EvalCase(case_id, tags, doc, ExtractedInvoice.model_validate(truth[case_id]["invoice"]))

    async def run_case(self, case: EvalCase[DocRef, ExtractedInvoice], env: EvalEnv,
                       hook: CollectingHook) -> ExtractionOutput:
        agent = ExtractionAgent(model=self._settings.model_extraction)
        document = source_of(case.input)

        async def fn(ctx: NodeContext) -> ExtractionOutput:
            return await agent.run(ctx, ExtractionInput(document=document))

        return await run_node(env, hook, suite=self.name, case_id=case.case_id, agent="extraction",
                              action="extract", kind=StepKind.LLM, fn=fn)

    def score(self, case: EvalCase[DocRef, ExtractedInvoice], output: ExtractionOutput) -> CaseScore:
        acc, details = score_invoices([case.truth], [output.invoice])
        return CaseScore(case.case_id, case.tags, accuracy_metrics(acc, details), details)

    def error_score(self, case: EvalCase[DocRef, ExtractedInvoice], error: str) -> CaseScore:
        """An errored case scores 0 on every truth field (spec section 5.6)."""
        _, details = score_invoices([case.truth], [])
        counted = int(details["counted"])  # type: ignore[call-overload]
        metrics = {"field_accuracy": 0.0, "field_accuracy#counted": float(counted), "field_accuracy#correct": 0.0}
        return CaseScore(case.case_id, case.tags, metrics, {"counted": counted, "correct": 0}, error=error)

    def fake_script(self, case: EvalCase[DocRef, ExtractedInvoice]) -> Callable[[ModelRequest], ExtractionOutput]:
        """The truth with every non-empty field corrupted with probability 5 %, seeded by case id."""
        rng = random.Random(f"fake-extraction:{case.case_id}")
        inv = case.truth.model_copy(deep=True)
        for path, value in flatten(case.truth).items():
            if value and rng.random() < FAKE_FIELD_CORRUPTION:
                set_path(inv, path, value + "x")
        out = ExtractionOutput(invoice=inv, field_confidence=[], language=_language(case))
        return lambda _req: out


def _language(case: EvalCase[DocRef, ExtractedInvoice]) -> str:
    return "ar" if "lang:ar" in case.tags else "en"
