"""`verifier` suite: catch rate on seeded extraction corruptions and false-flag rate on clean extractions.

The suite forces the critic on every case (spec section 5.6): it measures the Verifier's ability to detect,
while production gates the critic with `critic_when`.
"""

from __future__ import annotations

import re
from collections.abc import Callable, Iterable
from dataclasses import dataclass, replace
from pathlib import Path
from typing import ClassVar

from ai.agents.extraction.schema import ExtractedInvoice, ExtractionOutput, flatten
from ai.agents.verifier.agent import VerifierAgent
from ai.agents.verifier.core import Finding, VerdictResult
from ai.agents.verifier.critic import CRITIC_PROMPT_ID, CriticOutput, CriticReview, InvoiceCritic
from ai.agents.verifier.invoice import invoice_profile
from ai.evals.core import CaseScore, DocRef, EvalCase, EvalEnv, Subset, pick, run_node
from ai.evals.suites.extraction import DEFAULT_ROOT, source_of
from ai.gateway.types import ModelRequest, TextPart
from ai.runtime.context import NodeContext
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.types import StepKind
from ai.settings import Settings
from ai.synthetic import datasets as ds

FAKE_MISS_ONE_IN = 10  # the fake critic fails to see about 10 % of the corruptions
FAKE_FALSE_FLAG_ONE_IN = 20  # and misreads one value in about 5 % of the clean extractions
_LISTED = re.compile(r"^- ([^:]+): ", re.MULTILINE)


@dataclass(frozen=True, slots=True)
class VerifierInput:
    doc: DocRef
    output: ExtractionOutput


@dataclass(frozen=True, slots=True)
class VerifierTruth:
    invoice: ExtractedInvoice  # what the document prints
    corrupted: bool
    corruption_path: str = ""


def always(_out: object, _findings: object) -> bool:
    return True


class VerifierSuite:
    name: ClassVar[str] = "verifier"
    prompt_ids: ClassVar[tuple[str, ...]] = (CRITIC_PROMPT_ID,)

    def __init__(self, settings: Settings | None = None, datasets_root: Path = DEFAULT_ROOT) -> None:
        self._settings = settings or Settings.from_env()
        self._root = datasets_root

    def _agent(self) -> VerifierAgent:
        profile = invoice_profile(critic=InvoiceCritic(model=self._settings.model_critic))
        return VerifierAgent([replace(profile, critic_when=always)])

    def cases(self, subset: Subset) -> Iterable[EvalCase[VerifierInput, VerifierTruth]]:
        source = ds.load_truth(self._root, ds.EXTRACTION)
        pr_sources = {str(e["case_id"]) for e in ds.load_manifest(self._root, ds.EXTRACTION)
                      if "subset:pr" in e["tags"]}  # type: ignore[operator]
        truth = ds.load_truth(self._root, ds.VERIFIER)
        for e in ds.load_manifest(self._root, ds.VERIFIER):
            case_id = str(e["case_id"])
            t = truth[case_id]
            src_id = str(t["source_case"])
            if subset == "pr" and src_id not in pr_sources:
                continue
            doc = DocRef((self._root / ds.VERIFIER / str(e["file"])).resolve(), str(e["media_type"]),
                         str(e["sha256"]))
            output = ExtractionOutput(invoice=ExtractedInvoice.model_validate(t["extraction"]),
                                      field_confidence=[], language=source[src_id]["language"])  # type: ignore[arg-type]
            corruption = t["corruption"]
            vt = VerifierTruth(ExtractedInvoice.model_validate(source[src_id]["invoice"]),
                               corruption is not None, str(corruption["path"]) if corruption else "")  # type: ignore[index]
            yield EvalCase(case_id, frozenset(e["tags"]), VerifierInput(doc, output), vt)  # type: ignore[arg-type]

    async def run_case(self, case: EvalCase[VerifierInput, VerifierTruth], env: EvalEnv,
                       hook: CollectingHook) -> VerdictResult:
        agent = self._agent()
        evidence = source_of(case.input.doc).parts()

        async def fn(ctx: NodeContext) -> VerdictResult:
            return await agent.verify(ctx, case.input.output, evidence, revisions=0)

        return await run_node(env, hook, suite=self.name, case_id=case.case_id, agent="verifier",
                              action="verify", kind=StepKind.LLM, fn=fn)

    def score(self, case: EvalCase[VerifierInput, VerifierTruth], output: VerdictResult) -> CaseScore:
        flagged = output.verdict != "accept"
        details = {"verdict": output.verdict, "confidence": output.confidence,
                   "codes": sorted({f.code for f in output.findings})}
        metric = "catch_rate" if case.truth.corrupted else "false_flag_rate"
        return CaseScore(case.case_id, case.tags, {metric: 1.0 if flagged else 0.0}, details)

    def error_score(self, case: EvalCase[VerifierInput, VerifierTruth], error: str) -> CaseScore:
        """Counts against the Verifier: an errored corruption is not caught, an errored clean case is flagged
        (production sends a case whose critic failed to a human)."""
        metric, value = ("catch_rate", 0.0) if case.truth.corrupted else ("false_flag_rate", 1.0)
        return CaseScore(case.case_id, case.tags, {metric: value}, {}, error=error)

    def fake_script(self, case: EvalCase[VerifierInput, VerifierTruth]) -> Callable[[ModelRequest], CriticOutput]:
        """A critic that reads the truth, except it misses ~10 % of the corruptions and misreads one value in
        ~5 % of the clean extractions, so a correct harness lands strictly inside the thresholds."""
        truth_flat = flatten(case.truth.invoice)
        extracted = flatten(case.input.output.invoice)
        miss = case.truth.corrupted and fake_miss(case.case_id)
        misread = not case.truth.corrupted and fake_false_flag(case.case_id)

        def critic(req: ModelRequest) -> CriticOutput:
            text = "".join(p.text for m in req.messages for p in m.parts if isinstance(p, TextPart))
            reviews: list[CriticReview] = []
            for i, path in enumerate(_LISTED.findall(text)):
                printed = extracted[path] if miss else truth_flat.get(path, "")
                if misread and i == 0:
                    printed += "x"
                reviews.append(CriticReview(path=path, document_value=printed, matches=printed == extracted[path]))
            return CriticOutput(reviews=reviews)

        return critic


def fake_miss(case_id: str) -> bool:
    return pick(case_id, "m2", FAKE_MISS_ONE_IN)


def fake_false_flag(case_id: str) -> bool:
    return pick(case_id, "f3", FAKE_FALSE_FLAG_ONE_IN)


__all__ = ["Finding", "VerifierInput", "VerifierSuite", "VerifierTruth"]
