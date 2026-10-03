"""The `ai-documents` consumer: ack/nak/dead-letter decisions (contract section 4, AC-E2) and the heartbeat.

Fake JetStream and pull subscription; a real GraphExecutor (MemorySink) running the real document_ingestion@1
graph with the real Phase 1 agents and the Verifier wired as in production."""

import asyncio
import hashlib
import io
from collections.abc import Awaitable, Callable, Sequence
from dataclasses import dataclass, field

import pypdfium2 as pdfium
import pytest
from nats.js.api import AckPolicy, ConsumerConfig
from nats.js.errors import FetchTimeoutError

from ai.agents.extraction.agent import ExtractionAgent
from ai.agents.intake.agent import IntakeAgent
from ai.agents.orchestrator.fetch import FetchError
from ai.gateway.errors import TransientModelError
from ai.gateway.fake import DEFAULT_SCENARIO, FakeGateway, ScenarioGateway
from ai.gateway.types import ModelGateway
from ai.gen.compliance.v1 import agents_pb2, documents_pb2
from ai.runtime.events import MemorySink
from ai.runtime.registry import AgentRegistry
from ai.runtime.types import RunStatus
from ai.service import bootstrap, documents_consumer
from ai.settings import Settings

FIRM = "00000000-0000-4000-8000-00000000e001"
CC = "00000000-0000-4000-8000-00000000c001"
DOC = "00000000-0000-4000-8000-00000000d001"
KEY = f"firms/{FIRM}/docs/{DOC}"


def pdf() -> bytes:
    doc = pdfium.PdfDocument.new()
    doc.new_page(595, 842)
    buf = io.BytesIO()
    doc.save(buf)
    doc.close()
    return buf.getvalue()


PDF = pdf()


def upload(doc_id: str = DOC, data: bytes = PDF) -> documents_pb2.DocumentUploaded:
    return documents_pb2.DocumentUploaded(
        document_id=doc_id, firm_id=FIRM, client_company_id=CC, sha256=hashlib.sha256(data).hexdigest(),
        object_key=f"firms/{FIRM}/docs/{doc_id}", content_type="application/pdf", size_bytes=len(data),
        filename="scan.pdf",
        candidates=[documents_pb2.ClientCompanyRef(client_company_id=CC, name="Chosen Co")],
        reprocess_nonce="n1")


@dataclass
class Meta:
    num_delivered: int


class FakeMsg:
    def __init__(self, data: bytes, num_delivered: int = 1) -> None:
        self.data = data
        self.headers = {"Nats-Msg-Id": "document.uploaded:x:n1"}
        self.metadata = Meta(num_delivered)
        self.events: list[str] = []
        self.naks: list[float | None] = []
        self.settled = asyncio.Event()

    def count(self, name: str) -> int:
        return self.events.count(name)

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
        self.batches: list[int] = []

    async def fetch(self, batch: int = 1, timeout: float | None = None) -> list[FakeMsg]:
        if not self.queue:
            await asyncio.sleep(0.005)
            raise FetchTimeoutError
        self.batches.append(batch)
        out, self.queue = self.queue[:batch], self.queue[batch:]
        return out

    async def unsubscribe(self) -> None:
        self.unsubscribed = True


class FakeJS:
    def __init__(self, sub: FakeSub) -> None:
        self.sub = sub
        self.subscribed: list[tuple[str, str | None, str | None, ConsumerConfig | None]] = []
        self.published: list[tuple[str, bytes]] = []

    async def pull_subscribe(self, subject, durable=None, stream=None, config=None):
        self.subscribed.append((subject, durable, stream, config))
        return self.sub

    async def publish(self, subject, payload=b"", timeout=None, stream=None, headers=None):
        self.published.append((subject, payload))


def registry() -> AgentRegistry:
    reg = AgentRegistry()
    reg.add_agent(IntakeAgent())
    reg.add_agent(ExtractionAgent())
    return reg


