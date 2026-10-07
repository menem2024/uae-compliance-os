"""Fix agent configuration, read from the environment when a task arrives (never in `register()`, F12)."""

from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass

from ai.gateway.types import MODEL_IDS, SONNET, ModelId
from ai.settings import Settings


@dataclass(frozen=True, slots=True)
class FixConfig:
    model: ModelId = SONNET  # AI_MODEL_FIX (agent-runtime contract section 1: "Fix (C)" row)
    auto_llm: bool = False  # FIX_AUTO_LLM=true lets mode "auto" call the model (default: protect the budget)
    # True when the gateway is the canned fake one (AI_GATEWAY=fake without AI_FAKE_RESULTS=accept): its
    # output must never turn into a proposal, so the LLM step is skipped.
    untrusted_llm: bool = False

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> FixConfig:
        e = os.environ if env is None else env
        model = e.get("AI_MODEL_FIX", "") or SONNET
        if model not in MODEL_IDS:
            raise ValueError(f"AI_MODEL_FIX={model!r}: expected one of {', '.join(MODEL_IDS)}")
        s = Settings.from_env(e)
        return cls(model=model, auto_llm=e.get("FIX_AUTO_LLM", "") == "true",  # type: ignore[arg-type]
                   untrusted_llm=s.gateway == "fake" and s.fake_results == "review")

    def llm_allowed(self, mode: str) -> bool:
        """`on_demand` always asks the model; `auto` only with FIX_AUTO_LLM=true; never an untrusted one."""
        if self.untrusted_llm:
            return False
        return mode == "on_demand" or (mode == "auto" and self.auto_llm)
