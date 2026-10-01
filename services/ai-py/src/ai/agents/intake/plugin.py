"""Entry point `compliance.agents: intake` (spec amendment 4)."""

from ai.agents.intake.agent import IntakeAgent
from ai.runtime.registry import AgentRegistry
from ai.settings import Settings


def register(reg: AgentRegistry) -> None:
    reg.add_agent(IntakeAgent(model=Settings.from_env().model_intake))
