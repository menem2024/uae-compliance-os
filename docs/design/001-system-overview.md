---
title: "System Overview — Compliance OS"
description: "Architecture of Compliance OS: services, data flow, numbers, failure modes, boundaries. Start here."
category: "design"
number: "001"
status: draft
services: [services/api-go, services/validator-rs, services/ai-py, apps/web, proto, deploy]
related: ["roadmap/001", "adr/001", "adr/002", "adr/003", "adr/004", "adr/006", "adr/007"]
last_modified: "2026-09-27"
---

# 001 — System Overview — Compliance OS

## Status

Accepted on 2026-09-27 after a design grilling session. Nothing is implemented yet. The status changes to `current` once Phase 0 (see `roadmap/001`) is built and these claims are verified against the code.

## Summary

Compliance OS is a multi-tenant B2B platform for UAE accounting firms. It turns their client companies' invoices (PDF, image, Excel, or accounting software) into validated PINT-AE e-invoices before the 2027-07-01 SME mandate. Four services in four languages sit behind one Go API: **Go** for the API and orchestration, **Rust** for deterministic validation and XML, **Python** for agents, RAG and data science, and **Next.js** for the UI. The core rule is that validity is decided by deterministic Rust rules. LLM agents only extract, explain and propose, and humans approve.

## Goals / Non-goals

**Goals**
- A Firm can onboard 40+ ClientCompanies and see each one's e-invoicing readiness.
- Ingest PDF, image, XLSX/CSV and Zoho Books data, then extract canonical Invoices in Arabic and English.
- Validate every Invoice against a versioned PINT-AE RuleSet, explain each issue with an official citation, and propose fixes.
- Export valid PINT-AE XML.
- Offer a Client Portal where ClientUsers upload documents and answer DataRequests.
- Provide an Arabic/English UI with RTL from day one.
- Serve as a portfolio-grade system: observable, tested, evaluated and reproducible infrastructure.

**Non-goals (v1)**
- Becoming an ASP or transmitting to Peppol or the FTA. Export only; direct ASP integration comes later.
- Full accounting, ERP or bookkeeping.
- Filing VAT or Corporate Tax returns.
- Document-expiry, Emiratisation or WPS modules. These are phase-2 product (see `docs/BACKLOG.md`).
- On-prem or self-hosted customer deployments.

## Requirements & numbers

The sizing target is a **pilot of 5 Firms**. The design must stay sane at **50 Firms**.

| Quantity | Pilot | Design ceiling |
|---|---|---|
| Firms | 5 | 50 |
| ClientCompanies per Firm | 40 | 40 |
| Invoices per ClientCompany per month | 200 | 1,000 |
| Invoices per month | 40k | 2M |
| Average rate | ~0.015 invoice/s | ~0.8 invoice/s |
| Peak burst (month-end batch) | 5,000 docs in one upload | 20 concurrent such uploads |
| Stored document size | ~200 KB each → 8 GB/month | 400 GB/month |
| Retention | 7 years | 7 years |

**LLM budget.** Extraction costs roughly 3k input and 0.5k output tokens per PDF or image invoice. XLSX, CSV and Integration imports skip LLM extraction entirely. Results are cached by document `sha256`, so a document is never extracted twice. During development the total spend (infra + LLM) is capped at **USD 50–100/month**. This cap is why caching, the structured-import bypass and per-agent cost limits are mandatory rather than optional.

**Latency targets.**
- Validation of one Invoice in Rust: < 5 ms p99.
- A 5,000-document batch reaches `extracted` within 30 min at pilot scale. This is bounded by LLM rate limits, not by our code.
- The UI API: < 300 ms p95.

## Design

### Components

```
                  Cloudflare (CDN for static assets, WAF, DNS)
                                   │
                        apps/web (Next.js, ar/en, RTL)
                                   │  HTTPS (JSON)
                        services/api-go  ── the only public API
                      ┌──────┬──────────┬───────────┬─────────┐
                  Zitadel  Postgres   Valkey     R2 (S3)   NATS JetStream
                  (authn) (+RLS,     (cache,    (documents,    │
                           pgvector)  sessions,  exports)      │ jobs / events
                                      rate limit)              │
                    ┌──────────────────────────────────────────┤
          services/validator-rs                     services/ai-py
          (Rust: RuleSets, PINT-AE XML,             (Python: agent runtime,
           gRPC + NATS consumer)                     ModelGateway, RAG,
                                                     extraction, anomaly models)
```

