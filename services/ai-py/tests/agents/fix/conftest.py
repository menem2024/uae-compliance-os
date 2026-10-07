"""Fixtures for the Fix agent tests. The helpers live in `fixhelpers` (importlib mode has no package imports)."""

import sys
from collections.abc import Callable
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parent))

from fixhelpers import Harness, make_harness


@pytest.fixture
def harness() -> Callable[..., Harness]:
    return make_harness
