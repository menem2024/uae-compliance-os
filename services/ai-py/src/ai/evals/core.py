"""Eval harness core: the contract section 3.7 types and the one-node graph every suite runs through.

Every suite case runs through `GraphExecutor`, even a one-node graph, so an eval run sees exactly the budget,
tracing and hook wiring production sees (plan Task 25, binding decision).
"""

from __future__ import annotations

import hashlib
import uuid
from collections.abc import Awaitable, Callable, Iterable, Mapping
from dataclasses import dataclass, field
from pathlib import Path
from typing import ClassVar, Literal, Protocol, cast

from ai.gateway.types import ModelGateway
from ai.runtime.context import NodeContext
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.events import MemorySink
from ai.runtime.graph import GraphExecutor, Node, RetryPolicy, TaskGraph
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import Budget, RunIdentity, RunStatus, RunTotals, StepKind

EVAL_FIRM_ID = "00000000-0000-4000-8000-00000000e001"
NODE_ID = "run"
_RUN_NS = uuid.UUID("7d0c4b0e-4f6a-4c0f-9a53-2f1d6a0e5e01")

type Mode = Literal["fake", "replay", "live"]
type Subset = Literal["pr", "full"]


@dataclass(frozen=True, slots=True)
class EvalCase[InT, TruthT]:
    case_id: str
    tags: frozenset[str]  # e.g. {"lang:ar", "format:pdf"}
    input: InT
    truth: TruthT


@dataclass(frozen=True, slots=True)
class CaseScore:
    case_id: str
    tags: frozenset[str]
    metrics: Mapping[str, float]  # e.g. {"field_accuracy": 0.95}; "<m>#counted"/"<m>#correct" make it micro-averaged
    details: Mapping[str, object]  # per-field outcome; no PII (synthetic data only)
    usage: RunTotals = field(default_factory=RunTotals)
    error: str = ""


@dataclass(frozen=True, slots=True)
class EvalEnv:
    gateway: ModelGateway
    mode: Mode
    concurrency: int = 4


class Suite[InT, OutT, TruthT](Protocol):
    name: ClassVar[str]
    prompt_ids: ClassVar[tuple[str, ...]]  # prompt ids the suite calls; used for recording manifests

    def cases(self, subset: Subset) -> Iterable[EvalCase[InT, TruthT]]: ...
    async def run_case(self, case: EvalCase[InT, TruthT], env: EvalEnv, hook: CollectingHook) -> OutT: ...
    def score(self, case: EvalCase[InT, TruthT], output: OutT) -> CaseScore: ...
    # Optional (contract 0.2): `fake_script(case) -> Callable[[ModelRequest], ModelResponse | BaseModel | Exception]`.
    # The runner builds FakeGateway(suite.fake_script(case)) per case in `fake` mode; a suite without it is
    # reported "skipped" in fake mode, never "passed".
    # Optional: `error_score(case, error_code) -> CaseScore`, the score of a case that errored (spec 5.6: it
    # scores 0 on all its truth fields); the runner falls back to an empty-metrics score without it.


class CaseFailed(Exception):
    """The case's graph run did not succeed. `code` is the failed node's error code (never model output)."""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


class DatasetStale(Exception):
    """An eval binary is missing or differs from the committed manifest hash. Replay keys depend on the exact
    bytes, so this stops the run instead of scoring against the wrong document."""


@dataclass(frozen=True, slots=True)
class DocRef:
    """A dataset binary, read lazily and verified against the committed sha256."""

    path: Path
    media_type: str
    sha256: str

    def read(self) -> bytes:
        try:
            data = self.path.read_bytes()
        except FileNotFoundError:
            raise DatasetStale(
                f"{self.path.name} is missing: run `uv run python -m ai.synthetic build` in services/ai-py"
            ) from None
        if hashlib.sha256(data).hexdigest() != self.sha256:
            raise DatasetStale(
                f"{self.path.name} differs from its manifest sha256: regenerate it with "
                "`uv run python -m ai.synthetic build`")
        return data


def pick(case_id: str, salt: str, one_in: int) -> bool:
    """Deterministic 1-in-`one_in` selection by case id, for the fake scripts' seeded errors."""
    return int(hashlib.sha256(f"{salt}:{case_id}".encode()).hexdigest()[:8], 16) % one_in == 0


def run_policy(env: EvalEnv) -> RetryPolicy:
    """Production retry policy in live mode; immediate retries when nothing external can be rate limited."""
    if env.mode == "live":
        return RetryPolicy()
    return RetryPolicy(base_delay_s=0.0, max_delay_s=0.0, jitter=0.0)


async def run_node[T](env: EvalEnv, hook: CollectingHook, *, suite: str, case_id: str, agent: str, action: str,
                      kind: StepKind, fn: Callable[[NodeContext], Awaitable[T]],
                      budget: Budget | None = None) -> T:
    """Runs one node through GraphExecutor with the case's hook; raises CaseFailed unless it succeeded."""
    node = Node(NODE_ID, agent, action, kind, fn, retry=run_policy(env))
    workflow = f"eval.{suite}@1"
    identity = RunIdentity(run_id=str(uuid.uuid5(_RUN_NS, f"{suite}:{case_id}")), firm_id=EVAL_FIRM_ID,
                           client_company_id="", workflow=workflow, subject_type="document", subject_id=case_id)
    executor = GraphExecutor(gateway=env.gateway, sink=MemorySink(), tools=ToolRegistry(), hooks=(hook,))
    outcome = await executor.run(TaskGraph(workflow, [node]), identity, budget or Budget())
    if outcome.status is not RunStatus.SUCCEEDED or NODE_ID not in outcome.results:
        raise CaseFailed(outcome.error_code or "run_failed")
    return cast("T", outcome.results[NODE_ID])
