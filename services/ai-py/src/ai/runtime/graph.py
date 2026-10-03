"""Task graph and executor (agent-runtime contract v0.2, section 3.5).

Semantics 1-11 of the contract are pinned by tests/runtime/test_graph.py.
"""

from __future__ import annotations

import asyncio
import contextlib
import logging
import random
import re
import uuid
from collections.abc import Awaitable, Callable, Iterable, Mapping, Sequence
from dataclasses import dataclass, field
from types import MappingProxyType
from typing import Literal

from opentelemetry import trace
from opentelemetry.trace import Status, StatusCode

from ai.gateway.errors import OutputInvalid, TransientModelError
from ai.gateway.types import ModelGateway, Usage
from ai.runtime.budget import BudgetMeter
from ai.runtime.clock import SYSTEM_CLOCK, Clock
from ai.runtime.context import NodeContext, NodeContextImpl, error_code_of
from ai.runtime.errors import BudgetExceeded, GraphInvalid, ToolTransientError
from ai.runtime.evalhooks import EvalHook
from ai.runtime.events import EventSink
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import (
    TERMINAL_STEP_STATUSES,
    Budget,
    GraphNodeSpec,
    RunIdentity,
    RunOutcome,
    RunStatus,
    StepKind,
    StepRecord,
    StepStatus,
)

log = logging.getLogger(__name__)
_tracer = trace.get_tracer("ai.runtime")
_NODE_ID = re.compile(r"^[a-z][a-z0-9_.#-]{0,63}$")


@dataclass(frozen=True, slots=True)
class RetryPolicy:
    max_attempts: int = 3
    base_delay_s: float = 1.0
    max_delay_s: float = 20.0
    jitter: float = 0.2  # +/-20 %
    retry_on: tuple[type[BaseException], ...] = (TransientModelError, ToolTransientError, OutputInvalid)

    def delay_s(self, attempt: int, exc: BaseException, rng: random.Random) -> float:
        retry_after = getattr(exc, "retry_after_s", None)
        if isinstance(exc, TransientModelError) and retry_after is not None:
            return min(self.max_delay_s, max(0.0, retry_after))
        base = min(self.max_delay_s, self.base_delay_s * (2 ** (attempt - 1)))
        return max(0.0, base * (1 + rng.uniform(-self.jitter, self.jitter)))


type NodeFn = Callable[[NodeContext], Awaitable[object]]


@dataclass(frozen=True, slots=True)
class Node:
    id: str  # ^[a-z][a-z0-9_.#-]{0,63}$, unique in the run
    agent: str
    action: str
    kind: StepKind
    fn: NodeFn
    depends_on: tuple[str, ...] = ()
    when: Callable[[Mapping[str, object]], bool] | None = None  # False -> SKIPPED (not a failure)
    trigger: Literal["all_succeeded", "all_done"] = "all_succeeded"
    retry: RetryPolicy = RetryPolicy()
    timeout_s: float = 120.0
    critical: bool = True  # a failed critical node fails the run
    feed_key: str = ""  # message_key emitted on success when the fn emitted none
    tools: tuple[str, ...] = ()  # ToolRegistry allowlist for this node's context

    def spec(self, depends_on: Iterable[str] | None = None) -> GraphNodeSpec:
        deps = tuple(depends_on) if depends_on is not None else self.depends_on
        return GraphNodeSpec(self.id, self.agent, self.action, self.kind, deps)


@dataclass(frozen=True, slots=True)
class Expand:
    """A node result that adds nodes to the running graph (dynamic fan-out, bounded revise loops)."""

    nodes: tuple[Node, ...]
    result: object = None  # stored as the expanding node's own result
    join: str = ""  # id of one of `nodes`; see semantics rule 10


def fan_out[T](prefix: str, items: Sequence[T], make: Callable[[str, T], Node], *, join: Node) -> Expand:
    made = tuple(make(f"{prefix}#{i}", item) for i, item in enumerate(items))
    joined = Node(**{**_node_fields(join), "depends_on": join.depends_on + tuple(n.id for n in made)})
    return Expand(nodes=(*made, joined), join=joined.id)


