"""Static rules of the Fix package (spec 5.7.1, plan Task 21 review focus) and registration behaviour."""

import ast
import os
import socket
from pathlib import Path

import grpc
import pytest
from fixhelpers import llm_answer, make_invoice, task_input

import ai.agents.fix as fix_pkg
from ai.agents.fix.validator_client import LazyValidator
from ai.runtime.registry import AgentRegistry, load_registry
from ai.runtime.types import RunStatus

PKG = Path(fix_pkg.__file__).parent
FILES = sorted(PKG.glob("*.py"))
ALLOWED = ("ai.agents.fix", "ai.agents.verifier", "ai.runtime", "ai.gen", "ai.settings", "ai.gateway.types",
           "ai.gateway.errors", "grpc", "pydantic", "google.protobuf", "__future__")
STDLIB = {"json", "os", "re", "uuid", "dataclasses", "collections", "typing", "pathlib"}


def _imports(path: Path) -> set[str]:
    out: set[str] = set()
    for n in ast.walk(ast.parse(path.read_text(encoding="utf-8"))):
        if isinstance(n, ast.Import):
            out.update(a.name for a in n.names)
        elif isinstance(n, ast.ImportFrom) and n.module and n.level == 0:
            out.add(n.module)
    return out


def test_the_package_imports_only_the_runtime_gateway_types_verifier_gen_grpc_pydantic_and_stdlib():
    bad: list[str] = []
    for f in FILES:
        extra = ("ai.evals",) if f.name == "evals.py" else ()
        for m in _imports(f):
            top = m.split(".")[0]
            if not (m.startswith((*ALLOWED, *extra)) or top in STDLIB):
                bad.append(f"{f.name}: {m}")
    assert bad == []


def test_no_provider_sdk_and_no_gateway_adapter_is_imported():
    mods = {m for f in FILES for m in _imports(f)}
    assert not any(m.split(".")[0] in {"anthropic", "httpx", "httpx2", "openai"} for m in mods)
    assert not any(m.startswith(("ai.gateway.anthropic_gw", "ai.gateway.openai_compat_gw", "ai.gateway.factory"))
                   for m in mods)


def test_no_float_in_the_fix_package_except_confidence_and_timeouts_and_eval_ratios():
    """Money is a decimal string everywhere (contract rule 5). The only floats are the confidence probabilities,
    the gRPC timeout, and the eval metric ratios (CaseScore.metrics is Mapping[str, float])."""
    hits: list[str] = []
    for f in FILES:
        if f.name == "evals.py":
            continue
        for n, line in enumerate(f.read_text(encoding="utf-8").splitlines(), 1):
            code = line.split("#")[0]
            if "float" in code and "confidence" not in code and "timeout_s" not in code:
                hits.append(f"{f.name}:{n}: {line.strip()}")
    assert hits == []


def test_the_nodes_never_write_business_state():
    """Rule 3: an agent reads, returns typed output and proposes. No database, file or HTTP client in the nodes."""
    banned = {"psycopg", "asyncpg", "sqlalchemy", "requests", "minio", "nats", "redis"}
    mods = {m.split(".")[0] for f in FILES for m in _imports(f)}
    assert not mods & banned


def test_register_does_not_touch_the_environment(monkeypatch: pytest.MonkeyPatch):
    class Boom(dict):
        def __getitem__(self, k):
            raise AssertionError(f"environ[{k!r}] read in register()")

        def get(self, k, default=None):
            raise AssertionError(f"environ.get({k!r}) read in register()")

        def __contains__(self, k):
            raise AssertionError(f"{k!r} in environ checked in register()")

    monkeypatch.setattr(os, "environ", Boom())
    reg = AgentRegistry()
    fix_pkg.register(reg)
    assert set(reg.agents) == {"fix"} and "fix" in reg.task_handlers
    assert reg.tools.get("validate_invoice").spec.side_effects == "read"


def test_the_entry_point_registers_agent_tool_handler_and_profile():
    reg = load_registry()
    assert "fix" in reg.agents and "fix" in reg.task_handlers
    assert reg.tools.get("validate_invoice").spec.name == "validate_invoice"
    from ai.agents.fix.types import FixCandidate

    assert FixCandidate in reg.verifier_profiles
    assert reg.agents["fix"].tools == ()  # the model gets no tools


def test_a_lazy_validator_reads_no_address_until_it_is_used():
    env_reads: list[str] = []

    class Env(dict):
        def get(self, k, default=None):
            env_reads.append(k)
            return super().get(k, default)

    LazyValidator(env=lambda: Env())
    assert env_reads == []


async def test_a_full_task_makes_zero_network_calls(harness, monkeypatch: pytest.MonkeyPatch):
    calls: list[object] = []
    monkeypatch.setattr(socket.socket, "connect", lambda self, addr: calls.append(addr))
    monkeypatch.setattr(socket.socket, "connect_ex", lambda self, addr: calls.append(addr) or 0)
    monkeypatch.setattr(grpc.aio, "insecure_channel", lambda *a, **k: calls.append(a))
    h = harness(answer=llm_answer(("seller.postal_address.country_subdivision", "SHJ")))
    inv = make_invoice(seller__postal_address__country_subdivision="", totals__payable_amount="5.00")
    out = await h.run(task_input(inv))
    assert out.status is RunStatus.SUCCEEDED and out.results["fix.result"].outcome == "proposed"
    assert calls == []
