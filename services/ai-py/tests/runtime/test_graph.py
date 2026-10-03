"""Pins GraphExecutor semantics 1-11 (agent-runtime contract v0.2, section 3.5)."""

import asyncio
import uuid

import pytest
from pydantic import BaseModel

from ai.gateway.errors import TransientModelError
from ai.gateway.fake import FakeGateway
from ai.gateway.types import HAIKU, Message, ModelRequest, TextPart
from ai.runtime.clock import FakeClock
from ai.runtime.errors import GraphInvalid
from ai.runtime.events import MemorySink
from ai.runtime.graph import Expand, GraphExecutor, Node, RetryPolicy, TaskGraph, fan_out, step_id_for
from ai.runtime.proposals import ProposalDraft
from ai.runtime.tools import ToolRegistry
from ai.runtime.types import Budget, RunIdentity, RunStatus, StepKind, StepStatus

RUN = RunIdentity(run_id=str(uuid.uuid4()), firm_id="f1", client_company_id="", workflow="t@1",
                  subject_type="document", subject_id="d1")
D = StepKind.DETERMINISTIC


def ok(value=None):
    async def fn(ctx):
        return value

    return fn


def boom(exc):
    async def fn(ctx):
        raise exc

    return fn


async def no_sleep(_s: float) -> None:
    return None


def executor(sink, gateway=None, **kw):
    return GraphExecutor(gateway=gateway or FakeGateway({}), sink=sink, tools=ToolRegistry(),
                         clock=kw.pop("clock", FakeClock()), sleep=kw.pop("sleep", no_sleep), **kw)


DEFAULT_BUDGET = Budget()


async def run(nodes, *, budget=DEFAULT_BUDGET, gateway=None, max_parallel=4, **kw):
    sink = MemorySink()
    out = await executor(sink, gateway, **kw).run(TaskGraph("t@1", nodes, max_parallel=max_parallel), RUN, budget)
    return out, sink


async def test_1_parallel_ready_set_bounded_by_max_parallel():
    live, peak = 0, 0

    def slow(v):
        async def fn(ctx):
            nonlocal live, peak
            live += 1
            peak = max(peak, live)
            await asyncio.sleep(0.01)
            live -= 1
            return v

        return fn

    nodes = [Node(f"n{i}", "a", "x", D, slow(i)) for i in range(5)]
    out, _ = await run(nodes, max_parallel=2)
    assert out.status is RunStatus.SUCCEEDED
    assert peak == 2
    assert [out.results[f"n{i}"] for i in range(5)] == [0, 1, 2, 3, 4]


async def test_2_all_succeeded_skips_on_failed_dep_all_done_runs():
    nodes = [
        Node("a", "a", "x", D, boom(ValueError("x")), critical=False),
        Node("b", "a", "x", D, ok(1), depends_on=("a",)),
        Node("j", "a", "x", D, ok(2), depends_on=("a", "b"), trigger="all_done"),
    ]
    out, sink = await run(nodes)
    assert out.status is RunStatus.SUCCEEDED
    assert out.node_status == {"a": StepStatus.FAILED, "b": StepStatus.SKIPPED, "j": StepStatus.SUCCEEDED}
    skipped = [r for r in sink.steps if r.node_id == "b"][-1]
    assert skipped.error_code == "upstream" and skipped.attempt == 0
    assert skipped.step_id == step_id_for(RUN.run_id, "b", 0)


async def test_3_when_false_skips_without_failure():
    nodes = [Node("a", "a", "x", D, ok(1)),
             Node("b", "a", "x", D, ok(2), depends_on=("a",), when=lambda r: r["a"] == 99)]
    out, _ = await run(nodes)
    assert out.status is RunStatus.SUCCEEDED and out.node_status["b"] is StepStatus.SKIPPED


async def test_4_retry_emits_started_retrying_then_final():
    calls = 0

    async def flaky(ctx):
        nonlocal calls
        calls += 1
        if calls < 3:
            raise TransientModelError("429", retry_after_s=3)
        return "ok"

    delays: list[float] = []

    async def sleep(s):
        delays.append(s)

    out, sink = await run([Node("a", "a", "x", D, flaky)], sleep=sleep)
    assert out.results["a"] == "ok"
    assert sink.statuses("a") == ["started", "retrying", "started", "retrying", "started", "succeeded"]
    assert delays == [3, 3]  # retry_after overrides the computed delay
    assert len({r.step_id for r in sink.steps if r.attempt == 1}) == 1


