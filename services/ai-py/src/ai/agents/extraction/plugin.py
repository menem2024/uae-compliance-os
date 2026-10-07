"""Entry point `compliance.agents: extraction` (spec amendment 4)."""

from ai.agents.extraction.agent import ExtractionAgent
from ai.runtime.registry import AgentRegistry
from ai.settings import Settings


def register(reg: AgentRegistry) -> None:
    reg.add_agent(ExtractionAgent(model=Settings.from_env().model_extraction))
