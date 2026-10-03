"""The `ai-agent-tasks` consumer: `agent.task.requested` -> a TaskPlan run -> `agent.task.completed.<agent>`
(contract sections 3.8 and 4).

- an `agent` with no task handler (or an undecodable request) is dead-lettered to dlq.agent.task;
- a handler raising ValueError is a definitive bad request: AgentTaskCompleted(FAILED, "bad_request"), acked;
- otherwise the plan's graph runs (budget: the plan's, else the request's, else Budget()); the run's status
  and the protobuf result of `plan.output_node` (packed into google.protobuf.Any) are published, then acked;
- any other exception (a crashing handler, a failed publish) naks with a 30 s delay; on delivery `max_deliver`
  the message is dead-lettered and a FAILED/max_deliver_exceeded completion still answers the requester.
Same heartbeat and shutdown rules as `ai-documents`. Never touches Postgres (rule 3).
"""

from __future__ import annotations

import asyncio
import logging
import uuid
from typing import Any

from google.protobuf.message import DecodeError, Message
from nats.js.api import AckPolicy, ConsumerConfig

from ai.gateway.types import ModelGateway
from ai.gen.compliance.v1 import agents_pb2
from ai.runtime.context import error_code_of
from ai.runtime.events import EventSink
from ai.runtime.graph import GraphExecutor
from ai.runtime.nats_sink import NatsSink
from ai.runtime.registry import AgentRegistry
from ai.runtime.tasks import TaskPlan
from ai.runtime.types import Budget, RunIdentity, RunOutcome, RunStatus
from ai.service import bootstrap
from ai.service.bootstrap import Sleep
from ai.settings import Settings

logger = logging.getLogger(__name__)

TASKS_DURABLE = "ai-agent-tasks"
TASKS_SUBJECT = "agent.task.requested"
TASKS_STREAM = "AGENTS"
DLQ_SUBJECT = "dlq.agent.task"
MAX_DELIVER = 5
MAX_IN_FLIGHT = 4
NAK_DELAY_S = bootstrap.NAK_DELAY_S
SHUTDOWN_GRACE_S = 3.0

_FAILED = agents_pb2.AGENT_RUN_STATUS_FAILED


def consumer_config(settings: Settings) -> ConsumerConfig:
    return ConsumerConfig(durable_name=TASKS_DURABLE, filter_subject=TASKS_SUBJECT,
                          ack_policy=AckPolicy.EXPLICIT, max_deliver=MAX_DELIVER,
                          ack_wait=settings.ack_wait_s, max_ack_pending=MAX_IN_FLIGHT)


def budget_of(b: agents_pb2.RunBudget) -> Budget:
    """A requested RunBudget; an unset (zero or negative) limit keeps the Budget() default."""
    d = Budget()
    return Budget(
        max_steps=b.max_steps if b.max_steps > 0 else d.max_steps,
        max_llm_calls=b.max_llm_calls if b.max_llm_calls > 0 else d.max_llm_calls,
        max_input_tokens=b.max_input_tokens if b.max_input_tokens > 0 else d.max_input_tokens,
        max_output_tokens=b.max_output_tokens if b.max_output_tokens > 0 else d.max_output_tokens,
        max_cost_micro_usd=b.max_cost_micro_usd if b.max_cost_micro_usd > 0 else d.max_cost_micro_usd,
        deadline_s=float(b.deadline_seconds) if b.deadline_seconds > 0 else d.deadline_s,
    )


def _completed(req: agents_pb2.AgentTaskRequested, *, status: int, run_id: str = "", error_code: str = "",
               subject: tuple[str, str] | None = None) -> agents_pb2.AgentTaskCompleted:
    subject_type, subject_id = subject or (req.subject_type, req.subject_id)
    return agents_pb2.AgentTaskCompleted(
        task_id=req.task_id, firm_id=req.firm_id, client_company_id=req.client_company_id, agent=req.agent,
        run_id=run_id, status=status, error_code=error_code,  # type: ignore[arg-type]
        subject_type=subject_type, subject_id=subject_id)


def _identity(req: agents_pb2.AgentTaskRequested, plan: TaskPlan, attempt: int) -> RunIdentity:
    """Contract section 3.8: a new run_id per delivery, the graph's workflow, the plan's or the request's
    subject."""
    subject_type, subject_id = plan.workflow_subject or (req.subject_type, req.subject_id)
    return RunIdentity(run_id=str(uuid.uuid4()), firm_id=req.firm_id, client_company_id=req.client_company_id,
                       workflow=plan.graph.workflow, subject_type=subject_type, subject_id=subject_id,
                       delivery_attempt=attempt)


