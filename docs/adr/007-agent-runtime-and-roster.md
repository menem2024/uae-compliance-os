---
title: "Custom agent runtime, agent roster, human approval"
description: "Own Python agent runtime with 8 agents + orchestrator; agents propose, humans approve writes/sends."
category: "adr"
number: "007"
status: draft
related: ["adr/006", "adr/008", "adr/014"]
last_modified: "2026-09-27"
---

# 007 — Custom agent runtime, agent roster, human approval

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
The user wants many agents, one per job, and a runtime they understand end to end.

## Decision
**Runtime:** built in-house inside `services/ai-py`. It provides a tool registry, typed outputs through pydantic, a per-agent step and token cap, OTel and Langfuse tracing, and eval hooks. Every model call goes through the ModelGateway (`adr/008`).

**Roster:**
| Agent | Job |
|---|---|
| Intake | Classify a Document (`DocumentKind`) and attribute it to a ClientCompany |
| Extraction | Document → canonical Invoice, with a confidence score |
| Fix | ValidationIssue → FixSuggestion |
| Legal | Answer and explain from the RAG corpus with Citations |
| AnomalyInvestigator | Explain why an Anomaly was flagged |
| ReadinessAdvisor | Weekly readiness report per ClientCompany |
| ClientComms | Draft DataRequests and emails to ClientCompanies |
| RegulationWatcher | Detect new official publications and propose corpus updates |
| Orchestrator | Route jobs between agents |

**Rule:** agents read, propose and explain. Any business-state write or external send requires a human Approval, applied by `api-go`, and is recorded as an AuditEvent.

## Trade-offs
- *LangGraph:* hides the loop, which defeats the learning goal. Rejected.
- *Claude Agent SDK:* ties us to one provider, which conflicts with the ModelGateway. Rejected.
- Nine agents multiply eval and cost surface. Mitigated by `adr/014` and by per-agent caps.

## Revisit triggers
- The runtime passes about 2k lines of framework code that has nothing to do with our domain.
