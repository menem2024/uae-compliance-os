from typing import ClassVar

import pytest
from pydantic import BaseModel

from ai.runtime.graph import Node, TaskGraph
from ai.runtime.registry import AgentRegistry, load_registry
from ai.runtime.tasks import TaskPlan
from ai.runtime.tools import tool
from ai.runtime.types import StepKind


class In(BaseModel):
    x: str = ""


class EchoAgent:
    name: ClassVar[str] = "echo"
    version: ClassVar[int] = 1
    input_model: ClassVar[type[BaseModel]] = In
    output_model: ClassVar[type[BaseModel]] = In
    tools: ClassVar[tuple[str, ...]] = ()

    async def run(self, ctx, inp):
        return inp


class Profile:
    output_type = In


@tool(name="lookup", version=1, description="d", side_effects="read")
async def lookup(ctx, args: In) -> str:
    return args.x


async def handler(req) -> TaskPlan:
    async def fn(ctx):
        return None

    return TaskPlan(graph=TaskGraph("echo@1", [Node("run", "echo", "run", StepKind.DETERMINISTIC, fn)]))


def test_registry_rejects_duplicates_and_bad_names():
    reg = AgentRegistry()
    reg.add_agent(EchoAgent())
    reg.add_tool(lookup)
    reg.add_task_handler("echo", handler)
    reg.add_verifier_profile(Profile())
    for add in (lambda: reg.add_agent(EchoAgent()), lambda: reg.add_tool(lookup),
                lambda: reg.add_task_handler("echo", handler), lambda: reg.add_verifier_profile(Profile())):
        with pytest.raises(ValueError):
            add()
    bad = EchoAgent()
    bad.name = "Bad-Name"
    with pytest.raises(ValueError):
        AgentRegistry().add_agent(bad)


class FakeEntryPoint:
    def __init__(self, name, fn):
        self.name = name
        self._fn = fn

    def load(self):
        return self._fn


def test_load_registry_calls_every_entry_point_in_name_order():
    order = []
    eps = [FakeEntryPoint("zeta", lambda reg: order.append("zeta")),
           FakeEntryPoint("alpha", lambda reg: (order.append("alpha"), reg.add_agent(EchoAgent())))]
    reg = load_registry(eps=eps)
    assert order == ["alpha", "zeta"] and "echo" in reg.agents


def test_broken_entry_point_fails_fast():
    def boom(reg):
        raise RuntimeError("bad plugin")

    with pytest.raises(RuntimeError):
        load_registry(eps=[FakeEntryPoint("bad", boom)])


def test_task_plan_defaults():
    plan = TaskPlan(graph=TaskGraph("w@1", []))
    assert (plan.workflow_subject, plan.output_node, plan.budget) == (None, "", None)