async def test_4b_non_retryable_fails_first_time():
    _out, sink = await run([Node("a", "a", "x", D, boom(ValueError("x")), critical=False)])
    assert sink.statuses("a") == ["started", "failed"]
    assert [r.error_code for r in sink.steps][-1] == "internal"


async def test_5_timeout_not_retried_by_default():
    async def hang(ctx):
        await asyncio.sleep(10)

    _out, sink = await run([Node("a", "a", "x", D, hang, timeout_s=0.01, critical=False)])
    assert sink.statuses("a") == ["started", "failed"]
    assert sink.steps[-1].error_code == "timeout"
    pol = RetryPolicy(retry_on=(TimeoutError,), max_attempts=2)
    _out, sink = await run([Node("a", "a", "x", D, hang, timeout_s=0.01, critical=False, retry=pol)])
    assert sink.statuses("a") == ["started", "retrying", "started", "failed"]


async def test_6_expand_adds_nodes_and_validates():
    async def expander(ctx):
        return Expand(nodes=(Node("c", "a", "x", D, ok(3), depends_on=("a",)),), result="exp")

    nodes = [Node("a", "a", "x", D, expander)]
    out, _ = await run(nodes)
    assert out.results == {"a": "exp", "c": 3}

    async def bad(ctx):
        return Expand(nodes=(Node("c", "a", "x", D, ok(), depends_on=("nope",)),))

    out, sink = await run([Node("a", "a", "x", D, bad, critical=False)])
    assert out.node_status["a"] is StepStatus.FAILED and sink.steps[-1].error_code == "graph_invalid"


async def test_7_critical_failure_cancels_inflight_and_pending():
    async def slow(ctx):
        await asyncio.sleep(10)

    nodes = [Node("a", "a", "x", D, boom(ValueError("x"))),
             Node("s", "a", "x", D, slow),
             Node("p", "a", "x", D, ok(), depends_on=("s",))]
    out, sink = await run(nodes)
    assert out.status is RunStatus.FAILED and out.error_code == "internal"
    last = {r.node_id: r for r in sink.steps}
    assert last["s"].status is StepStatus.FAILED and last["s"].error_code == "cancelled"
    assert last["p"].status is StepStatus.SKIPPED and last["p"].error_code == "cancelled"


async def test_7b_budget_exceeded_ends_run():
    class Out(BaseModel):
        v: str = ""

    gw = FakeGateway({"t.p": [Out(v="1")]})

    async def call(ctx):
        req = ModelRequest(model=HAIKU, prompt_id="t.p", prompt_version=1, system="s",
                           messages=(Message("user", (TextPart("x"),)),), output_model=Out)
        await ctx.complete(req)
        await ctx.complete(req)

    out, _ = await run([Node("a", "a", "x", StepKind.LLM, call)], budget=Budget(max_llm_calls=1), gateway=gw)
    assert out.status is RunStatus.BUDGET_EXCEEDED and out.error_code == "budget_exceeded:llm_calls"
    assert out.totals.llm_calls == 1


async def test_7c_deadline():
    async def slow(ctx):
        await asyncio.sleep(10)

    from ai.runtime.clock import SYSTEM_CLOCK

    out, _ = await run([Node("a", "a", "x", D, slow)], budget=Budget(deadline_s=0.05), clock=SYSTEM_CLOCK)
    assert out.status is RunStatus.BUDGET_EXCEEDED and out.error_code == "budget_exceeded:deadline"


async def test_8_non_critical_failure_run_succeeds():
    out, _ = await run([Node("a", "a", "x", D, boom(ValueError()), critical=False), Node("b", "a", "x", D, ok())])
    assert out.status is RunStatus.SUCCEEDED


async def test_9_event_order_and_seq():
    nodes = [Node("a", "a", "x", D, ok(1)), Node("b", "a", "x", D, ok(2), depends_on=("a",))]
    _out, sink = await run(nodes)
    assert len(sink.started) == 1 and len(sink.finished) == 1
    seqs = [r.seq for r in sink.steps]
    assert seqs == sorted(seqs) == list(range(1, len(seqs) + 1))
    assert sink.started[0][2][1].depends_on == ("a",)