def _node_fields(n: Node) -> dict[str, object]:
    return {f: getattr(n, f) for f in Node.__slots__}  # type: ignore[attr-defined]


def _find_cycle(deps: Mapping[str, Iterable[str]]) -> bool:
    state: dict[str, int] = {}  # 1 = visiting, 2 = done

    def visit(n: str) -> bool:
        s = state.get(n)
        if s == 1:
            return True
        if s == 2:
            return False
        state[n] = 1
        if any(visit(d) for d in deps.get(n, ())):
            return True
        state[n] = 2
        return False

    return any(visit(n) for n in deps)


class TaskGraph:
    def __init__(self, workflow: str, nodes: Iterable[Node], *, max_nodes: int = 64,
                 max_parallel: int = 4) -> None:
        self.workflow = workflow
        self.nodes: tuple[Node, ...] = tuple(nodes)
        self.max_nodes = max_nodes
        self.max_parallel = max_parallel

    def validate(self) -> None:
        if self.max_parallel < 1:
            raise GraphInvalid(f"max_parallel {self.max_parallel} < 1")
        ids = [n.id for n in self.nodes]
        if len(ids) > self.max_nodes:
            raise GraphInvalid(f"{len(ids)} nodes > max_nodes {self.max_nodes}")
        seen: set[str] = set()
        for n in self.nodes:
            if not _NODE_ID.match(n.id):
                raise GraphInvalid(f"invalid node id {n.id!r}")
            if n.id in seen:
                raise GraphInvalid(f"duplicate node id {n.id}")
            seen.add(n.id)
        for n in self.nodes:
            for d in n.depends_on:
                if d not in seen:
                    raise GraphInvalid(f"{n.id} depends on unknown node {d}")
        if _find_cycle({n.id: n.depends_on for n in self.nodes}):
            raise GraphInvalid("cycle")

    def plan(self) -> tuple[GraphNodeSpec, ...]:
        return tuple(n.spec() for n in self.nodes)


@dataclass(slots=True)
class _Attempt:
    node_id: str
    status: StepStatus  # SUCCEEDED or FAILED
    attempt: int
    step_id: str
    result: object = None
    error: BaseException | None = None
    error_code: str = ""
    duration_ms: int = 0
    usage: Usage | None = None
    line: tuple[str, Mapping[str, str]] | None = None


@dataclass(slots=True)
class _Run:
    identity: RunIdentity
    nodes: dict[str, Node]
    deps: dict[str, tuple[str, ...]]
    status: dict[str, StepStatus | None]
    results: dict[str, object] = field(default_factory=dict)
    inflight: dict[str, tuple[int, str]] = field(default_factory=dict)  # node -> (attempt, step_id)


def step_id_for(run_id: str, node_id: str, attempt: int) -> str:
    return str(uuid.uuid5(uuid.UUID(run_id), f"{node_id}#{attempt}"))


class _Emitter:
    """Assigns seq synchronously (call order) and delivers to the sink in seq order."""

    def __init__(self, sink: EventSink, hooks: Sequence[EvalHook]) -> None:
        self._sink = sink
        self._hooks = hooks
        self._seq = 0
        self._queue: asyncio.Queue[StepRecord] = asyncio.Queue()
        self._task = asyncio.create_task(self._drain())

    def next_seq(self) -> int:
        self._seq += 1
        return self._seq

    def emit(self, rec: StepRecord) -> None:
        for h in self._hooks:
            try:
                h.on_step(rec)
            except Exception:  # hooks never fail a run
                log.exception("eval hook on_step failed")
        self._queue.put_nowait(rec)

    async def _drain(self) -> None:
        while True:
            rec = await self._queue.get()
            try:
                await self._sink.step(rec)
            except Exception:  # sink failures never fail the run (rule 9)
                log.warning("event sink step failed run_id=%s seq=%s", rec.run_id, rec.seq, exc_info=True)
            finally:
                self._queue.task_done()

    async def close(self) -> None:
        await self._queue.join()
        self._task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await self._task


