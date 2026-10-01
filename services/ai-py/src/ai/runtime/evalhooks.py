"""Eval hooks (contract section 3.7)."""

from typing import Protocol

from ai.gateway.types import ModelRequest, ModelResponse
from ai.runtime.types import RunOutcome, StepRecord


class EvalHook(Protocol):
    def on_step(self, rec: StepRecord) -> None: ...
    def on_model_call(self, req: ModelRequest, resp: ModelResponse) -> None: ...
    def on_run_finished(self, outcome: RunOutcome) -> None: ...


class CollectingHook:
    def __init__(self) -> None:
        self.steps: list[StepRecord] = []
        self.calls: list[tuple[ModelRequest, ModelResponse]] = []
        self.outcome: RunOutcome | None = None

    def on_step(self, rec: StepRecord) -> None:
        self.steps.append(rec)

    def on_model_call(self, req: ModelRequest, resp: ModelResponse) -> None:
        self.calls.append((req, resp))

    def on_run_finished(self, outcome: RunOutcome) -> None:
        self.outcome = outcome
