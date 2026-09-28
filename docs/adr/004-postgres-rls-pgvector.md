---
title: "Postgres with RLS multi-tenancy and pgvector"
description: "One Postgres; shared schema with firm_id + Row-Level Security; pgvector for the RAG corpus."
category: "adr"
number: "004"
status: draft
related: ["design/001", "adr/005"]
last_modified: "2026-09-27"
---

# 004 — Postgres with RLS multi-tenancy and pgvector

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
There are two tenancy levels, Firm and then ClientCompany, and tax data where leaks are unacceptable. The RAG corpus is small and shared by all tenants.

## Decision
- One Postgres with a shared schema. Every tenant table has `firm_id` and an RLS policy based on `current_setting('app.firm_id')`. Services connect as non-superuser roles.
- ClientCompany scoping for ClientUsers is enforced in `api-go` and covered by tests.
- `sources` and `source_chunks` (pgvector) are global and not tenant-scoped.
- A CI test asserts that every table with `firm_id` has RLS enabled, and that Firm A cannot read Firm B.

## Trade-offs
- *Schema per Firm:* migration fan-out. Rejected.
- *DB per Firm:* the operational cost does not fit a USD 100/month budget. Rejected.
- *Qdrant:* an extra service for roughly 10⁵ chunks. Rejected.
- RLS adds friction for debugging and connection pooling (`SET LOCAL` per transaction). This is accepted.

## Revisit triggers
- A Firm contractually requires physical isolation.
- The vector corpus grows past about 5M chunks.
