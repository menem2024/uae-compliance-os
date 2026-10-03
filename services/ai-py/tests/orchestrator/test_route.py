"""`route` node (spec section 5.2): spreadsheets go tabular, PDFs and images go to the LLM branch, and a PDF
over the page cap short-circuits before any model call."""

import pytest

from ai.agents.orchestrator.fetch import CSV, JPEG, PDF, PNG, WEBP, XLSX, Fetched
from ai.agents.orchestrator.route import route_node
from ai.runtime.types import StepKind


class Ctx:
    def __init__(self, fetched: Fetched) -> None:
        self.results = {"fetch": fetched}
        self.lines: list[tuple[str, dict[str, str]]] = []

    def result[T](self, node_id: str, typ: type[T]) -> T:
        value = self.results[node_id]
        assert isinstance(value, typ)
        return value

    def emit(self, message_key: str, **args: str) -> None:
        self.lines.append((message_key, args))


async def route(media_type: str, pages: int, max_pdf_pages: int = 10) -> tuple[str, Ctx]:
    ctx = Ctx(Fetched(b"data", media_type, pages))
    return await route_node(max_pdf_pages).fn(ctx), ctx  # type: ignore[arg-type]


@pytest.mark.parametrize(("media_type", "pages"), [(PDF, 1), (PDF, 10), (PNG, 1), (JPEG, 1), (WEBP, 1)])
async def test_pdf_and_images_under_the_cap_go_to_the_llm(media_type, pages):
    branch, ctx = await route(media_type, pages)
    assert branch == "llm" and ctx.lines == [("orchestrator.routed", {"branch": "llm"})]


@pytest.mark.parametrize("media_type", [XLSX, CSV])
async def test_spreadsheets_go_tabular(media_type):
    assert (await route(media_type, 0))[0] == "tabular"


async def test_a_pdf_over_the_cap_is_too_many_pages():
    assert (await route(PDF, 11))[0] == "too_many_pages"
    assert (await route(PDF, 3, max_pdf_pages=2))[0] == "too_many_pages"


def test_route_is_a_critical_router_after_fetch():
    node = route_node(10)
    assert (node.id, node.agent, node.kind, node.depends_on, node.critical) == (
        "route", "orchestrator", StepKind.ROUTER, ("fetch",), True)