async def no_sleep(s: float) -> None:
    """Node retry backoffs pass at once; this clock never reaches the 20 s heartbeat (see ManualTimer)."""
    if s >= documents_consumer.HEARTBEAT_S:
        await asyncio.Event().wait()
    await asyncio.sleep(0)


async def store(key: str) -> bytes:
    assert key.startswith(f"firms/{FIRM}/docs/")
    return PDF


@dataclass
class Consumed:
    js: FakeJS
    sink: MemorySink
    published: list[object] = field(default_factory=list)


async def consume(msgs: Sequence[FakeMsg], gateway: ModelGateway, *,
                  fetch: Callable[[str], Awaitable[bytes]] = store, settings: Settings | None = None,
                  sleep: Callable[[float], Awaitable[None]] = no_sleep, publish_fails: bool = False,
                  until: Callable[[], Awaitable[None]] | None = None) -> Consumed:
    out = Consumed(FakeJS(FakeSub(msgs)), MemorySink())
    stop = asyncio.Event()

    async def publish(m: documents_pb2.DocumentExtracted | documents_pb2.DocumentFailed) -> None:
        if publish_fails:
            raise TimeoutError("nats down")
        out.published.append(m)

    task = asyncio.create_task(documents_consumer.run(
        out.js, gateway=gateway, registry=registry(), verifier=bootstrap.build_verifier(Settings()),
        fetch=fetch, publish=publish, settings=settings or Settings(), stop_event=stop, sink=out.sink,
        sleep=sleep))
    if until is not None:
        await until()
    await asyncio.wait_for(asyncio.gather(*(m.settled.wait() for m in msgs)), 10)
    stop.set()
    await asyncio.wait_for(task, 5)
    return out


def failing_gateway() -> FakeGateway:
    """A transient model failure that outlives the node's RetryPolicy (3 attempts): RetryLater."""
    return FakeGateway({"intake.classify": [TransientModelError("529 overloaded")]})


# ------------------------------------------------------------------ the binding table
async def test_the_durable_matches_the_contract():
    msg = FakeMsg(upload().SerializeToString())
    out = await consume([msg], ScenarioGateway.from_file(DEFAULT_SCENARIO),
                        settings=Settings(ack_wait_s=60, docs_max_ack_pending=8))
    subject, durable, stream, cfg = out.js.subscribed[0]
    assert (subject, durable, stream) == ("document.uploaded", documents_consumer.DOCS_DURABLE, "DOCUMENTS")
    assert documents_consumer.DOCS_DURABLE == "ai-documents"
    assert cfg is not None and (cfg.durable_name, cfg.filter_subject) == ("ai-documents", "document.uploaded")
    assert (cfg.max_deliver, cfg.ack_wait, cfg.max_ack_pending, cfg.ack_policy) == (
        5, 60, 8, AckPolicy.EXPLICIT)
    assert out.js.sub.unsubscribed  # on stop, or nc.drain() waits out its timeout on the pull inbox


async def test_a_clean_message_is_extracted_published_once_and_acked():
    msg = FakeMsg(upload().SerializeToString(), num_delivered=2)
    out = await consume([msg], ScenarioGateway.from_file(DEFAULT_SCENARIO),
                        settings=Settings(fake_results="accept"))  # the hermetic harnesses' opt-out (AC-E2)
    assert msg.events == ["ack"] and msg.naks == []
    assert len(out.published) == 1
    ext = out.published[0]
    assert isinstance(ext, documents_pb2.DocumentExtracted)
    assert (ext.document_id, ext.firm_id, ext.client_company_id) == (DOC, FIRM, CC)
    assert not ext.needs_review and ext.invoices[0].verdict.verdict == agents_pb2.VERDICT_ACCEPT
    (identity, _budget, _plan), = out.sink.started
    assert identity.delivery_attempt == 2 and ext.run_id == identity.run_id
    assert [o.status for o in out.sink.finished] == [RunStatus.SUCCEEDED]
    assert out.js.published == []  # nothing dead-lettered