def _from_outcome(req: agents_pb2.AgentTaskRequested, plan: TaskPlan,
                  outcome: RunOutcome) -> agents_pb2.AgentTaskCompleted:
    ident = outcome.identity
    subject = (ident.subject_type, ident.subject_id)
    status = agents_pb2.AgentRunStatus.Value(f"AGENT_RUN_STATUS_{outcome.status.name}")
    if outcome.status is not RunStatus.SUCCEEDED:
        return _completed(req, status=status, run_id=ident.run_id, error_code=outcome.error_code,
                          subject=subject)
    done = _completed(req, status=status, run_id=ident.run_id, subject=subject)
    if plan.output_node:
        result = outcome.results.get(plan.output_node)
        if not isinstance(result, Message):  # the output node was skipped or failed non-critically
            return _completed(req, status=_FAILED, run_id=ident.run_id, error_code="internal",
                              subject=subject)
        done.output.Pack(result)
    return done


class _Handler:
    def __init__(self, js: Any, executor: GraphExecutor, *, registry: AgentRegistry, settings: Settings,
                 sleep: Sleep) -> None:
        self._js = js
        self._executor = executor
        self._registry = registry
        self._sleep = sleep
        self._beat_s = bootstrap.heartbeat_interval_s(settings.ack_wait_s)

    async def __call__(self, msg: Any) -> None:
        attempt = msg.metadata.num_delivered
        try:
            req = agents_pb2.AgentTaskRequested.FromString(msg.data)
        except DecodeError:
            logger.error("undecodable agent.task.requested, dead-lettering attempt=%d", attempt)
            await bootstrap.dead_letter(self._js, msg, DLQ_SUBJECT)
            return
        handler = self._registry.task_handlers.get(req.agent)
        if handler is None:
            logger.error("no task handler, dead-lettering agent=%s task_id=%s", req.agent, req.task_id)
            await bootstrap.dead_letter(self._js, msg, DLQ_SUBJECT)
            return
        identity: RunIdentity | None = None  # set once this delivery starts a run
        try:
            async with bootstrap.heartbeat(msg, interval_s=self._beat_s, sleep=self._sleep):
                try:
                    plan = await handler(req)
                except ValueError:
                    logger.warning("task handler rejected the request agent=%s task_id=%s",
                                   req.agent, req.task_id)
                    done = _completed(req, status=_FAILED, error_code="bad_request")
                else:
                    identity = _identity(req, plan, attempt)
                    budget = plan.budget or (budget_of(req.budget) if req.HasField("budget") else Budget())
                    done = _from_outcome(req, plan, await self._executor.run(plan.graph, identity, budget))
                await self._send(req, done)
        except asyncio.CancelledError:  # shutdown: hand the message back now rather than after ack_wait
            await bootstrap.settle("nak", msg.nak())
            raise
        except Exception as exc:  # noqa: BLE001 - every escape is a retry, or a dead letter at max_deliver
            await self._failed(msg, req, identity.run_id if identity else "", attempt, exc)
            return
        await bootstrap.settle("ack", msg.ack())

    async def _send(self, req: agents_pb2.AgentTaskRequested, done: agents_pb2.AgentTaskCompleted) -> None:
        await bootstrap.publish_proto(self._js, f"agent.task.completed.{req.agent}",
                                      f"agent.task.completed:{req.task_id}", done)

    async def _failed(self, msg: Any, req: agents_pb2.AgentTaskRequested, run_id: str, attempt: int,
                      exc: Exception) -> None:
        code = error_code_of(exc)
        # the exception text may echo input: debug only
        logger.debug("task raised agent=%s task_id=%s", req.agent, req.task_id, exc_info=exc)
        if attempt < MAX_DELIVER:
            logger.warning("task failed, nak agent=%s task_id=%s attempt=%d code=%s error=%s",
                           req.agent, req.task_id, attempt, code, type(exc).__name__)
            await bootstrap.settle("nak", msg.nak(delay=NAK_DELAY_S))
            return
        logger.error("task failed at max_deliver, dead-lettering agent=%s task_id=%s code=%s",
                     req.agent, req.task_id, code)
        try:
            await self._send(req, _completed(req, status=_FAILED, run_id=run_id,
                                             error_code="max_deliver_exceeded"))
        except Exception:
            logger.exception("agent.task.completed publish failed task_id=%s", req.task_id)
        await bootstrap.dead_letter(self._js, msg, DLQ_SUBJECT)


async def run(js: Any, *, gateway: ModelGateway, registry: AgentRegistry, settings: Settings,
              stop_event: asyncio.Event, sink: EventSink | None = None, sleep: Sleep = asyncio.sleep) -> None:
    """Consumes until stop_event is set. `sink` and `sleep` are test seams (NatsSink(js), asyncio.sleep)."""
    sub = await js.pull_subscribe(TASKS_SUBJECT, durable=TASKS_DURABLE, stream=TASKS_STREAM,
                                  config=consumer_config(settings))
    executor = GraphExecutor(gateway=gateway, sink=sink if sink is not None else NatsSink(js),
                             tools=registry.tools, hooks=(), sleep=sleep)
    await bootstrap.consume(sub, _Handler(js, executor, registry=registry, settings=settings, sleep=sleep),
                            max_in_flight=MAX_IN_FLIGHT, stop_event=stop_event, grace_s=SHUTDOWN_GRACE_S)
