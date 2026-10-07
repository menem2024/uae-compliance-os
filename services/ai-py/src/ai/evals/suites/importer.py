"""`importer` suite: the deterministic XLSX/CSV importer on 40 files, exact match, no model (AC-6)."""

from __future__ import annotations

from collections.abc import Callable, Iterable
from pathlib import Path
from typing import ClassVar, NoReturn

from ai.agents.extraction.schema import ExtractedInvoice
from ai.agents.extraction.tabular.importer import ImportResult, TabularFormat, import_tabular
from ai.evals.core import CaseScore, DocRef, EvalCase, EvalEnv, Subset, run_node
from ai.evals.scoring import accuracy_metrics, score_invoices
from ai.evals.suites.extraction import DEFAULT_ROOT
from ai.gateway.types import ModelRequest
from ai.runtime.context import NodeContext
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.types import StepKind
from ai.synthetic import datasets as ds

PR_PER_FORMAT = 5


class ImporterSuite:
    name: ClassVar[str] = "importer"
    prompt_ids: ClassVar[tuple[str, ...]] = ()  # never calls a model

    def __init__(self, datasets_root: Path = DEFAULT_ROOT) -> None:
        self._root = datasets_root

    def cases(self, subset: Subset) -> Iterable[EvalCase[DocRef, tuple[ExtractedInvoice, ...]]]:
        truth = ds.load_truth(self._root, ds.IMPORTER)
        seen: dict[str, int] = {}
        for e in ds.load_manifest(self._root, ds.IMPORTER):
            tags = frozenset(e["tags"])  # type: ignore[arg-type]
            fmt = "csv" if "format:csv" in tags else "xlsx"
            seen[fmt] = n = seen.get(fmt, 0) + 1
            if subset == "pr" and n > PR_PER_FORMAT:
                continue
            case_id = str(e["case_id"])
            doc = DocRef(self._root / ds.IMPORTER / str(e["file"]), str(e["media_type"]), str(e["sha256"]))
            invoices = tuple(ExtractedInvoice.model_validate(i) for i in truth[case_id]["invoices"])  # type: ignore[attr-defined]
            yield EvalCase(case_id, tags, doc, invoices)

    async def run_case(self, case: EvalCase[DocRef, tuple[ExtractedInvoice, ...]], env: EvalEnv,
                       hook: CollectingHook) -> ImportResult:
        data = case.input.read()
        fmt: TabularFormat = "csv" if case.input.media_type == "text/csv" else "xlsx"

        async def fn(_ctx: NodeContext) -> ImportResult:
            return import_tabular(data, fmt)

        return await run_node(env, hook, suite=self.name, case_id=case.case_id, agent="importer",
                              action="import", kind=StepKind.DETERMINISTIC, fn=fn)

    def score(self, case: EvalCase[DocRef, tuple[ExtractedInvoice, ...]], output: ImportResult) -> CaseScore:
        acc, details = score_invoices(case.truth, [i.invoice for i in output.invoices])
        return CaseScore(case.case_id, case.tags, accuracy_metrics(acc, details), details)

    def error_score(self, case: EvalCase[DocRef, tuple[ExtractedInvoice, ...]], error: str) -> CaseScore:
        _, details = score_invoices(case.truth, [])
        counted = int(details["counted"])  # type: ignore[call-overload]
        metrics = {"field_accuracy": 0.0, "field_accuracy#counted": float(counted), "field_accuracy#correct": 0.0}
        return CaseScore(case.case_id, case.tags, metrics, {"counted": counted, "correct": 0}, error=error)

    def fake_script(self, case: EvalCase[DocRef, tuple[ExtractedInvoice, ...]]) -> Callable[[ModelRequest], NoReturn]:
        """Declared so fake mode runs this suite (a suite without one is reported "skipped"); it raises if the
        gateway is ever called, which proves the importer's "no model" assumption."""

        def never(_req: ModelRequest) -> NoReturn:
            raise AssertionError("the importer suite must never call the model gateway")

        return never