| Service | Owns | Talks to |
|---|---|---|
| `apps/web` | UI, i18n, Client Portal | `api-go` only |
| `services/api-go` | AuthZ, tenancy, REST/JSON for web, Job orchestration, AuditEvents, InvoiceStatus transitions | Postgres, Valkey, R2, NATS, gRPC to `validator-rs` and `ai-py` |
| `services/validator-rs` | Rules, RuleSets, ValidationRuns, PINT-AE XML generation | gRPC (sync validate), NATS (batch) |
| `services/ai-py` | Agents, Orchestrator, ModelGateway, RAG ingestion/retrieval, anomaly and readiness models, SyntheticInvoice generator | NATS, Postgres (through its own read and write paths, still under RLS), R2 (read) |
| `proto/` | All gRPC contracts and event schemas | managed by `buf` |

### Main data flow (one invoice)

1. `web` → `api-go` `POST /documents` issues a signed R2 upload URL. The client uploads directly to R2.
2. `api-go` stores the Document (by `sha256`, deduplicated per ClientCompany) and publishes `document.uploaded` on NATS.
3. `ai-py` runs the **Intake** agent to classify the Document, then the **Extraction** agent, which produces a canonical Invoice JSON validated by pydantic. It publishes `invoice.extracted`. XLSX, CSV and Integration imports skip straight to this step with a deterministic parser.
4. `api-go` persists the Invoice (`extracted`, or `needs_review` when extraction confidence is low) and calls `validator-rs.Validate` over gRPC.
5. `validator-rs` returns a ValidationRun. `api-go` then sets `validated` or `has_issues`.
6. For each ValidationIssue, `ai-py` **Fix** proposes a FixSuggestion and **Legal** attaches a Citation.
7. A FirmUser accepts or rejects in the UI. Acceptance writes an AuditEvent and triggers re-validation. A clean run sets `ready`.
8. `api-go` asks `validator-rs.Export` for PINT-AE XML, stores it in R2, and the Firm downloads it.

Side flows:
- **ClientComms** drafts a DataRequest. After a FirmUser approves it, it is sent and the ClientUser answers in the Portal, which leads to re-validation.
- **AnomalyInvestigator** explains anomaly-model flags.
- **ReadinessAdvisor** writes a weekly per-ClientCompany report.
- **RegulationWatcher** watches official sources and proposes corpus updates for human approval.

### Data model (core tables, all carrying `firm_id` and RLS-protected)

`firms`, `client_companies`, `users`, `memberships(user, firm, client_company?, role)`, `documents(sha256, r2_key, kind)`, `invoices(status, client_company_id, canonical jsonb, extraction_confidence)`, `invoice_lines`, `validation_runs(ruleset_version)`, `validation_issues(rule_id, severity, path)`, `fix_suggestions(state)`, `approvals`, `data_requests`, `audit_events` (append-only), `anomalies`, `readiness_snapshots`, `jobs`.

Shared, non-tenant tables: `sources`, `source_chunks(embedding vector)`, `rulesets`.

Money is stored as `numeric` in Postgres and handled as `rust_decimal`, `shopspring/decimal` and `Decimal` in the services. **Floats are never used for money.**

### Contracts

- gRPC via `buf`, in `proto/compliance/v1/*.proto`. Go uses `connect-go`, Rust uses `tonic`, Python uses `grpcio`. `buf breaking` runs in CI.
- NATS subjects are `<domain>.<event>` (for example `document.uploaded` and `invoice.extracted`). Payloads are protobuf messages from the same `proto/`.

### Libraries

| Service | Libraries |
|---|---|
| Go | chi, pgx + sqlc, goose, connect-go, shopspring/decimal, OTel SDK |
| Rust | tonic, quick-xml, rust_decimal, async-nats, tracing + OTel |
| Python | uv, grpcio, nats-py, pydantic, polars, scikit-learn, OTel, Langfuse SDK |
| Web | Next.js App Router, next-intl, shadcn/ui (RTL), TanStack Query |

## Boundaries

These are the anti-drift rules. Breaking one requires a new ADR.

