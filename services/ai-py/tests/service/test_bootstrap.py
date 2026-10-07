"""Service bootstrap: streams, registry, Verifier wiring, the document result publisher, dead-lettering."""

import dataclasses

import pytest
from nats.js.api import RetentionPolicy, StorageType, StreamConfig
from nats.js.errors import BadRequestError

from ai.agents.extraction.schema import ExtractionOutput
from ai.agents.verifier.critic import CRITIC_PROMPT_ID, ESCALATION_PROMPT_ID, InvoiceCritic
from ai.gateway.types import HAIKU, OPUS, SONNET
from ai.gen.compliance.v1 import documents_pb2
from ai.service import bootstrap
from ai.settings import Settings

DAY = 86400.0


class StreamJS:
    """Mimics JetStream stream creation: an identical create is a no-op, a different config is 10058."""

    def __init__(self, existing: dict[str, StreamConfig] | None = None) -> None:
        self.streams: dict[str, StreamConfig] = dict(existing or {})
        self.creates: list[str] = []

    async def add_stream(self, config: StreamConfig | None = None, **params: object) -> object:
        assert config is not None and not params
        self.creates.append(config.name or "")
        have = self.streams.get(config.name or "")
        if have is not None and have != config:
            raise BadRequestError(code=400, err_code=10058,
                                  description="stream name already in use with a different configuration")
        self.streams[config.name or ""] = config
        return object()


async def test_ensure_streams_matches_api_go_and_is_idempotent():
    js = StreamJS()
    await bootstrap.ensure_streams(js)
    await bootstrap.ensure_streams(js)  # second call does not raise
    docs, agents, dlq = js.streams["DOCUMENTS"], js.streams["AGENTS"], js.streams["DLQ"]
    assert (docs.subjects, docs.max_age, docs.duplicate_window) == (["document.>"], 30 * DAY, 120.0)
    assert (agents.subjects, agents.max_age, agents.duplicate_window) == (["agent.>"], 7 * DAY, 120.0)
    assert dlq.subjects == ["dlq.>"]
    for s in (docs, agents, dlq):
        assert s.storage is StorageType.FILE and s.retention is RetentionPolicy.LIMITS


async def test_a_stream_api_go_created_first_with_other_settings_is_accepted():
    created_by_go = StreamConfig(name="DOCUMENTS", subjects=["document.>"], storage=StorageType.FILE,
                                 max_age=30 * DAY, duplicate_window=120.0, allow_direct=True)
    js = StreamJS({"DOCUMENTS": created_by_go})
    await bootstrap.ensure_streams(js)
    assert js.streams["DOCUMENTS"] is created_by_go and {"AGENTS", "DLQ"} <= set(js.streams)


async def test_any_other_stream_error_propagates():
    class Broken(StreamJS):
        async def add_stream(self, config=None, **params):
            raise BadRequestError(code=400, err_code=10052, description="insufficient resources")

    with pytest.raises(BadRequestError):
        await bootstrap.ensure_streams(Broken())


def test_build_verifier_registers_exactly_the_invoice_profile():
    s = Settings(model_critic=HAIKU, model_escalation=OPUS)
    v = bootstrap.build_verifier(s)
    assert list(v._profiles) == [ExtractionOutput]  # type: ignore[attr-defined]
    p = v.profile(ExtractionOutput)
    assert isinstance(p.critic, InvoiceCritic) and isinstance(p.escalation_critic, InvoiceCritic)
    assert (p.critic.model, p.critic.prompt_id) == (HAIKU, CRITIC_PROMPT_ID)
    assert (p.escalation_critic.model, p.escalation_critic.prompt_id) == (OPUS, ESCALATION_PROMPT_ID)


def test_build_registry_loads_the_phase1_agents_from_entry_points():
    reg = bootstrap.build_registry(Settings())
    assert {"intake", "extraction"} <= set(reg.agents)
    assert reg.agents["extraction"].model == SONNET  # type: ignore[attr-defined]


class PublishJS:
    def __init__(self, fail: bool = False) -> None:
        self.fail = fail
        self.calls: list[dict] = []

    async def publish(self, subject, payload=b"", timeout=None, stream=None, headers=None):
        if self.fail:
            raise TimeoutError("nats down")
        self.calls.append({"subject": subject, "data": payload, "timeout": timeout, "headers": headers})


async def test_make_publish_uses_the_contract_subjects_and_msg_ids():
    js = PublishJS()
    publish = bootstrap.make_publish(js)
    ext = documents_pb2.DocumentExtracted(document_id="d1", firm_id="f1", run_id="r1",
                                          document_kind="invoice")
    failed = documents_pb2.DocumentFailed(document_id="d2", firm_id="f1", run_id="r2",
                                          reason_code="object_missing")
    await publish(ext)
    await publish(failed)
    assert [(c["subject"], c["headers"]["Nats-Msg-Id"]) for c in js.calls] == [
        ("document.extracted", "document.extracted:d1:r1"), ("document.failed", "document.failed:d2:r2")]
    assert documents_pb2.DocumentExtracted.FromString(js.calls[0]["data"]) == ext
    assert documents_pb2.DocumentFailed.FromString(js.calls[1]["data"]) == failed
    assert all(c["timeout"] for c in js.calls)


async def test_a_failed_result_publish_raises_so_the_message_is_retried():
    publish = bootstrap.make_publish(PublishJS(fail=True))
    with pytest.raises(TimeoutError):
        await publish(documents_pb2.DocumentFailed(document_id="d", run_id="r", reason_code="internal"))
    with pytest.raises(TypeError):
        await bootstrap.make_publish(PublishJS())(documents_pb2.DocumentUploaded())  # type: ignore[arg-type]


@dataclasses.dataclass
class Meta:
    num_delivered: int


class DeadMsg:
    def __init__(self) -> None:
        self.data = b"raw"
        self.headers = {"Nats-Msg-Id": "document.uploaded:d1:n1"}
        self.metadata = Meta(5)
        self.terms = 0

    async def term(self) -> None:
        self.terms += 1


async def test_dead_letter_publishes_the_raw_message_then_terms_even_when_the_dlq_is_down():
    js, msg = PublishJS(), DeadMsg()
    await bootstrap.dead_letter(js, msg, "dlq.document.uploaded")
    assert [(c["subject"], c["data"]) for c in js.calls] == [("dlq.document.uploaded", b"raw")]
    assert msg.terms == 1
    down, msg2 = PublishJS(fail=True), DeadMsg()
    await bootstrap.dead_letter(down, msg2, "dlq.document.uploaded", backoff_s=0)  # never raises
    assert msg2.terms == 1


def test_the_heartbeat_always_beats_well_inside_ack_wait():
    assert bootstrap.heartbeat_interval_s(60) == 20.0  # the contract cadence at the default ack_wait
    assert bootstrap.heartbeat_interval_s(300) == 20.0
    assert bootstrap.heartbeat_interval_s(15) == 5.0  # AI_ACK_WAIT_S may be as low as 10 s
