"""The `fix_invoice@1` task graph and the `agent.task.requested` handler for agent `fix` (spec 5.7.2).

    fix.deterministic -> fix.llm (gated) -> fix.verify (all_done) -> fix.propose -> fix.result (all_done)

`fix.llm` is non-critical: a model refusal, outage or spend cap leaves the computed fixes standing.
"""

from __future__ import annotations

from collections.abc import Callable

from ai.agents.fix import nodes
from ai.agents.fix.agent import AGENT_NAME, TOOL_NAME, FixAgent
from ai.agents.fix.config import FixConfig
from ai.agents.verifier.agent import VerifierAgent
from ai.gen.compliance.v1 import validator_pb2
from ai.runtime.context import NodeContext
from ai.runtime.graph import Node, TaskGraph
from ai.runtime.tasks import TaskPlan
from ai.runtime.types import StepKind

WORKFLOW = "fix_invoice@1"
MODES = ("auto", "on_demand")


def build_graph(inp: validator_pb2.FixTaskInput, cfg: FixConfig, verifier: VerifierAgent) -> TaskGraph:
    agent = FixAgent(model=cfg.model)

    async def det(ctx: NodeContext) -> object:
        return await nodes.deterministic(ctx, inp)

    async def llm(ctx: NodeContext) -> object:
        return await nodes.llm(ctx, agent)

    async def verify(ctx: NodeContext) -> object:
        return await nodes.verify(ctx, inp, verifier)

    async def propose(ctx: NodeContext) -> object:
        return await nodes.propose(ctx, inp)

    async def result(ctx: NodeContext) -> object:
        return await nodes.result(ctx)

    return TaskGraph(WORKFLOW, [
        Node(nodes.NODE_DETERMINISTIC, AGENT_NAME, "deterministic", StepKind.DETERMINISTIC, det,
             tools=(TOOL_NAME,), timeout_s=60.0),
        Node(nodes.NODE_LLM, AGENT_NAME, "llm", StepKind.LLM, llm, depends_on=(nodes.NODE_DETERMINISTIC,),
             when=lambda r: nodes.llm_wanted(r, cfg, inp.mode), critical=False, timeout_s=90.0),
        Node(nodes.NODE_VERIFY, AGENT_NAME, "verify", StepKind.TOOL, verify,
             depends_on=(nodes.NODE_DETERMINISTIC, nodes.NODE_LLM), trigger="all_done", tools=(TOOL_NAME,),
             timeout_s=120.0),
        Node(nodes.NODE_PROPOSE, AGENT_NAME, "propose", StepKind.DETERMINISTIC, propose,
             depends_on=(nodes.NODE_VERIFY,)),
        Node(nodes.NODE_RESULT, AGENT_NAME, "result", StepKind.DETERMINISTIC, result,
             depends_on=(nodes.NODE_VERIFY, nodes.NODE_PROPOSE), trigger="all_done"),
    ])


def parse_input(req: object) -> validator_pb2.FixTaskInput:
    """`AgentTaskRequested.input` as a FixTaskInput; ValueError (a bad request) when it is anything else."""
    any_msg = getattr(req, "input")  # noqa: B009 - the request is an agents_pb2.AgentTaskRequested
    if not any_msg.Is(validator_pb2.FixTaskInput.DESCRIPTOR):
        raise ValueError("input is not a FixTaskInput")
    inp = validator_pb2.FixTaskInput()
    any_msg.Unpack(inp)
    if not inp.invoice_id or not inp.HasField("invoice"):
        raise ValueError("FixTaskInput needs invoice_id and invoice")
    if inp.mode not in MODES:
        raise ValueError(f"FixTaskInput.mode {inp.mode!r} is not auto or on_demand")
    subject_id = getattr(req, "subject_id")  # noqa: B009
    if subject_id and subject_id != inp.invoice_id:
        raise ValueError("subject_id differs from FixTaskInput.invoice_id")
    return inp


class FixTaskHandler:
    """`TaskHandler` for agent `fix`. The configuration (AI_MODEL_FIX, FIX_AUTO_LLM, gateway mode) is read when
    a task arrives, so building the handler in `register()` never touches the environment."""

    def __init__(self, verifier: VerifierAgent, config: Callable[[], FixConfig] = FixConfig.from_env) -> None:
        self._verifier = verifier
        self._config = config

    async def __call__(self, req: object) -> TaskPlan:
        inp = parse_input(req)
        graph = build_graph(inp, self._config(), self._verifier)
        return TaskPlan(graph=graph, workflow_subject=("invoice", inp.invoice_id), output_node=nodes.NODE_RESULT)