- **Only `api-go` is public.** `web` never calls `ai-py` or `validator-rs`, and nothing else exposes an internet-facing port.
- **Only `validator-rs` decides validity.** `ai-py` may never set an InvoiceStatus to `validated` or `ready`.
- **Agents never write business state directly.** They emit proposals. Only `api-go`, acting on a human Approval, applies them. The exception is `extracted`/`needs_review` Invoice creation, which is re-validated before it means anything.
- **Every tenant table has `firm_id` and an RLS policy.** Services connect as a non-superuser role and set `app.firm_id` per transaction.
- **All LLM calls go through ModelGateway.** No provider SDK is imported outside it.
- **Legal answers require a Citation to a `sources` row.** With no citation, the answer is "I don't know."
- **Private documents never pass through the CDN.** They are served only by short-lived signed R2 URLs.
- **Contracts live only in `proto/`.** No hand-written cross-service JSON.
- **No floats for money**, in any language.

## Failure modes

| Failure | Effect | Mitigation |
|---|---|---|
| LLM provider down or rate-limited | Extraction stalls | JetStream redelivery with backoff. Jobs resume. The UI shows a "queued" state. Structured imports are unaffected. |
| Extraction hallucinates a field | Wrong data in the Invoice | Deterministic validation catches structural and arithmetic errors. A low-confidence result goes to `needs_review`. Evals track field accuracy. |
| RLS misconfigured | Cross-tenant data leak | CI integration test: a query as Firm A must return 0 rows of Firm B for every tenant table. Services never use a superuser role. |
| Rules change (new PINT-AE version) | Old validations become stale | RuleSets are versioned. Re-validating in bulk against the new version produces a diff report. |
| A poison message in NATS | Consumer crash loop | Max-deliver followed by a dead-letter stream, with an alert in Grafana. |
| Cost runaway (agent loop) | Budget blown | Per-agent max steps and token cap in the runtime. A daily spend alert from Langfuse. The sha256 cache. |
| Single VPS dies | Full outage | Acceptable for the pilot. Nightly Postgres backup to R2. Terraform/Helm rebuild in under 1 hour (see `adr/012`). |

## Security

- Authentication through Zitadel (OIDC). Authorization in `api-go` from `memberships`. ClientUsers are scoped to one ClientCompany.
- Data isolation through Postgres RLS on `firm_id`. ClientCompany scoping for ClientUsers is enforced in `api-go` and tested.
- The LLM provider must offer zero data retention. PII is redacted before traces reach Langfuse and the logs (see `adr/008`).
- Secrets live in k8s Secrets (sealed-secrets in Git). gitleaks runs in CI. The repo is public.
- Documents are encrypted at rest (R2 default). All traffic is TLS.

## Alternatives considered

- **A single language (Go or Python) monolith.** Simpler and faster to ship. Rejected because the learning goal needs polyglot service boundaries, and each language here has a job it is best at.
- **Validating with the LLM.** Rejected because compliance needs deterministic, auditable outcomes (see `adr/006`).
- **REST between services.** Rejected in favour of gRPC plus `buf`, which gives typed contracts across three languages (see `adr/003`).
- **A separate vector DB (Qdrant).** Rejected because the corpus is small, and one Postgres means fewer moving parts (see `adr/004`).

## Assumptions

- SME mandate date 2027-07-01 holds (Ministerial Decisions 243/244 of 2025).
- The PINT-AE specification and MoF data dictionary are publicly available and machine-readable enough to encode as Rules.
- Pilot scale fits on one VPS.
- An LLM provider with zero data retention and acceptable Arabic extraction quality is available.

## Revisit triggers

- The first paying Firm: move production to UAE-region managed cloud (`adr/012`).
- More than 20 Firms, or any Firm demanding UAE data residency: revisit infra and the LLM provider (`adr/008`).
- MoF changes the e-invoicing model or dates: revisit scope (`adr/001`).
- Field accuracy of extraction below 90% after Phase 1: revisit the extraction approach.

## References

- `roadmap/001`: phases and exit criteria
- `adr/001` … `adr/015`: individual decisions
- UAE MoF e-invoicing (Ministerial Decisions 243 and 244 of 2025; Electronic Invoicing Guidelines v1.1, 2026-06-01)
- [Banqup: UAE phased e-invoicing rollout](https://www.banqup.com/resources/blog/uae-confirms-phased-e-invoicing-mandate-rollout)
- [FTA: Federal Decree-Law No. 47 of 2022](https://tax.gov.ae/en/content/federal.decreelaw.no.47.of.2022.on.taxation.of.corporations.and.businesses.home.new.aspx)
