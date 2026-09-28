---
title: "Roadmap — Phases 0 to 7"
description: "Ordered delivery phases with scope and hard exit criteria; a phase is done only when its exit criteria pass."
category: "roadmap"
number: "001"
status: draft
related: ["design/001", "adr/014"]
last_modified: "2026-09-27"
---

# 001 — Roadmap — Phases 0 to 7

## Status

Accepted on 2026-09-27. Phase 0 has not started yet. Live progress is tracked in `docs/STATUS.md`.

## Summary

Eight phases, done in strict order. **A phase is done only when all of its exit criteria pass.** We do not start phase N+1 while phase N is "almost done". New ideas go to `docs/BACKLOG.md`, not into the current phase.

## Delivery loop per phase

`/ship:design` (plan the phase) → `/ship:dev` → `/ship:e2e` → `/ship:review` → `/ship:qa` → `/ship:handoff` (PR) → the owner runs `/grill-me:grill-me` on the new code → update `docs/STATUS.md`.

## Phases

### Phase 0 — Walking skeleton
**Scope:**
- The monorepo layout and `proto/` with `buf`.
- A minimal Go API, Rust validator (one Rule), Python worker (a stub agent) and Next.js page with ar/en and RTL.
- The design foundation per `adr/016`: tokens, IBM Plex fonts, light/dark, per-Firm accent plumbing, the app shell (sidebar, header, Ctrl+K palette) and motion.
- NATS, Postgres with RLS and one tenant table, Valkey, MinIO and Zitadel login.
- Docker Compose for local development.
- k3s on a VPS with Helm and Cloudflare.
- CI per service, gitleaks, and OTel feeding Grafana.

**Exit criteria:**
- [ ] A logged-in FirmUser submits a fake invoice JSON in the UI. It flows web → Go → NATS → Python → NATS → Go → gRPC Rust and back, and the UI shows the ValidationRun result.
- [ ] That single request appears as **one connected trace** across all 4 services in Grafana Tempo.
- [ ] The RLS test proves that Firm A cannot read Firm B.
- [ ] `docker compose up` and a Helm deploy to k3s both work from a clean checkout.
- [ ] CI is green, and `buf breaking` and gitleaks are active.

### Phase 1 — Ingestion and extraction
**Scope:**
- A public landing page (per `adr/016`).
- Signed R2 upload and `sha256` deduplication.
- Firm and ClientCompany management.
- The Intake and Extraction agents on the in-house runtime, with the ModelGateway.
- The SyntheticInvoice generator.
- The XLSX/CSV deterministic importer.
- The Langfuse and eval harness.

**Exit criteria:**
- [ ] Field-level extraction accuracy ≥ 90% on 200 SyntheticInvoices (ar and en, PDF and image).
- [ ] A 1,000-document batch upload completes without manual intervention. Killing the worker mid-batch loses nothing.
- [ ] Re-uploading the same file makes no LLM call (cache hit visible in Langfuse).
- [ ] Extraction evals run in CI.

### Phase 2 — Validation, fix and export
**Scope:**
- PINT-AE Rules and RuleSet versioning in Rust.
- ValidationRuns and ValidationIssues.
- The Fix agent, the review UI and Approvals.
- The AuditLog.
- PINT-AE XML Export.

**Exit criteria:**
- [ ] Every implemented Rule has unit tests, and the rule coverage list is documented.
- [ ] Exported XML validates against the official schema or schematron.
- [ ] Accepting a FixSuggestion re-validates the invoice and writes an AuditEvent. The DB role cannot UPDATE or DELETE `audit_events`.
- [ ] Rust validation p99 < 5 ms per invoice (benchmarked).

### Phase 3 — Legal knowledge
**Scope:**
- Official Source ingestion (ar and en) and pgvector hybrid retrieval.
- The Legal agent, linked from each ValidationIssue.
- RegulationWatcher.

**Exit criteria:**
- [ ] 50-question Legal eval: 100% of answers carry a Citation, and faithfulness is at or above threshold.
- [ ] Out-of-corpus questions answer "I don't know" (eval subset).
- [ ] A new Source detected by RegulationWatcher requires human approval before ingestion.

### Phase 4 — Client Portal and communications
**Scope:**
- ClientUser role and the Portal.
- The ClientComms agent and DataRequests.
- Email and notifications.

**Exit criteria:**
- [ ] The full loop works: a missing-field issue leads to a drafted DataRequest, a FirmUser approves it, it is sent, the ClientUser answers in the Portal, and the invoice is re-validated.
- [ ] A ClientUser cannot see any other ClientCompany (test).

### Phase 5 — Data science
**Scope:**
- Anomaly models (duplicates, TRN patterns, VAT plausibility) and the AnomalyInvestigator.
- ReadinessScore and its projection, plus the ReadinessAdvisor weekly report.

**Exit criteria:**
- [ ] Injected anomalies in the synthetic data are detected with precision and recall at or above agreed thresholds (fixed at Phase 5 design).
- [ ] The weekly readiness report is generated per ClientCompany, and its projection is backtested on synthetic history.

### Phase 6 — Integrations
**Scope:** Zoho Books, then QuickBooks, then Tally.

**Exit criteria:**
- [ ] A full sync from each provider's sandbox produces Invoices that validate without manual mapping fixes (Tally is best effort).

### Phase 7 — Hardening and production readiness
**Scope:**
- Rust vs naive-baseline benchmarks and cache hit-rate measurement.
- A load test at the design ceiling burst (20 × 5,000 documents).
- `/security-review`.
- A cloud migration dry run.

**Exit criteria:**
- [ ] Before/after numbers are documented in `docs/`.
- [ ] The load test meets the latency targets in `design/001`.
- [ ] A Terraform dry run of the UAE-region deployment succeeds.

## Standing rules

- One phase at a time. Its exit criteria are the only gate.
- Any architecture change needs a new ADR in `docs/adr/`.
- Check the weekly LLM spend in Langfuse against the USD 50–100/month budget.
- After Phase 1, show the product to at least one real accounting Firm.

## References

- `design/001`: system overview
- `adr/001` … `adr/015`
