"""AgentContext.complete / run_tool_loop / propose through a real GraphExecutor run."""

import uuid

from pydantic import BaseModel

from ai.gateway.fake import FakeGateway, response_for
from ai.gateway.types import SONNET, Message, ModelRequest, ModelResponse, TextPart, ToolCall, Usage
from ai.runtime.clock import FakeClock
from ai.runtime.evalhooks import CollectingHook
from ai.runtime.events import MemorySink
from ai.runtime.graph import GraphExecutor, Node, TaskGraph
from ai.runtime.proposals import ProposalDraft, proposal_id
from ai.runtime.tools import ToolRegistry, tool
from ai.runtime.types import Budget, RunIdentity, RunStatus, StepKind

RUN = RunIdentity(run_id=str(uuid.uuid4()), firm_id="firm-1", client_company_id="cc", workflow="t@1",
                  subject_type="document", subject_id="d1")


class Out(BaseModel):
    v: str = ""


def req(output_model: type[BaseModel] | None = Out) -> ModelRequest:
    return ModelRequest(model=SONNET, prompt_id="t.p", prompt_version=1, system="s",
                        messages=(Message("user", (TextPart("x"),)),), output_model=output_model)


async def go(fn, gateway, hooks=(), tools: ToolRegistry | None = None):
    tools = tools or ToolRegistry()
    sink = MemorySink()
    ex = GraphExecutor(gateway=gateway, sink=sink, tools=tools, hooks=hooks, clock=FakeClock())
    node = Node("a", "agent", "act", StepKind.LLM, fn, tools=tuple(s.name for s in tools.specs()))
    out = await ex.run(TaskGraph("t@1", [node]), RUN, Budget())
    return out, sink


async def test_complete_sets_meta_meters_and_traces(spans):
    gw = FakeGateway({"t.p": [Out(v="1")]})
    hook = CollectingHook()

    async def fn(ctx):
        return (await ctx.complete(req())).parsed

    out, sink = await go(fn, gw, hooks=[hook])
    assert out.results["a"] == Out(v="1")
    sent = gw.calls[0]
    assert sent.meta is not None and sent.meta.firm_id == "firm-1" and sent.meta.run_id == RUN.run_id
    assert out.totals.llm_calls == 1 and len(hook.calls) == 1 and hook.outcome is out
    assert sink.steps[-1].usage is not None and sink.steps[-1].usage.llm_calls == 1
    chat = next(s for s in spans.get_finished_spans() if s.name == f"chat {SONNET}")
    assert chat.attributes["compliance.cache_hit"] is False
    assert chat.attributes["compliance.prompt.id"] == "t.p"
    assert "gen_ai.prompt" not in chat.attributes  # rule 6: no prompt text in spans


async def test_cache_hit_emits_feed_line_and_span_flag(spans):
    def script(r: ModelRequest) -> ModelResponse:
        base = response_for(r, Out(v="c"))
        return ModelResponse(model=base.model, text=base.text, parsed=base.parsed, tool_calls=(),
                             stop_reason="end_turn", usage=Usage(model=r.model, response_cache_hit=True),
                             latency_ms=0, cache_key=base.cache_key)

    async def fn(ctx):
        await ctx.complete(req())

    out, sink = await go(fn, FakeGateway(script))
    assert out.totals.llm_calls == 0 and out.totals.response_cache_hits == 1
    assert sink.steps[-1].message_key == "gateway.cache_hit"
    chat = next(s for s in spans.get_finished_spans() if s.name.startswith("chat "))
    assert chat.attributes["compliance.cache_hit"] is True


class Q(BaseModel):
    q: str


@tool(name="echo", version=1, description="echo", side_effects="none")
async def echo(ctx, args: Q) -> str:
    return args.q.upper()


async def test_run_tool_loop_returns_results_in_one_user_message():
    turns = iter([
        ModelResponse(model=SONNET, text="", parsed=None, tool_calls=(ToolCall("u1", "echo", {"q": "a"}),
                                                                       ToolCall("u2", "echo", {"q": "b"})),
                      stop_reason="tool_use", usage=Usage(llm_calls=1), latency_ms=0, cache_key="k"),
        ModelResponse(model=SONNET, text="done", parsed=None, tool_calls=(), stop_reason="end_turn",
                      usage=Usage(llm_calls=1), latency_ms=0, cache_key="k2"),
    ])
    gw = FakeGateway(lambda r: next(turns))

    async def fn(ctx):
        return (await ctx.run_tool_loop(req(output_model=None))).text

    out, _ = await go(fn, gw, tools=ToolRegistry([echo]))
    assert out.status is RunStatus.SUCCEEDED and out.results["a"] == "done"
    second = gw.calls[1].messages
    assert [m.role for m in second] == ["user", "assistant", "user"]
    assert [p.content for p in second[2].parts] == ["A", "B"]


async def test_propose_is_deterministic_and_published():
    draft = ProposalDraft(kind="document.attribution", target_type="document", target_id="d1", summary_key="k")

    async def fn(ctx):
        return await ctx.propose(draft)

    out, sink = await go(fn, FakeGateway({}))
    assert out.results["a"] == proposal_id(RUN, draft) == sink.proposals[0][2]
    other_run = RunIdentity(**{**{f: getattr(RUN, f) for f in RUN.__slots__}, "run_id": str(uuid.uuid4())})
    assert proposal_id(other_run, draft) == proposal_id(RUN, draft)  # redelivered run: same id
    assert sink.steps[-1].message_key == "proposal.created"
