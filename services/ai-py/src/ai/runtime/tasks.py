"""Task handlers for agent.task.requested (contract section 3.8).

The proto type is imported for type checking only, so the runtime core stays importable without generated
code (finding F7). Budget conversion and dispatch live in the service consumer (ai/service/tasks.py).
"""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import TYPE_CHECKING

from ai.runtime.graph import TaskGraph
from ai.runtime.types import Budget

if TYPE_CHECKING:
    from ai.gen.compliance.v1 import agents_pb2


@dataclass(frozen=True, slots=True)
class TaskPlan:
    graph: TaskGraph
    workflow_subject: tuple[str, str] | None = None  # (subject_type, subject_id); None -> from the request
    output_node: str = ""  # node whose result (a protobuf Message) is packed into AgentTaskCompleted.output
    budget: Budget | None = None  # None -> the request's budget when set, else Budget()


type TaskHandler = Callable[["agents_pb2.AgentTaskRequested"], Awaitable[TaskPlan]]
