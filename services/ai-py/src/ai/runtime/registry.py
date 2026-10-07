"""Agent registry and entry-point loading (contract section 3.4, spec amendment 4).

Each track's agent module exposes `register(reg: AgentRegistry) -> None` and adds one line under
`[project.entry-points."compliance.agents"]` in services/ai-py/pyproject.toml.
"""

from __future__ import annotations

import re
from collections.abc import Callable, Iterable
from importlib.metadata import entry_points
from typing import Any, Protocol

from pydantic import BaseModel

from ai.runtime.agent import Agent
from ai.runtime.tasks import TaskHandler
from ai.runtime.tools import Tool, ToolRegistry

AGENTS_GROUP = "compliance.agents"
_AGENT_NAME = re.compile(r"^[a-z][a-z0-9_]{1,40}$")


class VerifierProfileLike(Protocol):
    """Structural view of ai.agents.verifier.VerifierProfile (the runtime never imports agents)."""

    output_type: type[BaseModel]


class EntryPointLike(Protocol):
    name: str

    def load(self) -> Any: ...


class AgentRegistry:
    def __init__(self) -> None:
        self.agents: dict[str, Agent[Any, Any]] = {}
        self.tools = ToolRegistry()
        self.task_handlers: dict[str, TaskHandler] = {}
        self.verifier_profiles: dict[type[BaseModel], VerifierProfileLike] = {}

    def add_agent(self, agent: Agent[Any, Any]) -> None:
        name = agent.name
        if not _AGENT_NAME.match(name):
            raise ValueError(f"invalid agent name {name!r}")
        if name in self.agents:
            raise ValueError(f"duplicate agent {name}")
        self.agents[name] = agent

    def add_tool(self, tool: Tool) -> None:
        self.tools.register(tool)

    def add_task_handler(self, agent_name: str, handler: TaskHandler) -> None:
        if agent_name in self.task_handlers:
            raise ValueError(f"duplicate task handler for {agent_name}")
        self.task_handlers[agent_name] = handler

    def add_verifier_profile(self, profile: VerifierProfileLike) -> None:
        if profile.output_type in self.verifier_profiles:
            raise ValueError(f"duplicate verifier profile for {profile.output_type.__name__}")
        self.verifier_profiles[profile.output_type] = profile


def load_registry(group: str = AGENTS_GROUP, *, eps: Iterable[EntryPointLike] | None = None) -> AgentRegistry:
    """Loads every entry point of `group` in name order. A broken entry point fails start-up (fail fast)."""
    reg = AgentRegistry()
    found = list(eps) if eps is not None else list(entry_points(group=group))
    for ep in sorted(found, key=lambda e: e.name):
        register: Callable[[AgentRegistry], None] = ep.load()
        register(reg)
    return reg
