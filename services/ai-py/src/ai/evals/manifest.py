"""Recording manifests: `<recordings>/<suite>/MANIFEST.json` says which prompt versions the recordings were made
with. A stale or missing manifest keeps `auto` mode on the fake gateway (never a silent pass)."""

from __future__ import annotations

import json
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path

from ai.agents.extraction import prompts as extraction_prompts
from ai.agents.intake import agent as intake_agent
from ai.agents.verifier import critic as verifier_critic
from ai.canonical import canonical_json

MANIFEST_NAME = "MANIFEST.json"

# prompt id -> current version, for every prompt id an eval suite lists in `Suite.prompt_ids`.
PROMPT_VERSIONS: dict[str, int] = {
    extraction_prompts.PROMPT_ID: extraction_prompts.PROMPT_VERSION,
    extraction_prompts.REVISE_PROMPT_ID: extraction_prompts.PROMPT_VERSION,
    intake_agent.PROMPT_ID: intake_agent.PROMPT_VERSION,
    verifier_critic.CRITIC_PROMPT_ID: verifier_critic.PROMPT_VERSION,
    verifier_critic.ESCALATION_PROMPT_ID: verifier_critic.PROMPT_VERSION,
}


@dataclass(frozen=True, slots=True)
class Manifest:
    suite: str
    prompt_versions: dict[str, int]
    recorded_at: str


def prompt_versions(prompt_ids: tuple[str, ...]) -> dict[str, int]:
    """Current version of each prompt id; an unregistered id is a harness bug, not a stale recording."""
    return {pid: PROMPT_VERSIONS[pid] for pid in prompt_ids}


def manifest_path(root: Path, suite: str) -> Path:
    return root / suite / MANIFEST_NAME


def load_manifest(root: Path, suite: str) -> Manifest | None:
    try:
        d = json.loads(manifest_path(root, suite).read_text(encoding="utf-8"))
        return Manifest(suite=str(d["suite"]), prompt_versions={str(k): int(v) for k, v in
                                                                 d["prompt_versions"].items()},
                        recorded_at=str(d["recorded_at"]))
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        return None


def matches_current(m: Manifest | None, current: Mapping[str, int]) -> bool:
    """True when the recordings were made with exactly the suite's current prompt versions."""
    return m is not None and m.prompt_versions == dict(current)


def write_manifest(root: Path, suite: str, versions: Mapping[str, int]) -> Manifest:
    m = Manifest(suite=suite, prompt_versions=dict(versions),
                 recorded_at=datetime.now(UTC).isoformat(timespec="seconds"))
    path = manifest_path(root, suite)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(canonical_json({"suite": m.suite, "prompt_versions": m.prompt_versions,
                                    "recorded_at": m.recorded_at}) + "\n", encoding="utf-8")
    return m