class GraphExecutor:
    def __init__(self, *, gateway: ModelGateway, sink: EventSink, tools: ToolRegistry,
                 hooks: Sequence[EvalHook] = (), clock: Clock = SYSTEM_CLOCK,
                 sleep: Callable[[float], Awaitable[None]] = asyncio.sleep) -> None:
        self._gateway = gateway
        self._sink = sink
        self._tools = tools
        self._hooks = tuple(hooks)
        self._clock = clock
        self._sleep = sleep

    async def run(self, graph: TaskGraph, identity: RunIdentity, budget: Budget, *,
                  finalize: Callable[[RunOutcome], Awaitable[None]] | None = None) -> RunOutcome:
        graph.validate()
        with _tracer.start_as_current_span(f"agent.run {graph.workflow}") as span:
            span.set_attribute("compliance.run.id", identity.run_id)
            span.set_attribute("compliance.firm.id", identity.firm_id)
            span.set_attribute("compliance.subject.type", identity.subject_type)
            span.set_attribute("compliance.subject.id", identity.subject_id)
            span.set_attribute("compliance.workflow", identity.workflow)
            outcome = await self._run(graph, identity, budget, finalize)
            span.set_attribute("compliance.run.status", outcome.status.value)
            span.set_attribute("compliance.cost_micro_usd", outcome.totals.cost_micro_usd)
            span.set_attribute("compliance.llm_calls", outcome.totals.llm_calls)
            span.set_attribute("compliance.response_cache_hits", outcome.totals.response_cache_hits)
            if outcome.status is not RunStatus.SUCCEEDED:
                span.set_status(Status(StatusCode.ERROR, outcome.error_code))
            return outcome

    async def _run(self, graph: TaskGraph, identity: RunIdentity, budget: Budget,
                   finalize: Callable[[RunOutcome], Awaitable[None]] | None) -> RunOutcome:
        meter = BudgetMeter(budget, self._clock)
        await self._safe_sink(self._sink.run_started(identity, budget, graph.plan()))
        emitter = _Emitter(self._sink, self._hooks)
        st = _Run(identity=identity, nodes={n.id: n for n in graph.nodes},
                  deps={n.id: n.depends_on for n in graph.nodes},
                  status={n.id: None for n in graph.nodes})
        running: dict[asyncio.Task[_Attempt], str] = {}
        run_status, run_error = RunStatus.SUCCEEDED, ""
        try:
            while True:
                for att in self._schedule(graph, st, meter, emitter, running):
                    if st.nodes[att.node_id].critical and run_status is RunStatus.SUCCEEDED:
                        run_status, run_error = RunStatus.FAILED, att.error_code or "node_failed"
                if run_status is RunStatus.FAILED:
                    await self._cancel(st, emitter, running, "cancelled")
                    break
                if not running:
                    break  # every node is terminal (the graph is acyclic, so nothing is stuck)
                done, _ = await asyncio.wait(running, timeout=meter.remaining_s(),
                                             return_when=asyncio.FIRST_COMPLETED)
                if not done:
                    raise BudgetExceeded("deadline")
                order = list(st.nodes)
                budget_exc: BudgetExceeded | None = None
                for task in sorted(done, key=lambda t: order.index(running[t])):
                    node_id = running.pop(task)
                    st.inflight.pop(node_id, None)
                    att = task.result()
                    self._finish(graph, st, emitter, att)
                    if att.status is not StepStatus.FAILED:
                        continue
                    if isinstance(att.error, BudgetExceeded):
                        budget_exc = budget_exc or att.error
                    elif st.nodes[node_id].critical and run_status is RunStatus.SUCCEEDED:
                        run_status, run_error = RunStatus.FAILED, att.error_code or "node_failed"
                if budget_exc is not None:
                    raise budget_exc
                if run_status is RunStatus.FAILED:
                    await self._cancel(st, emitter, running, "cancelled")
                    break
        except BudgetExceeded as exc:
            run_status, run_error = RunStatus.BUDGET_EXCEEDED, f"budget_exceeded:{exc.limit}"
            await self._cancel(st, emitter, running, "cancelled")
        except asyncio.CancelledError:
            await self._cancel(st, emitter, running, "cancelled")
            await emitter.close()
            raise
        except Exception as exc:  # an executor bug: end the run, never leak in-flight nodes or the emitter
            log.exception("graph executor failed run_id=%s error=%s", identity.run_id, type(exc).__name__)
            run_status, run_error = RunStatus.FAILED, error_code_of(exc)
            await self._cancel(st, emitter, running, "cancelled")

        outcome = RunOutcome(identity=identity, status=run_status,
                             results=MappingProxyType(dict(st.results)),
                             node_status=MappingProxyType({k: v for k, v in st.status.items() if v}),
                             totals=meter.totals, error_code=run_error)
        await emitter.close()
        finalize_exc: BaseException | None = None
        if finalize is not None:
            try:
                await finalize(outcome)
            except Exception as exc:  # noqa: BLE001 - reported, then re-raised below
                finalize_exc = exc
                outcome = RunOutcome(identity=identity, status=RunStatus.FAILED, results=outcome.results,
                                     node_status=outcome.node_status, totals=outcome.totals,
                                     error_code="finalize_failed")
        for h in self._hooks:
            try:
                h.on_run_finished(outcome)
            except Exception:
                log.exception("eval hook on_run_finished failed")
        await self._safe_sink(self._sink.run_finished(outcome))
        if finalize_exc is not None:
            raise finalize_exc
        return outcome

    async def _safe_sink(self, aw: Awaitable[None]) -> None:
        try:
            await aw
        except Exception:  # rule 9
            log.warning("event sink call failed", exc_info=True)

    # ------------------------------------------------------------------ scheduling
    def _schedule(self, graph: TaskGraph, st: _Run, meter: BudgetMeter, emitter: _Emitter,
                  running: dict[asyncio.Task[_Attempt], str]) -> list[_Attempt]:
        """Starts ready nodes and skips the ones whose `when` is False. A `when` that raises fails its node
        (attempt 0, it never ran); the failures are returned, and scheduling stops at a critical one."""
        failed: list[_Attempt] = []
        progressed = True
        while progressed:  # skipping a node can make others ready
            progressed = False
            for node_id, node in st.nodes.items():
                if st.status[node_id] is not None or node_id in st.inflight:
                    continue
                deps = st.deps[node_id]
                dep_status = [st.status[d] for d in deps]
                if any(s is None or s not in TERMINAL_STEP_STATUSES for s in dep_status):
                    continue
                if node.trigger == "all_succeeded" and any(s is not StepStatus.SUCCEEDED for s in dep_status):
                    self._skip(st, emitter, node, "upstream")
                    progressed = True
                    continue
                if node.when is not None:
                    try:
                        ready = node.when(MappingProxyType(st.results))
                    except Exception as exc:  # noqa: BLE001 - the predicate's node fails, not the executor
                        log.error("when predicate failed run_id=%s node_id=%s error=%s", st.identity.run_id,
                                  node_id, type(exc).__name__)
                        att = _Attempt(node_id, StepStatus.FAILED, 0, step_id_for(st.identity.run_id, node_id, 0),
                                       error=exc, error_code=error_code_of(exc))
                        self._finish(graph, st, emitter, att)
                        failed.append(att)
                        if node.critical:
                            return failed
                        progressed = True
                        continue
                    if not ready:
                        self._skip(st, emitter, node, "")
                        progressed = True
                        continue
                if len(running) >= graph.max_parallel:
                    continue
                st.inflight[node_id] = (0, "")
                task = asyncio.create_task(self._run_node(st, node, meter, emitter))
                running[task] = node_id
        return failed

    def _skip(self, st: _Run, emitter: _Emitter, node: Node, code: str) -> None:
        st.status[node.id] = StepStatus.SKIPPED
        emitter.emit(self._record(st, emitter, node, StepStatus.SKIPPED, attempt=0,
                                  step_id=step_id_for(st.identity.run_id, node.id, 0), error_code=code))

    def _record(self, st: _Run, emitter: _Emitter, node: Node, status: StepStatus, *, attempt: int,
                step_id: str, duration_ms: int = 0, usage: Usage | None = None, error_code: str = "",
                line: tuple[str, Mapping[str, str]] | None = None) -> StepRecord:
        key, args = line if line else ("", {})
        return StepRecord(run_id=st.identity.run_id, firm_id=st.identity.firm_id, step_id=step_id,
                          seq=emitter.next_seq(), node_id=node.id, depends_on=st.deps[node.id],
                          agent=node.agent, action=node.action, kind=node.kind, status=status,
                          attempt=attempt, at=self._clock.now(), duration_ms=duration_ms, usage=usage,
                          message_key=key, message_args=dict(args), error_code=error_code)

    # ------------------------------------------------------------------ one node
    async def _run_node(self, st: _Run, node: Node, meter: BudgetMeter, emitter: _Emitter) -> _Attempt:
        rng = random.Random(f"{st.identity.run_id}:{node.id}")
        attempt = 0
        while True:
            attempt += 1
            step_id = step_id_for(st.identity.run_id, node.id, attempt)
            st.inflight[node.id] = (attempt, step_id)
            try:
                meter.check_step()
            except BudgetExceeded as exc:
                return _Attempt(node.id, StepStatus.FAILED, attempt, step_id, error=exc,
                                error_code=exc.code)
            t0 = self._clock.monotonic()

            def progress(key: str, args: Mapping[str, str], _a: int = attempt, _s: str = step_id,
                         _t0: float = t0) -> None:
                emitter.emit(self._record(st, emitter, node, StepStatus.STARTED, attempt=_a, step_id=_s,
                                          duration_ms=self._ms(_t0), line=(key, args)))

            ctx = NodeContextImpl(
                run=st.identity, node_id=node.id, step_id=step_id, attempt=attempt, budget=meter,
                tools=self._tools.scoped(node.tools), gateway=self._gateway, sink=self._sink,
                hooks=self._hooks, results=MappingProxyType(st.results), emit_progress=progress)
            emitter.emit(self._record(st, emitter, node, StepStatus.STARTED, attempt=attempt, step_id=step_id))
            with _tracer.start_as_current_span(f"agent.step {node.agent}.{node.action}") as span:
                span.set_attribute("compliance.node.id", node.id)
                span.set_attribute("compliance.step.attempt", attempt)
                span.set_attribute("compliance.step.kind", node.kind.value)
                try:
                    async with asyncio.timeout(node.timeout_s):
                        result = await node.fn(ctx)
                except BudgetExceeded as exc:
                    span.set_attribute("compliance.step.status", StepStatus.FAILED.value)
                    span.set_attribute("compliance.error_code", exc.code)
                    return _Attempt(node.id, StepStatus.FAILED, attempt, step_id, error=exc,
                                    error_code=exc.code, duration_ms=self._ms(t0), usage=ctx.usage,
                                    line=ctx.last_line)
                except Exception as exc:  # noqa: BLE001 - classified below
                    code = error_code_of(exc)
                    span.record_exception(exc)
                    span.set_attribute("compliance.error_code", code)
                    if attempt < node.retry.max_attempts and isinstance(exc, node.retry.retry_on):
                        span.set_attribute("compliance.step.status", StepStatus.RETRYING.value)
                        emitter.emit(self._record(st, emitter, node, StepStatus.RETRYING, attempt=attempt,
                                                  step_id=step_id, duration_ms=self._ms(t0), usage=ctx.usage,
                                                  error_code=code))
                        await self._sleep(node.retry.delay_s(attempt, exc, rng))
                        continue
                    span.set_attribute("compliance.step.status", StepStatus.FAILED.value)
                    span.set_status(Status(StatusCode.ERROR, code))
                    return _Attempt(node.id, StepStatus.FAILED, attempt, step_id, error=exc, error_code=code,
                                    duration_ms=self._ms(t0), usage=ctx.usage, line=ctx.last_line)
                span.set_attribute("compliance.step.status", StepStatus.SUCCEEDED.value)
                line = ctx.last_line or ((node.feed_key, {}) if node.feed_key else None)
                return _Attempt(node.id, StepStatus.SUCCEEDED, attempt, step_id, result=result,
                                duration_ms=self._ms(t0), usage=ctx.usage, line=line)

    def _ms(self, t0: float) -> int:
        return int((self._clock.monotonic() - t0) * 1000)

    # ------------------------------------------------------------------ completion
    def _finish(self, graph: TaskGraph, st: _Run, emitter: _Emitter, att: _Attempt) -> None:
        node = st.nodes[att.node_id]
        if att.status is StepStatus.SUCCEEDED and isinstance(att.result, Expand):
            try:
                self._expand(graph, st, node, att.result)
            except GraphInvalid as exc:
                att.status, att.error, att.error_code = StepStatus.FAILED, exc, exc.code
            else:
                att.result = att.result.result
        if att.status is StepStatus.SUCCEEDED:
            st.results[node.id] = att.result
        st.status[node.id] = att.status
        emitter.emit(self._record(st, emitter, node, att.status, attempt=att.attempt, step_id=att.step_id,
                                  duration_ms=att.duration_ms, usage=att.usage, error_code=att.error_code,
                                  line=att.line))

    def _expand(self, graph: TaskGraph, st: _Run, parent: Node, exp: Expand) -> None:
        new_ids = [n.id for n in exp.nodes]
        if len(st.nodes) + len(new_ids) > graph.max_nodes:
            raise GraphInvalid("expansion exceeds max_nodes")
        if len(set(new_ids)) != len(new_ids) or any(i in st.nodes for i in new_ids):
            raise GraphInvalid("expansion reuses a node id")
        if any(not _NODE_ID.match(i) for i in new_ids):
            raise GraphInvalid("expansion has an invalid node id")
        known = set(st.nodes) | set(new_ids)
        for n in exp.nodes:
            if any(d not in known for d in n.depends_on):
                raise GraphInvalid(f"{n.id} depends on an unknown node")
        if exp.join and exp.join not in new_ids:
            raise GraphInvalid("expansion join is not one of its nodes")
        deps = dict(st.deps)
        for n in exp.nodes:
            deps[n.id] = n.depends_on
        if exp.join:
            for other, other_deps in st.deps.items():
                if parent.id in other_deps and st.status[other] is None:
                    deps[other] = other_deps + (exp.join,)
        if _find_cycle(deps):
            raise GraphInvalid("expansion creates a cycle")
        for n in exp.nodes:
            st.nodes[n.id] = n
            st.status[n.id] = None
        st.deps = deps

    async def _cancel(self, st: _Run, emitter: _Emitter, running: dict[asyncio.Task[_Attempt], str],
                      code: str) -> None:
        for task in running:
            task.cancel()
        if running:
            await asyncio.gather(*running, return_exceptions=True)
        for task, node_id in list(running.items()):
            attempt, step_id = st.inflight.pop(node_id, (1, step_id_for(st.identity.run_id, node_id, 1)))
            st.status[node_id] = StepStatus.FAILED
            emitter.emit(self._record(st, emitter, st.nodes[node_id], StepStatus.FAILED, attempt=attempt,
                                      step_id=step_id, error_code=code))
        running.clear()
        for node_id, node in st.nodes.items():
            if st.status[node_id] is None:
                st.status[node_id] = StepStatus.SKIPPED
                emitter.emit(self._record(st, emitter, node, StepStatus.SKIPPED, attempt=0,
                                          step_id=step_id_for(st.identity.run_id, node_id, 0),
                                          error_code=code))
