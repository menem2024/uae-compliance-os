"""The `ai-agent-tasks` consumer (contract section 3.8): dispatch, AgentTaskCompleted, dead letters."""

import asyncio
from collections.abc import Sequence
from dataclasses import dataclass

from google.protobuf import any_pb2, wrappers_pb2
from nats.js.api import AckPolicy
from nats.js.errors import FetchTimeoutError

from ai.gateway.fake import FakeGateway
from ai.gen.compliance.v1 import agents_pb2
from ai.runtime.context import NodeContext
from ai.runtime.events import MemorySink
from ai.runtime.graph import Node, TaskGraph
from ai.runtime.registry import AgentRegistry
from ai.runtime.tasks import TaskPlan
from ai.runtime.types import Budget, StepKind
from ai.service import tasks_consumer
from ai.settings import Settings

FIRM = "00000000-0000-4000-8000-00000000e001"
CC = "00000000-0000-4000-8000-00000000c001"


@dataclass
class Meta:
    num_delivered: int


class FakeMsg:
    def __init__(self, data: bytes, num_delivered: int = 1) -> None:
        self.data = data
        self.headers = {"Nats-Msg-Id": "agent.task.requested:t1"}
        self.metadata = Meta(num_delivered)
        self.events: list[str] = []
        self.naks: list[float | None] = []
        self.settled = asyncio.Event()

    async def ack(self) -> None:
        self.events.append("ack")
        self.settled.set()

    async def nak(self, delay: float | None = None) -> None:
        self.events.append("nak")
        self.naks.append(delay)
        self.settled.set()

    async def term(self) -> None:
        self.events.append("term")
        self.settled.set()

    async def in_progress(self) -> None:
        self.events.append("in_progress")


class FakeSub:
    def __init__(self, msgs: Sequence[FakeMsg]) -> None:
        self.queue = list(msgs)
        self.unsubscribed = False

    async def fetch(self, batch: int = 1, timeout: float | None = None) -> list[FakeMsg]:
        if not self.queue:
            await asyncio.sleep(0.005)
            raise FetchTimeoutError
        out, self.queue = self.queue[:batch], self.queue[batch:]
        return out

    async def unsubscribe(self) -> None:
        self.unsubscribed = True


class FakeJS:
    def __init__(self, sub: FakeSub) -> None:
        self.sub = sub
        self.subscribed: list[tuple] = []
        self.published: list[tuple[str, bytes, dict | None]] = []

    async def pull_subscribe(self, subject, durable=None, stream=None, config=None):
        self.subscribed.append((subject, durable, stream, config))
        return self.sub

    async def publish(self, subject, payload=b"", timeout=None, stream=None, headers=None):
        self.published.append((subject, payload, headers))

    def completed(self) -> list[tuple[str, str, agents_pb2.AgentTaskCompleted]]:
        return [(s, (h or {}).get("Nats-Msg-Id", ""), agents_pb2.AgentTaskCompleted.FromString(p))
                for s, p, h in self.published if s.startswith("agent.task.completed.")]

    def dead(self) -> list[tuple[str, bytes]]:
        return [(s, p) for s, p, _ in self.published if s.startswith("dlq.")]


def request(agent: str = "echo", *, task_id: str = "t1", budget: agents_pb2.RunBudget | None = None,
            text: str = "hello") -> agents_pb2.AgentTaskRequested:
    inp = any_pb2.Any()
    inp.Pack(wrappers_pb2.StringValue(value=text))
    req = agents_pb2.AgentTaskRequested(task_id=task_id, firm_id=FIRM, client_company_id=CC, agent=agent,
                                        subject_type="invoice", subject_id="inv-1", input=inp,
                                        requested_by="u1")
    if budget is not None:
        req.budget.CopyFrom(budget)
    return req


def echo_plan(req: agents_pb2.AgentTaskRequested, *, budget: Budget | None = None,
              critical_failure: bool = False, subject: tuple[str, str] | None = None,
              output_node: str = "echo") -> TaskPlan:
    value = wrappers_pb2.StringValue()
    req.input.Unpack(value)

    async def fn(ctx: NodeContext) -> wrappers_pb2.StringValue:
        if critical_failure:
            raise RuntimeError("boom")
        return wrappers_pb2.StringValue(value=value.value.upper())

    graph = TaskGraph("echo@1", [Node("echo", "echo", "shout", StepKind.DETERMINISTIC, fn)])
    return TaskPlan(graph=graph, workflow_subject=subject, output_node=output_node, budget=budget)


@dataclass
class Consumed:
    js: FakeJS
    sink: MemorySink


async def consume(msgs: Sequence[FakeMsg], reg: AgentRegistry) -> Consumed:
    out = Consumed(FakeJS(FakeSub(msgs)), MemorySink())
    stop = asyncio.Event()

    async def no_sleep(s: float) -> None:  # retry backoffs pass at once; the 20 s heartbeat never comes due
        if s >= 20.0:
            await asyncio.Event().wait()
        await asyncio.sleep(0)

    task = asyncio.create_task(tasks_consumer.run(out.js, gateway=FakeGateway({}), registry=reg,
                                                  settings=Settings(), stop_event=stop, sink=out.sink,
                                                  sleep=no_sleep))
    await asyncio.wait_for(asyncio.gather(*(m.settled.wait() for m in msgs)), 10)
    stop.set()
    await asyncio.wait_for(task, 5)
    return out


def registry(**handlers) -> AgentRegistry:
    reg = AgentRegistry()
    for name, h in handlers.items():
        reg.add_task_handler(name, h)
    return reg


