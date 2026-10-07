"""`route` (spec section 5.2): the branch every downstream `when` reads from ctx.results["route"]."""

from __future__ import annotations

from typing import Literal

from ai.agents.orchestrator.fetch import AGENT, CSV, IMAGE_TYPES, PDF, XLSX, Fetched, FetchError
from ai.runtime.context import NodeContext
from ai.runtime.graph import Node
from ai.runtime.types import StepKind

type Branch = Literal["tabular", "llm", "too_many_pages"]


def route_of(fetched: Fetched, max_pdf_pages: int) -> Branch:
    if fetched.media_type in (CSV, XLSX):
        return "tabular"
    if fetched.media_type == PDF:
        return "too_many_pages" if fetched.page_count > max_pdf_pages else "llm"
    if fetched.media_type in IMAGE_TYPES:
        return "llm"
    raise FetchError("unsupported_format")


def route_node(max_pdf_pages: int) -> Node:
    async def fn(ctx: NodeContext) -> Branch:
        branch = route_of(ctx.result("fetch", Fetched), max_pdf_pages)
        ctx.emit("orchestrator.routed", branch=branch)
        return branch

    return Node("route", AGENT, "route", StepKind.ROUTER, fn, depends_on=("fetch",), critical=True)
