"""fpdf2, uharfbuzz and Pillow are dev dependencies: only ai.synthetic and ai.evals may import them, and
nothing the service runs may import ai.synthetic (the production image is built with --no-dev)."""

import ast
from pathlib import Path

SRC = Path(__file__).resolve().parents[2] / "src" / "ai"
DEV_ONLY = ("fpdf", "uharfbuzz", "PIL", "ai.synthetic")
ALLOWED = ("synthetic/", "evals/")


def _imports(path: Path) -> set[str]:
    out: set[str] = set()
    for node in ast.walk(ast.parse(path.read_text(encoding="utf-8"))):
        if isinstance(node, ast.Import):
            out.update(a.name for a in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module and node.level == 0:
            out.add(node.module)
    return out


def test_dev_only_packages_stay_out_of_the_service():
    offenders = []
    for p in SRC.rglob("*.py"):
        rel = p.relative_to(SRC).as_posix()
        if "gen" in p.parts or rel.startswith(ALLOWED):
            continue
        bad = [m for m in _imports(p) if any(m == d or m.startswith(d + ".") for d in DEV_ONLY)]
        if bad:
            offenders.append((rel, bad))
    assert offenders == []
