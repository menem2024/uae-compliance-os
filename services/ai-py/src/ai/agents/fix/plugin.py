"""Entry point `compliance.agents: fix` (spec 5.7.1): registers the agent, the `validate_invoice` tool, the
task handler for agent `fix` and the `FixCandidate` verifier profile.

`register()` never touches the environment: the validator address, the model and the mode flags are read when
a task runs (finding F12). Tests pass `validator=` / `config=`.
"""

from __future__ import annotations

from collections.abc import Callable

from ai.agents.fix.agent import FixAgent, make_validate_tool
from ai.agents.fix.config import FixConfig
from ai.agents.fix.graph import FixTaskHandler
from ai.agents.fix.validator_client import LazyValidator, ValidatorLike
from ai.agents.fix.verifier_profile import fix_profile
from ai.agents.verifier.agent import VerifierAgent
from ai.runtime.registry import AgentRegistry


def register(reg: AgentRegistry, *, validator: ValidatorLike | None = None,
             config: Callable[[], FixConfig] = FixConfig.from_env) -> None:
    profile = fix_profile()
    reg.add_agent(FixAgent())
    reg.add_tool(make_validate_tool(validator if validator is not None else LazyValidator()))
    reg.add_task_handler(FixAgent.name, FixTaskHandler(VerifierAgent([profile]), config))
    reg.add_verifier_profile(profile)