async def test_the_fake_gateway_default_publishes_its_canned_result_for_review():
    """Review B #5: Helm and compose deploy AI_GATEWAY=fake; a real upload got FAKE-0001, verdict accept."""
    msg = FakeMsg(upload().SerializeToString())
    out = await consume([msg], ScenarioGateway.from_file(DEFAULT_SCENARIO))  # Settings(): fake, review
    ext, = out.published
    assert isinstance(ext, documents_pb2.DocumentExtracted) and msg.events == ["ack"]
    assert ext.needs_review and "extraction_failed" in ext.review_reasons
    assert ext.invoices[0].verdict.verdict == agents_pb2.VERDICT_ESCALATE
    assert ext.invoices[0].verdict.findings[-1].code == "provenance.fake_gateway"


async def test_a_live_gateway_result_is_not_marked():
    msg = FakeMsg(upload().SerializeToString())
    out = await consume([msg], ScenarioGateway.from_file(DEFAULT_SCENARIO), settings=Settings(gateway="replay"))
    ext, = out.published
    assert isinstance(ext, documents_pb2.DocumentExtracted) and not ext.needs_review


async def test_a_terminal_failure_is_published_and_acked_not_retried():
    async def missing(key: str) -> bytes:
        raise FetchError("object_missing")

    msg = FakeMsg(upload().SerializeToString())
    out = await consume([msg], FakeGateway({}), fetch=missing)
    assert msg.events == ["ack"]
    failed, = out.published
    assert isinstance(failed, documents_pb2.DocumentFailed) and failed.reason_code == "object_missing"


async def test_an_exception_escaping_run_naks_with_a_delay_and_publishes_nothing():
    msg = FakeMsg(upload().SerializeToString(), num_delivered=1)
    out = await consume([msg], failing_gateway())
    assert msg.events == ["nak"] and msg.naks == [documents_consumer.NAK_DELAY_S] and msg.naks == [30.0]
    assert out.published == [] and out.js.published == []
    assert len(out.sink.finished) == 1  # the run itself finished; only its result was withheld


async def test_a_failed_result_publish_is_retried_too():
    msg = FakeMsg(upload().SerializeToString())
    await consume([msg], ScenarioGateway.from_file(DEFAULT_SCENARIO), publish_fails=True)
    assert msg.events == ["nak"] and msg.naks == [30.0]


async def test_at_max_deliver_it_dead_letters_and_publishes_max_deliver_exceeded():
    raw = upload().SerializeToString()
    msg = FakeMsg(raw, num_delivered=documents_consumer.MAX_DELIVER)
    out = await consume([msg], failing_gateway())
    assert documents_consumer.MAX_DELIVER == 5
    assert msg.count("term") == 1 and msg.count("ack") == 0 and msg.naks == []
    assert out.js.published == [("dlq.document.uploaded", raw)]
    failed, = out.published
    assert isinstance(failed, documents_pb2.DocumentFailed)
    (identity, _b, _p), = out.sink.started
    assert (failed.document_id, failed.firm_id, failed.run_id, failed.reason_code) == (
        DOC, FIRM, identity.run_id, "max_deliver_exceeded")


async def test_an_undecodable_message_is_dead_lettered_at_once():
    msg = FakeMsg(b"\x0a\xff", num_delivered=1)  # field 1, length 255, truncated
    out = await consume([msg], FakeGateway({}))
    assert msg.events == ["term"]
    assert out.js.published == [("dlq.document.uploaded", b"\x0a\xff")]
    assert out.published == [] and out.sink.started == []


# ------------------------------------------------------------------ heartbeat
class ManualTimer:
    """A fake clock for the heartbeat: sleep(s) returns only when advance() moves time past it."""

    def __init__(self) -> None:
        self.now = 0.0
        self.requested: list[float] = []
        self._sleepers: list[tuple[float, asyncio.Future[None]]] = []

    async def sleep(self, s: float) -> None:
        self.requested.append(s)
        entry = (self.now + s, asyncio.get_running_loop().create_future())
        self._sleepers.append(entry)
        try:
            await entry[1]
        finally:
            self._sleepers.remove(entry)

    @property
    def pending(self) -> int:
        return len(self._sleepers)

    async def advance(self, s: float) -> None:
        self.now += s
        for due, fut in list(self._sleepers):
            if due <= self.now and not fut.done():
                fut.set_result(None)
        for _ in range(20):
            await asyncio.sleep(0)


