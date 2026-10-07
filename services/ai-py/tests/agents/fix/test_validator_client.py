"""The real ValidatorClient against an in-process grpc.aio server on loopback (the network guard allows 127.0.0.1)."""

import asyncio

import grpc
import pytest

from ai.agents.fix.validator_client import (
    DEFAULT_ADDR,
    FakeValidatorClient,
    LazyValidator,
    ValidatorClient,
    ValidatorFailed,
)
from ai.gen.compliance.v1 import invoice_pb2, validator_pb2, validator_pb2_grpc
from ai.runtime.errors import ToolTransientError


class Servicer(validator_pb2_grpc.ValidatorServiceServicer):
    def __init__(self) -> None:
        self.code: grpc.StatusCode | None = None
        self.delay_s = 0.0
        self.seen: list[validator_pb2.ValidateRequest] = []

    async def Validate(self, request, context):
        self.seen.append(request)
        if self.delay_s:
            await asyncio.sleep(self.delay_s)
        if self.code is not None:
            await context.abort(self.code, "scripted")
        return validator_pb2.ValidateResponse(run=validator_pb2.ValidationRun(
            ruleset_version="pint-ae@test",
            issues=[validator_pb2.ValidationIssue(rule_id="r1", path="currency")]))


@pytest.fixture
async def server():
    svc = Servicer()
    srv = grpc.aio.server()
    validator_pb2_grpc.add_ValidatorServiceServicer_to_server(svc, srv)
    port = srv.add_insecure_port("127.0.0.1:0")
    await srv.start()
    yield svc, f"127.0.0.1:{port}"
    await srv.stop(None)


async def test_validate_returns_the_run_and_sends_the_invoice_and_version(server):
    svc, addr = server
    client = ValidatorClient(addr)
    run = await client.validate(invoice_pb2.Invoice(currency="aed"), "pint-ae@test")
    await client.close()
    assert [i.rule_id for i in run.issues] == ["r1"] and run.ruleset_version == "pint-ae@test"
    assert svc.seen[0].invoice.currency == "aed" and svc.seen[0].ruleset_version == "pint-ae@test"


@pytest.mark.parametrize("code", [grpc.StatusCode.UNAVAILABLE, grpc.StatusCode.RESOURCE_EXHAUSTED])
async def test_transient_statuses_raise_tool_transient_error(server, code):
    svc, addr = server
    svc.code = code
    client = ValidatorClient(addr)
    with pytest.raises(ToolTransientError):
        await client.validate(invoice_pb2.Invoice(), "")
    await client.close()


async def test_deadline_exceeded_raises_tool_transient_error(server):
    svc, addr = server
    svc.delay_s = 1.0
    client = ValidatorClient(addr, timeout_s=0.05)
    with pytest.raises(ToolTransientError):
        await client.validate(invoice_pb2.Invoice(), "")
    await client.close()


async def test_connection_refused_is_unavailable_and_transient():
    client = ValidatorClient("127.0.0.1:1", timeout_s=1.0)  # nothing listens on port 1
    with pytest.raises(ToolTransientError):
        await client.validate(invoice_pb2.Invoice(), "")
    await client.close()


async def test_invalid_argument_raises_value_error(server):
    svc, addr = server
    svc.code = grpc.StatusCode.INVALID_ARGUMENT
    client = ValidatorClient(addr)
    with pytest.raises(ValueError, match="rejected"):
        await client.validate(invoice_pb2.Invoice(), "nope@1")
    await client.close()


async def test_other_statuses_are_a_validator_failure_not_a_retry(server):
    svc, addr = server
    svc.code = grpc.StatusCode.INTERNAL
    client = ValidatorClient(addr)
    with pytest.raises(ValidatorFailed):
        await client.validate(invoice_pb2.Invoice(), "")
    await client.close()


async def test_the_address_is_read_lazily_at_the_first_call(server):
    _, addr = server
    env: dict[str, str] = {}
    lazy = LazyValidator(env=lambda: env)  # built before VALIDATOR_ADDR exists
    env["VALIDATOR_ADDR"] = addr
    run = await lazy.validate(invoice_pb2.Invoice(), "")
    assert run.ruleset_version == "pint-ae@test"


def test_the_default_address_is_the_compose_service():
    assert DEFAULT_ADDR == "validator-rs:50051"


async def test_fake_client_records_calls_and_replays_a_script():
    ok = validator_pb2.ValidationRun(ruleset_version="a")
    fake = FakeValidatorClient([ToolTransientError("down"), ok])
    with pytest.raises(ToolTransientError):
        await fake.validate(invoice_pb2.Invoice(currency="x"), "v1")
    assert (await fake.validate(invoice_pb2.Invoice(), "v2")).ruleset_version == "a"
    assert (await fake.validate(invoice_pb2.Invoice(), "v3")).ruleset_version == "a"  # the last repeats
    assert [v for _, v in fake.calls] == ["v1", "v2", "v3"] and fake.calls[0][0].currency == "x"