async def test_9b_sink_failure_never_fails_run_but_proposal_failure_retries():
    class Broken(MemorySink):
        async def step(self, rec):
            raise RuntimeError("nats down")

    sink = Broken()
    out = await executor(sink).run(TaskGraph("t@1", [Node("a", "a", "x", D, ok(1))]), RUN, Budget())
    assert out.status is RunStatus.SUCCEEDED

    class ProposalFails(MemorySink):
        n = 0

        async def proposal(self, identity, draft, pid):
            self.n += 1
            if self.n == 1:
                raise TransientModelError("publish failed")
            await super().proposal(identity, draft, pid)

    draft = ProposalDraft(kind="document.attribution", target_type="document", target_id="d1",
                          summary_key="k")

    async def propose(ctx):
        return await ctx.propose(draft)

    sink2 = ProposalFails()
    out = await executor(sink2).run(TaskGraph("t@1", [Node("a", "a", "x", D, propose)]), RUN, Budget())
    assert out.status is RunStatus.SUCCEEDED and len(sink2.proposals) == 1
    assert out.results["a"] == sink2.proposals[0][2]


async def test_10_expand_join_rewires_dependents_and_chains():
    order: list[str] = []

    def rec(name, value=None):
        async def fn(ctx):
            order.append(name)
            return value

        return fn

    async def j1(ctx):
        order.append("j1")
        return Expand(nodes=(Node("r2", "a", "x", D, rec("r2")),
                             Node("j2", "a", "x", D, rec("j2", "final"), depends_on=("r2",), trigger="all_done")),
                      result="j1", join="j2")

    async def x(ctx):
        order.append("x")
        return Expand(nodes=(Node("r1", "a", "x", D, rec("r1")),
                             Node("j1", "a", "x", D, j1, depends_on=("r1",), trigger="all_done")),
                      result="x", join="j1")

    nodes = [Node("x", "a", "x", D, x), Node("emit", "a", "x", D, rec("emit"), depends_on=("x",), trigger="all_done")]
    _out, sink = await run(nodes)
    assert order == ["x", "r1", "j1", "r2", "j2", "emit"]
    emit_deps = [r.depends_on for r in sink.steps if r.node_id == "emit"][-1]
    assert emit_deps == ("x", "j1", "j2")


async def test_10b_cycle_from_rewiring_fails_expanding_node():
    async def x(ctx):
        return Expand(nodes=(Node("j", "a", "x", D, ok(), depends_on=("d",)),), join="j")

    nodes = [Node("x", "a", "x", D, x, critical=False), Node("d", "a", "x", D, ok(), depends_on=("x",))]
    out, _ = await run(nodes)
    assert out.node_status["x"] is StepStatus.FAILED
    assert out.node_status["d"] is StepStatus.SKIPPED


async def test_11_step_ids_per_attempt():
    n = 0

    async def flaky(ctx):
        nonlocal n
        n += 1
        if n == 1:
            raise TransientModelError("x")
        return ctx.step_id

    out, sink = await run([Node("a", "a", "x", D, flaky)])
    ids = {r.attempt: r.step_id for r in sink.steps}
    assert ids[1] == step_id_for(RUN.run_id, "a", 1) and ids[2] == step_id_for(RUN.run_id, "a", 2)
    assert out.results["a"] == ids[2]


async def test_fan_out_and_validate():
    def make(node_id, item):
        return Node(node_id, "v", "check", D, ok(item * 2))

    async def src(ctx):
        return fan_out("inv", [1, 2, 3], make, join=Node("all", "v", "join", D, ok("j"), trigger="all_done"))

    out, _ = await run([Node("src", "a", "x", D, src)])
    assert out.results["inv#2"] == 6 and out.results["all"] == "j"
    with pytest.raises(GraphInvalid):
        TaskGraph("t@1", [Node("a", "a", "x", D, ok(), depends_on=("a",))]).validate()


async def test_emit_line_and_feed_key():
    async def talk(ctx):
        ctx.emit("intake.classified", kind="invoice")
        return 1

    _out, sink = await run([Node("a", "a", "x", D, talk), Node("b", "a", "x", D, ok(), feed_key="done.b")])
    a = [r for r in sink.steps if r.node_id == "a"]
    assert [r.status.value for r in a] == ["started", "started", "succeeded"]
    assert a[-1].message_key == "intake.classified" and a[-1].message_args == {"kind": "invoice"}
    assert [r for r in sink.steps if r.node_id == "b"][-1].message_key == "done.b"


