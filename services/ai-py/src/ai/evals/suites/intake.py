"""`intake` suite: document kind accuracy over the 200 extraction documents and 20 non-invoice pages."""

from __future__ import annotations

import re
from collections.abc import Callable, Iterable, Mapping
from pathlib import Path
from typing import ClassVar

from ai.agents.intake.agent import PROMPT_ID, IntakeAgent
from ai.agents.intake.schema import IntakeResult
from ai.evals.core import CaseScore, DocRef, EvalCase, EvalEnv, Subset, pick, run_node
from ai.evals.suites.extraction import DEFAULT_ROOT, source_of
from ai.gateway.types import ModelRequest
from ai.runtime.context import NodeContext
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.types import StepKind
from ai.settings import Settings
from ai.synthetic import datasets as ds

# Salts below are fixed by hand so the fake scores land strictly inside the thresholds on both the full and the
# pr subsets of the committed datasets (tests/evals/suites assert it); changing one needs those tests re-checked.
FAKE_WRONG_KIND_ONE_IN = 25  # about 4 % of the cases get a wrong kind, so the fake score sits above 0.95
PR_LETTERS_PER_LANGUAGE = 2

type IntakeTruth = Mapping[str, str]  # {"kind": ..., "language": ...}


class IntakeSuite:
    name: ClassVar[str] = "intake"
    prompt_ids: ClassVar[tuple[str, ...]] = (PROMPT_ID,)

    def __init__(self, settings: Settings | None = None, datasets_root: Path = DEFAULT_ROOT) -> None:
        self._settings = settings or Settings.from_env()
        self._root = datasets_root

    def cases(self, subset: Subset) -> Iterable[EvalCase[DocRef, IntakeTruth]]:
        for dataset in (ds.EXTRACTION, ds.INTAKE_EXTRA):
            truth = ds.load_truth(self._root, dataset)
            for e in ds.load_manifest(self._root, dataset):
                tags = frozenset(e["tags"])  # type: ignore[arg-type]
                case_id = str(e["case_id"])
                if subset == "pr" and not _in_pr(dataset, case_id, tags):
                    continue
                doc = DocRef(self._root / dataset / str(e["file"]), str(e["media_type"]), str(e["sha256"]))
                t = truth[case_id]
                yield EvalCase(case_id, tags, doc, {"kind": str(t["kind"]), "language": str(t["language"])})

    async def run_case(self, case: EvalCase[DocRef, IntakeTruth], env: EvalEnv,
                       hook: CollectingHook) -> IntakeResult:
        agent = IntakeAgent(model=self._settings.model_intake)
        document = source_of(case.input)

        async def fn(ctx: NodeContext) -> IntakeResult:
            return await agent.run(ctx, document)

        return await run_node(env, hook, suite=self.name, case_id=case.case_id, agent="intake",
                              action="classify", kind=StepKind.LLM, fn=fn)

    def score(self, case: EvalCase[DocRef, IntakeTruth], output: IntakeResult) -> CaseScore:
        ok = output.kind == case.truth["kind"]
        return CaseScore(case.case_id, case.tags, {"kind_accuracy": 1.0 if ok else 0.0},
                         {"kind_ok": ok})

    def error_score(self, case: EvalCase[DocRef, IntakeTruth], error: str) -> CaseScore:
        return CaseScore(case.case_id, case.tags, {"kind_accuracy": 0.0}, {"kind_ok": False}, error=error)

    def fake_script(self, case: EvalCase[DocRef, IntakeTruth]) -> Callable[[ModelRequest], IntakeResult]:
        truth_kind = case.truth["kind"]
        kind = truth_kind
        if fake_wrong_kind(case.case_id):
            kind = "other" if truth_kind != "other" else "invoice"
        out = IntakeResult(kind=kind, language=case.truth["language"],  # type: ignore[arg-type]
                           invoice_count=1 if truth_kind in ("invoice", "credit_note") else 0,
                           seller_trn="", buyer_trn="", confidence=0.97)
        return lambda _req: out


def fake_wrong_kind(case_id: str) -> bool:
    return pick(case_id, "intake-c", FAKE_WRONG_KIND_ONE_IN)


def _in_pr(dataset: str, case_id: str, tags: frozenset[str]) -> bool:
    if dataset == ds.EXTRACTION:
        return "subset:pr" in tags
    m = re.search(r"-(\d+)$", case_id)
    return bool(m) and int(m.group(1)) < PR_LETTERS_PER_LANGUAGE
