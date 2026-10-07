from pathlib import Path

import pytest

from ai.synthetic.datasets import EXTRACTION, IMPORTER, INTAKE_EXTRA, VERIFIER, build

REAL_ROOT = Path(__file__).resolve().parents[2] / "evals" / "datasets"


@pytest.fixture(scope="session")
def datasets_root(tmp_path_factory: pytest.TempPathFactory) -> Path:
    """A small regenerated copy of the datasets (first cases only), so tests never depend on the 1-minute build."""
    root = tmp_path_factory.mktemp("datasets")
    build(root, [EXTRACTION], limit=20)
    build(root, [INTAKE_EXTRA], limit=4)
    build(root, [IMPORTER])
    build(root, [VERIFIER], limit=20)
    return root