async def test_finalize_runs_before_run_finished_and_failure_propagates():
    seen = []

    async def fin(outcome):
        seen.append(outcome.status)
        raise RuntimeError("publish failed")

    sink = MemorySink()
    with pytest.raises(RuntimeError):
        await executor(sink).run(TaskGraph("t@1", [Node("a", "a", "x", D, ok())]), RUN, Budget(), finalize=fin)
    assert seen == [RunStatus.SUCCEEDED]
    assert sink.finished[0].status is RunStatus.FAILED and sink.finished[0].error_code == "finalize_failed"


# ------------------------------------------------------------------ review B #4: scheduler failures
def hang():
    async def fn(ctx):
        await asyncio.sleep(3600)

    return fn


def leaked(before: set[asyncio.Task]) -> list[asyncio.Task]:
    return [t for t in asyncio.all_tasks() - before if t is not asyncio.current_task() and not t.done()]


def raising_when(_results) -> bool:
    raise ZeroDivisionError("bad predicate")


async def test_a_raising_when_fails_the_node_and_the_run_cleanly():
    """A `when` raised out of run(): no run_finished, the sibling node and the emitter task kept running."""
    before = set(asyncio.all_tasks())
    out, sink = await run([Node("slow", "a", "x", D, hang()), Node("a", "a", "x", D, ok(1)),
                           Node("b", "a", "x", D, ok(2), depends_on=("a",), when=raising_when),
                           Node("c", "a", "x", D, ok(3), depends_on=("b",))])
    assert out.status is RunStatus.FAILED and out.error_code == "internal"
    assert [r.status for r in sink.steps if r.node_id == "b"] == [StepStatus.FAILED]
    b = next(r for r in sink.steps if r.node_id == "b")
    assert (b.attempt, b.step_id, b.error_code) == (0, step_id_for(RUN.run_id, "b", 0), "internal")
    slow = [r for r in sink.steps if r.node_id == "slow"][-1]
    assert slow.status is StepStatus.FAILED and slow.error_code == "cancelled"
    assert out.node_status["c"] is StepStatus.SKIPPED
    assert len(sink.finished) == 1 and sink.finished[0].status is RunStatus.FAILED
    assert leaked(before) == []


async def test_a_raising_when_on_a_non_critical_node_fails_only_that_node():
    out, sink = await run([Node("a", "a", "x", D, ok(1)),
                           Node("b", "a", "x", D, ok(2), depends_on=("a",), when=raising_when, critical=False),
                           Node("c", "a", "x", D, ok(3), depends_on=("b",)),
                           Node("d", "a", "x", D, ok(4), depends_on=("a",))])
    assert out.status is RunStatus.SUCCEEDED
    assert (out.node_status["b"], out.node_status["c"]) == (StepStatus.FAILED, StepStatus.SKIPPED)
    assert out.results["d"] == 4 and len(sink.finished) == 1


async def test_a_scheduler_error_fails_the_run_and_leaks_no_task():
    class Broken(GraphExecutor):
        calls = 0

        def _schedule(self, *a, **kw):
            Broken.calls += 1
            if Broken.calls > 1:
                raise RuntimeError("scheduler bug")
            return super()._schedule(*a, **kw)

    before = set(asyncio.all_tasks())
    sink = MemorySink()
    ex = Broken(gateway=FakeGateway({}), sink=sink, tools=ToolRegistry(), clock=FakeClock(), sleep=no_sleep)
    out = await ex.run(TaskGraph("t@1", [Node("slow", "a", "x", D, hang()), Node("a", "a", "x", D, ok(1))]),
                       RUN, Budget())
    assert out.status is RunStatus.FAILED and out.error_code == "internal"
    assert out.node_status["slow"] is StepStatus.FAILED
    assert [r for r in sink.steps if r.node_id == "slow"][-1].error_code == "cancelled"
    assert len(sink.finished) == 1 and leaked(before) == []


@pytest.mark.parametrize("max_parallel", [0, -1])
async def test_max_parallel_must_be_positive(max_parallel):
    """max_parallel <= 0 used to validate, run nothing and report SUCCEEDED."""
    graph = TaskGraph("t@1", [Node("a", "a", "x", D, ok(1))], max_parallel=max_parallel)
    with pytest.raises(GraphInvalid, match="max_parallel"):
        graph.validate()
    with pytest.raises(GraphInvalid):
        await executor(MemorySink()).run(graph, RUN, Budget())
