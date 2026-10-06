"""AC-4 and contract rule 1: only ai/gateway/anthropic_gw.py imports the anthropic SDK."""

import ast
from pathlib import Path

SRC = Path(__file__).resolve().parents[1] / "src" / "ai"


def _modules(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"))
    out: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            out.update(a.name for a in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module and node.level == 0:
            out.add(node.module)
            out.update(f"{node.module}.{a.name}" for a in node.names)
    return out


def _sources() -> dict[str, set[str]]:
    return {p.relative_to(SRC).as_posix(): _modules(p) for p in SRC.rglob("*.py") if "gen" not in p.parts}


def test_only_the_adapter_imports_anthropic():
    offenders = [rel for rel, mods in _sources().items()
                 if any(m == "anthropic" or m.startswith("anthropic.") for m in mods)]
    assert offenders == ["gateway/anthropic_gw.py"]


def test_adapter_is_reached_only_through_the_factory_and_eval_runner():
    allowed = {"gateway/factory.py", "evals/runner.py"}
    users = {rel for rel, mods in _sources().items() if "ai.gateway.anthropic_gw" in mods}
    assert users <= allowed


def test_only_the_openai_compat_adapter_imports_httpx():
    offenders = [rel for rel, mods in _sources().items()
                 if any(m in ("httpx", "httpx2") or m.startswith(("httpx.", "httpx2.")) for m in mods)]
    assert offenders == ["gateway/openai_compat_gw.py"]


def test_openai_compat_adapter_is_reached_only_through_the_factory():
    users = {rel for rel, mods in _sources().items() if "ai.gateway.openai_compat_gw" in mods}
    assert users <= {"gateway/factory.py"}


def test_gateway_never_imports_the_runtime():
    offenders = [rel for rel, mods in _sources().items()
                 if rel.startswith("gateway/") and any(m.startswith("ai.runtime") for m in mods)]
    assert offenders == []