async def test_the_durable_matches_the_contract():
    msg = FakeMsg(request("nobody").SerializeToString())
    out = await consume([msg], registry())
    subject, durable, stream, cfg = out.js.subscribed[0]
    assert (subject, durable, stream) == ("agent.task.requested", "ai-agent-tasks", "AGENTS")
    assert tasks_consumer.TASKS_DURABLE == "ai-agent-tasks"
    assert (cfg.durable_name, cfg.filter_subject, cfg.max_deliver, cfg.ack_policy) == (
        "ai-agent-tasks", "agent.task.requested", 5, AckPolicy.EXPLICIT)


async def test_an_unknown_agent_is_dead_lettered():
    raw = request("nobody").SerializeToString()
    msg = FakeMsg(raw)
    out = await consume([msg], registry())
    assert msg.events == ["term"]
    assert out.js.dead() == [("dlq.agent.task", raw)] and out.js.completed() == []
    assert out.sink.started == []


async def test_a_registered_handler_runs_its_graph_and_publishes_the_packed_output():
    async def handler(req: agents_pb2.AgentTaskRequested) -> TaskPlan:
        return echo_plan(req)

    msg = FakeMsg(request(text="hi").SerializeToString(), num_delivered=3)
    out = await consume([msg], registry(echo=handler))
    assert msg.events == ["ack"]
    (subject, msg_id, done), = out.js.completed()
    assert (subject, msg_id) == ("agent.task.completed.echo", "agent.task.completed:t1")
    (identity, budget, _plan), = out.sink.started
    assert (done.task_id, done.firm_id, done.client_company_id, done.agent, done.run_id) == (
        "t1", FIRM, CC, "echo", identity.run_id)
    assert done.status == agents_pb2.AGENT_RUN_STATUS_SUCCEEDED and done.error_code == ""
    assert (done.subject_type, done.subject_id) == ("invoice", "inv-1")
    got = wrappers_pb2.StringValue()
    assert done.output.Unpack(got) and got.value == "HI"
    assert (identity.workflow, identity.subject_type, identity.subject_id, identity.delivery_attempt) == (
        "echo@1", "invoice", "inv-1", 3)
    assert budget == Budget()
    assert out.js.dead() == []


async def test_budget_precedence_and_the_plan_subject():
    async def plan_budget(req):
        return echo_plan(req, budget=Budget(max_steps=3), subject=("document", "d9"))

    async def request_budget(req):
        return echo_plan(req)

    m1 = FakeMsg(request(task_id="a").SerializeToString())
    m2 = FakeMsg(request("other", task_id="b",
                         budget=agents_pb2.RunBudget(max_steps=7, deadline_seconds=30)).SerializeToString())
    out = await consume([m1, m2], registry(echo=plan_budget, other=request_budget))
    by_task = {done.task_id: done for _s, _m, done in out.js.completed()}
    seen = {s[0].workflow + s[0].subject_id: s[1] for s in out.sink.started}
    assert seen["echo@1d9"] == Budget(max_steps=3)
    assert seen["echo@1inv-1"] == Budget(max_steps=7, deadline_s=30.0)
    assert (by_task["a"].subject_type, by_task["a"].subject_id) == ("document", "d9")


async def test_a_handler_value_error_is_a_definitive_bad_request_not_a_dead_letter():
    async def handler(req):
        raise ValueError("input is not a StringValue")

    msg = FakeMsg(request().SerializeToString())
    out = await consume([msg], registry(echo=handler))
    assert msg.events == ["ack"]
    (_s, msg_id, done), = out.js.completed()
    assert msg_id == "agent.task.completed:t1"
    assert done.status == agents_pb2.AGENT_RUN_STATUS_FAILED and done.error_code == "bad_request"
    assert done.run_id == "" and not done.HasField("output")
    assert out.js.dead() == [] and out.sink.started == []


async def test_a_failed_run_is_reported_with_its_error_code():
    async def handler(req):
        return echo_plan(req, critical_failure=True)

    msg = FakeMsg(request().SerializeToString())
    out = await consume([msg], registry(echo=handler))
    assert msg.events == ["ack"]
    (_s, _m, done), = out.js.completed()
    assert done.status == agents_pb2.AGENT_RUN_STATUS_FAILED and done.error_code == "internal"
    assert not done.HasField("output")


async def test_a_missing_output_is_a_failure_not_an_empty_success():
    async def handler(req):
        return echo_plan(req, output_node="nowhere")

    msg = FakeMsg(request().SerializeToString())
    out = await consume([msg], registry(echo=handler))
    (_s, _m, done), = out.js.completed()
    assert done.status == agents_pb2.AGENT_RUN_STATUS_FAILED and done.error_code == "internal"


async def test_a_handler_crash_naks_and_dead_letters_with_a_definitive_answer_at_max_deliver():
    async def handler(req):
        raise RuntimeError("registry backend down")

    raw = request().SerializeToString()
    early, last = FakeMsg(raw, num_delivered=1), FakeMsg(raw, num_delivered=tasks_consumer.MAX_DELIVER)
    out = await consume([early], registry(echo=handler))
    assert early.events == ["nak"] and early.naks == [30.0] and out.js.published == []
    out = await consume([last], registry(echo=handler))
    assert last.events == ["term"] and out.js.dead() == [("dlq.agent.task", raw)]
    (_s, _m, done), = out.js.completed()
    assert done.status == agents_pb2.AGENT_RUN_STATUS_FAILED and done.error_code == "max_deliver_exceeded"


async def test_an_undecodable_request_is_dead_lettered():
    msg = FakeMsg(b"\x0a\xff")
    out = await consume([msg], registry())
    assert msg.events == ["term"] and out.js.dead() == [("dlq.agent.task", b"\x0a\xff")]
