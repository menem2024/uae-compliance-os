---
title: "Observability: OpenTelemetry, Grafana stack, Langfuse"
description: "OTel in all services; Prometheus/Loki/Tempo/Grafana; Langfuse for agent traces, cost and tokens."
category: "adr"
number: "013"
status: draft
related: ["adr/007", "adr/008"]
last_modified: "2026-09-27"
---

# 013 — Observability: OpenTelemetry, Grafana stack, Langfuse

## Status

Accepted 2026-09-27. Not yet implemented.

## Decision
- OpenTelemetry SDKs run in Go, Rust and Python. Trace context propagates over gRPC and NATS headers.
- Prometheus handles metrics, Loki handles logs and Tempo handles traces, all in Grafana, all self-hosted on k3s.
- Langfuse (self-hosted) records agent traces with steps, tokens, cost per agent, prompt versions and eval scores.
- Alerts cover the DLQ being non-empty, daily LLM spend above threshold, p95 API latency, and failed backups.

## Trade-offs
- The monitoring stack uses a meaningful share of VPS RAM. This is accepted. Without traces, debugging nine agents across three languages is guesswork.

## Revisit triggers
- VPS memory pressure. In that case, move to Grafana Cloud's free tier.
