"""Event sinks (contract section 3.6). NatsSink lives in ai.runtime.nats_sink."""

from typing import Protocol

from ai.runtime.proposals import ProposalDraft
from ai.runtime.types import Budget, GraphNodeSpec, RunIdentity, RunOutcome, StepRecord


class EventSink(Protocol):
    async def run_started(self, identity: RunIdentity, budget: Budget,
                          plan: tuple[GraphNodeSpec, ...]) -> None: ...
    async def step(self, rec: StepRecord) -> None: ...
    async def run_finished(self, outcome: RunOutcome) -> None: ...
    async def proposal(self, identity: RunIdentity, draft: ProposalDraft, proposal_id: str) -> None: ...


class MemorySink:
    """Test sink: keeps everything in lists, in arrival order."""

    def __init__(self) -> None:
        self.started: list[tuple[RunIdentity, Budget, tuple[GraphNodeSpec, ...]]] = []
        self.steps: list[StepRecord] = []
        self.finished: list[RunOutcome] = []
        self.proposals: list[tuple[RunIdentity, ProposalDraft, str]] = []

    async def run_started(self, identity: RunIdentity, budget: Budget,
                          plan: tuple[GraphNodeSpec, ...]) -> None:
        self.started.append((identity, budget, plan))

    async def step(self, rec: StepRecord) -> None:
        self.steps.append(rec)

    async def run_finished(self, outcome: RunOutcome) -> None:
        self.finished.append(outcome)

    async def proposal(self, identity: RunIdentity, draft: ProposalDraft, proposal_id: str) -> None:
        self.proposals.append((identity, draft, proposal_id))

    def statuses(self, node_id: str) -> list[str]:
        return [r.status.value for r in self.steps if r.node_id == node_id]
