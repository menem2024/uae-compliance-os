---
title: "Polyglot service boundaries"
description: "Go API, Rust validator, Python AI service, Next.js web; each language owns one job."
category: "adr"
number: "002"
status: draft
related: ["design/001", "adr/003"]
last_modified: "2026-09-27"
---

# 002 — Polyglot service boundaries

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
The goal is to learn and to build a real project, and the user wants Go, Rust, Python and React all present from v1. Each language needs a job it is genuinely the best fit for.

## Decision
| Service | Language | Why this language |
|---|---|---|
| `services/api-go` | Go | Concurrency, a simple operational model, and a strong gRPC and Postgres ecosystem. It is the single public API and the orchestrator. |
| `services/validator-rs` | Rust | Deterministic, fast rule evaluation. Exact decimals. Safe XML generation at batch scale. |
| `services/ai-py` | Python | LLM, RAG and data-science ecosystem (pydantic, polars, scikit-learn). |
| `apps/web` | Next.js / React | SSR, i18n, RTL, and the ecosystem. |

Rust ships in v1, not later. This is an explicit user decision.

## Trade-offs
- Four toolchains, four CI pipelines and three OTel setups mean real overhead for one developer. This is accepted in exchange for the learning.
- *Ship v1 in Go + Python and add Rust later with a benchmark:* proposed and rejected by the user. The consequence is that `docs/roadmap/001` Phase 7 still records before/after benchmarks against a naive baseline.

## Revisit triggers
- If a service stays under 300 lines of logic after Phase 3, consider folding it into its neighbour.
