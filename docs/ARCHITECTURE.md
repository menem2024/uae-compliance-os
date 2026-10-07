# Architecture

Design records live in [`docs/adr/`](adr) and [`docs/design/001-system-overview.md`](design/001-system-overview.md).
This page is the short, current map. Where it disagrees with an ADR, check the code.

## Services

| Service | Language | Responsibility |
|---|---|---|
| `apps/web` | Next.js 16, React, Tailwind v4 | UI (ar/en, RTL, per-firm theme), Zitadel login (Auth.js), BFF routes. The browser never holds the Zitadel access token; the web tier never calls ai-py or validator-rs directly. |
| `services/api-go` | Go | Only public API. JWT verification (JWKS), tenant resolution, REST, signed uploads, state machine, audit, NATS consumers, SSE feed. |
| `services/validator-rs` | Rust | gRPC `ValidatorService` (rules engine) and `ExportService` (PINT-AE UBL 2.1 XML). Pure, deterministic, no I/O. |
| `services/ai-py` | Python | Agent runtime (graph executor, budgets, tools), model gateway, intake/extraction/verifier agents, evals, synthetic data. No database credentials. |
| Postgres 17 | — | System of record. Forced row-level security on every tenant table. |
| NATS JetStream | — | Events between api-go and ai-py (`DOCUMENTS`, `AGENTS`, `INVOICES`, DLQs). |
| MinIO / S3 | — | Uploaded documents and exported XML. |
| Valkey | — | Rate limits, model-response cache, spend counters. |
| Zitadel | — | Identity. One organisation = one accounting firm. |
| OpenTelemetry + Grafana LGTM | — | One trace across web, Go, NATS, Python, Rust. |

## Data flow

1. **Submit.** A canonical invoice arrives at `POST /v1/invoices` (or a document is uploaded to MinIO with a
   presigned PUT and `complete`d; sha256 deduplicates). The invoice is stored with a payload version.
2. **Orchestrate (documents).** `document.uploaded` starts the `document_ingestion@1` task graph in ai-py:
   intake, then extraction (deterministic importer for CSV/XLSX; model for PDF/images), then a verifier that
   checks the extraction independently. Results return as `document.extracted`; api-go creates invoices.
3. **Validate.** api-go calls validator-rs over gRPC with the stored payload. The run (issues with official
   business terms, English/Arabic messages, suggested values) is persisted; the invoice status moves
   `validated` or `has_issues`.
4. **Correct and approve.** A person applies corrections (optimistic lock on the payload version) and
   re-validates. Only a person moves `validated -> ready`.
5. **Export.** `ready` invoices are exported by validator-rs; the XML is stored with its sha256.
6. **Audit.** Field changes, approvals and exports append to `audit_events`.

## Deterministic validation

- `services/validator-rs/src/rules/*` implements the PINT-AE 1.0.4 assertions by family (header, parties,
  lines, codelists, totals, vat) plus platform `AE-*` rules. Each of the 302 official assertions is
  `implemented`, `structural` (proved by a named test) or `upstream_noop`, listed in
  `rulesets/pint-ae-1.0.4/coverage/*.tsv` with its Arabic message.
- Money is `rust_decimal`/exact cents, never a float. Sums use an exact type because the decimal crate rounds
  silently beyond ~29 digits.
- The official Schematron (SaxonC) is the oracle in CI, never the runtime: fixtures are compared against it.
- A language model never sets validity (ADR 006). Fix suggestions are proposals that need human approval.

## AI and orchestration

- Every model call goes through the gateway: response cache, concurrency limit, atomic daily spend cap per
  firm (reserve, then settle; failed-but-billed calls are charged; fail-closed when the limiter is down),
  record/replay for tests. No test or CI job calls a real model.
- A result produced by the fake gateway can never land as a clean accepted invoice.
- Agents emit `agent.run.*` events; api-go persists runs and steps and streams them per firm over SSE.

## Database and tenancy

Tenant tables carry `firm_id` with `ENABLE` + `FORCE ROW LEVEL SECURITY` and a `firm_isolation` policy on
`app.firm_id`; the app role has no `BYPASSRLS`, no `DELETE` on evidence tables and no `UPDATE` on
`audit_events`/`exports`. All tenant access goes through `db.WithFirm`. Migrations: goose, ranges per track.

## Security

JWT (JWKS with refetch, issuer and optional audience), per-firm named rate limits, presigned uploads that are
create-only (`If-None-Match: *`), size/type/sha256 verification on `complete`, no PII in logs/spans/feed,
non-root containers that exit on SIGTERM within 10 s, gitleaks and dev-only secrets in the repo.
Independent reviews of the Go services and the agent core produced fixes that are in the history
(atomic result apply, spend-cap accounting, SSE id validation, filename handling).

## Deployment

Docker Compose for local and demo (`make up`). Helm umbrella chart for k3d/k3s (static checks pass; not yet
run on a cluster in this phase). Terraform skeleton validates only; nothing has been applied.