async def test_the_heartbeat_fires_while_a_run_is_held_open_and_never_after_it_returns():
    timer = ManualTimer()
    started, release = asyncio.Event(), asyncio.Event()

    async def slow(key: str) -> bytes:
        started.set()
        await release.wait()
        return PDF

    msg = FakeMsg(upload().SerializeToString())

    async def drive() -> None:
        await asyncio.wait_for(started.wait(), 5)
        await timer.advance(19.9)
        assert msg.count("in_progress") == 0
        await timer.advance(0.2)  # 20.1 s into the run
        assert msg.count("in_progress") == 1
        await timer.advance(20.0)
        assert msg.count("in_progress") == 2
        release.set()

    await consume([msg], ScenarioGateway.from_file(DEFAULT_SCENARIO), fetch=slow, sleep=timer.sleep,
                  until=drive)
    assert set(timer.requested) == {documents_consumer.HEARTBEAT_S} and documents_consumer.HEARTBEAT_S == 20.0
    assert msg.events[-1] == "ack" and msg.count("ack") == 1
    beats = msg.count("in_progress")
    await timer.advance(500.0)
    assert msg.count("in_progress") == beats  # the heartbeat ended with the run
    assert timer.pending == 0


# ------------------------------------------------------------------ concurrency and shutdown
async def test_runs_are_concurrent_up_to_max_ack_pending():
    in_flight = peak = 0
    release = asyncio.Event()

    async def held(key: str) -> bytes:
        nonlocal in_flight, peak
        in_flight += 1
        peak = max(peak, in_flight)
        await release.wait()
        in_flight -= 1
        return PDF

    msgs = [FakeMsg(upload(f"00000000-0000-4000-8000-00000000d00{i}").SerializeToString()) for i in range(5)]

    async def drive() -> None:
        for _ in range(200):
            if peak == 2:
                break
            await asyncio.sleep(0.005)
        await asyncio.sleep(0.05)
        release.set()

    out = await consume(msgs, ScenarioGateway.from_file(DEFAULT_SCENARIO), fetch=held,
                        settings=Settings(docs_max_ack_pending=2), until=drive)
    assert peak == 2 and all(b <= 2 for b in out.js.sub.batches)
    assert all(m.events == ["ack"] for m in msgs) and len(out.published) == 5


async def test_shutdown_cancels_a_run_that_outlives_the_grace_and_naks_it_at_once(monkeypatch):
    monkeypatch.setattr(documents_consumer, "SHUTDOWN_GRACE_S", 0.05)
    started = asyncio.Event()

    async def forever(key: str) -> bytes:
        started.set()
        await asyncio.Event().wait()
        return PDF

    msg = FakeMsg(upload().SerializeToString())
    stop = asyncio.Event()
    js = FakeJS(FakeSub([msg]))
    published: list[object] = []

    async def publish(m: object) -> None:
        published.append(m)

    task = asyncio.create_task(documents_consumer.run(
        js, gateway=ScenarioGateway.from_file(DEFAULT_SCENARIO), registry=registry(),
        verifier=bootstrap.build_verifier(Settings()), fetch=forever, publish=publish, settings=Settings(),
        stop_event=stop, sink=MemorySink(), sleep=no_sleep))
    await asyncio.wait_for(started.wait(), 5)
    stop.set()
    await asyncio.wait_for(task, 5)
    assert msg.events == ["nak"] and msg.naks == [None]
    assert published == []


@pytest.mark.parametrize("attempt", [1, 4])
async def test_below_max_deliver_a_failure_is_never_dead_lettered(attempt):
    msg = FakeMsg(upload().SerializeToString(), num_delivered=attempt)
    out = await consume([msg], failing_gateway())
    assert msg.count("term") == 0 and out.js.published == []
