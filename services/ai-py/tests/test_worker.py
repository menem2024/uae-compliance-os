"""_handle must isolate failures: neither the original processing error nor a
failure while handling that error (DLQ publish, msg.term(), msg.nak()) may
escape and kill the worker loop.
"""

from ai.gen.compliance.v1 import events_pb2, invoice_pb2
from ai.worker import MAX_DELIVER, _handle


class _FakeMetadata:
    def __init__(self, num_delivered: int) -> None:
        self.num_delivered = num_delivered


class _FakeMsg:
    def __init__(self, data: bytes, num_delivered: int, headers: dict[str, str] | None = None) -> None:
        self.data = data
        self.headers = headers
        self.metadata = _FakeMetadata(num_delivered)

    async def ack(self) -> None:
        # Simulate the processing step itself failing after a good parse.
        raise RuntimeError("boom: ack failed")

    async def term(self) -> None:
        raise RuntimeError("boom: term failed")

    async def nak(self, delay: float | None = None) -> None:
        raise RuntimeError("boom: nak failed")


class _FakeJSPublishFails:
    async def publish(self, subject: str, payload: bytes, headers: dict[str, str] | None = None) -> None:
        raise RuntimeError("boom: publish failed")


class _FakeJSPublishOk:
    async def publish(self, subject: str, payload: bytes, headers: dict[str, str] | None = None) -> None:
        return None


def _submitted_event() -> events_pb2.InvoiceSubmitted:
    inv = invoice_pb2.Invoice(invoice_number="INV-1", seller_trn="123", total_amount="1050.00")
    return events_pb2.InvoiceSubmitted(invoice_id="i-1", firm_id="f-1", invoice=inv)


async def test_handle_does_not_raise_when_dlq_publish_and_term_both_fail():
    msg = _FakeMsg(data=_submitted_event().SerializeToString(), num_delivered=MAX_DELIVER)
    js = _FakeJSPublishFails()

    # ack() raises -> enters the failure path; num_delivered >= MAX_DELIVER so
    # it tries js.publish() to the DLQ (raises) then msg.term() (would also
    # raise) -- none of that may escape _handle.
    await _handle(js, msg)


async def test_handle_does_not_raise_when_nak_fails():
    msg = _FakeMsg(data=_submitted_event().SerializeToString(), num_delivered=1)
    js = _FakeJSPublishOk()

    # ack() raises -> enters the failure path; num_delivered < MAX_DELIVER so
    # it takes the nak() branch, which also raises -- must not escape.
    await _handle(js, msg)
