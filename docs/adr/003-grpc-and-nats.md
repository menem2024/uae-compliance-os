---
title: "gRPC for sync calls, NATS JetStream for jobs"
description: "Inter-service comms: buf-managed gRPC for request/response, NATS JetStream for async jobs and events."
category: "adr"
number: "003"
status: draft
related: ["design/001", "adr/002"]
last_modified: "2026-09-27"
---

# 003 — gRPC for sync calls, NATS JetStream for jobs

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
Three backend languages need typed contracts. Batch uploads of 5,000 documents must not block HTTP requests and must survive restarts.

## Decision
- **Synchronous:** gRPC, with contracts in `proto/` managed by `buf`. Go uses connect-go, Rust uses tonic, Python uses grpcio. `buf breaking` gates CI.
- **Asynchronous:** NATS JetStream. Subjects follow `<domain>.<event>` and payloads are protobuf. Consumers are durable, with max-deliver set and a dead-letter stream.
- `web` ↔ `api-go` uses JSON over HTTPS.

## Trade-offs
- *REST everywhere:* no shared types, and drift between three languages. Rejected.
- *Kafka:* heavier to operate on one VPS, and nothing needs its throughput. Rejected.
- *Postgres-backed queue (River):* Go-only. Rejected.
- NATS is one more stateful component to run. This is accepted.

## Revisit triggers
- Event volume above 1k msg/s sustained, or a need for long-term event replay.
