"""_handle must isolate failures: neither the original processing error nor a
failure while handling that error (DLQ publish, msg.term(), msg.nak()) may
escape and kill the worker loop. A permanently-failed message (num_delivered
>= MAX_DELIVER) must always be term()'d, even if the DLQ publish fails after
retries -- otherwise JetStream will never redeliver it (max_deliver reached)
and it is neither dead-lettered, acked, nor termed: silently stranded.
"""

from ai.gen.compliance.v1 import events_pb2, invoice_pb2
from ai.worker import DLQ_PUBLISH_ATTEMPTS, DLQ_SUBJECT, MAX_DELIVER, _handle


class _FakeMetadata:
    def __init__(self, num_delivered: int) -> None:
        self.num_delivered = num_delivered


class _RecordingMsg:
    """Fake JetStream message that records every ack/term/nak call."""

    def __init__(
        self,
        data: bytes,
        num_delivered: int,
        headers: dict[str, str] | None = None,
        term_raises: bool = False,
        nak_raises: bool = False,
    ) -> None:
        self.data = data
        self.headers = headers
        self.metadata = _FakeMetadata(num_delivered)
        self.ack_calls: int = 0
        self.term_calls: int = 0
        self.nak_calls: list[float | None] = []
        self._term_raises = term_raises
        self._nak_raises = nak_raises

    async def ack(self) -> None:
        # Simulate the processing step itself failing after a good parse, so
        # every test below exercises the failure path in _fail()/_dead_letter().
        raise RuntimeError("boom: ack failed")

    async def term(self) -> None:
        self.term_calls += 1
        if self._term_raises:
            raise RuntimeError("boom: term failed")

    async def nak(self, delay: float | None = None) -> None:
        self.nak_calls.append(delay)
        if self._nak_raises:
            raise RuntimeError("boom: nak failed")


class _RecordingJS:
    """Fake JetStream publisher that records every publish call. Publishes to
    the DLQ subject fail the first `dlq_fail_times` attempts before
    succeeding (or fail forever); publishes to any other subject (e.g. the
    happy-path `invoice.extracted` publish that runs before the message
    fails) always succeed, so DLQ-retry behaviour can be asserted in
    isolation.
    """

    def __init__(self, dlq_fail_times: int = 0) -> None:
        self.publish_calls: list[tuple[str, bytes]] = []
        self.dlq_publish_calls: list[tuple[str, bytes]] = []
        self._dlq_fail_times = dlq_fail_times

    async def publish(self, subject: str, payload: bytes, headers: dict[str, str] | None = None) -> None:
        self.publish_calls.append((subject, payload))
        if subject == DLQ_SUBJECT:
            self.dlq_publish_calls.append((subject, payload))
            if len(self.dlq_publish_calls) <= self._dlq_fail_times:
                raise RuntimeError("boom: dlq publish failed")


def _submitted_event() -> events_pb2.InvoiceSubmitted:
    inv = invoice_pb2.Invoice(invoice_number="INV-1", seller_trn="123", total_amount="1050.00")
    return events_pb2.InvoiceSubmitted(invoice_id="i-1", firm_id="f-1", invoice=inv)


async def test_dlq_publish_fails_every_attempt_but_term_is_still_called():
    """(a) publish fails (on every retry) -> term() must still be called."""
    msg = _RecordingMsg(data=_submitted_event().SerializeToString(), num_delivered=MAX_DELIVER)
    js = _RecordingJS(dlq_fail_times=DLQ_PUBLISH_ATTEMPTS)  # fails all attempts

    await _handle(js, msg)

    assert len(js.dlq_publish_calls) == DLQ_PUBLISH_ATTEMPTS
    assert msg.term_calls == 1


async def test_dlq_publish_succeeds_but_term_raises_handle_does_not_raise():
    """(b) publish succeeds but term() raises -> _handle does not raise."""
    msg = _RecordingMsg(
        data=_submitted_event().SerializeToString(), num_delivered=MAX_DELIVER, term_raises=True
    )
    js = _RecordingJS(dlq_fail_times=0)

    await _handle(js, msg)  # must not raise

    assert len(js.dlq_publish_calls) == 1
    assert msg.term_calls == 1


async def test_dlq_publish_fails_once_then_succeeds_on_retry():
    """(c) publish fails on the first attempt, succeeds on retry -> the
    message is published to the DLQ exactly once successfully (after one
    retry) and is termed.
    """
    msg = _RecordingMsg(data=_submitted_event().SerializeToString(), num_delivered=MAX_DELIVER)
    js = _RecordingJS(dlq_fail_times=1)  # first attempt fails, second succeeds

    await _handle(js, msg)

    assert len(js.dlq_publish_calls) == 2
    assert msg.term_calls == 1


async def test_handle_does_not_raise_when_nak_fails():
    """A transient failure (num_delivered < MAX_DELIVER) naks for redelivery;
    if nak() itself raises, _handle must still not raise.
    """
    msg = _RecordingMsg(data=_submitted_event().SerializeToString(), num_delivered=1, nak_raises=True)
    js = _RecordingJS(dlq_fail_times=0)

    await _handle(js, msg)  # must not raise

    assert msg.nak_calls == [2]  # nak() was attempted (and recorded) before it raised
