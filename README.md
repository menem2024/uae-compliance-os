# Compliance OS — UAE e-invoicing readiness for accounting firms

UAE businesses must issue structured e-invoices over the Peppol network (PINT-AE). Accounting and PRO
firms have to get their SME clients' invoices ready before the mandate reaches them
(SMEs go live on **2027-07-01**). This platform is the workspace where a firm does that for all of its clients.

> **Status: work in progress, not production-ready.** The deterministic core and the review workflow are
> verified end to end. Language-model features are built but not yet measured against a real model. See
> [`docs/STATUS.md`](docs/STATUS.md) for exactly what is verified, what is only implemented, and what is blocked.

## What it does today

```
invoice (JSON / spreadsheet / PDF)
   -> canonical invoice model            (PINT-AE business terms, decimal-exact money)
   -> deterministic validation in Rust   (302 official PINT-AE assertions + platform rules)
   -> findings with official business terms, English and Arabic messages, suggested values
   -> human correction and approval      (every change audited, append-only audit log)
   -> PINT-AE UBL 2.1 XML export         (checked against the official schema and Schematron)
```

- **Deterministic validation is the source of truth.** The Rust engine implements the official PINT-AE 1.0.4
  rule set and was compared against the official Schematron on the 30 official examples and ~20,000 mutated
  documents with no differences. A language model never decides whether an invoice is valid.
- **Multi-agent orchestra** (document intake, extraction, a verifier that checks the other agents, budgets,
  live activity feed) runs on an in-house runtime with a model gateway (spend cap, cache, record/replay).
- **Multi-tenant by construction:** one firm cannot read another firm's data (Postgres row-level security,
  tested through every route and consumer).
- **Arabic and English with RTL**, per-firm theming, light and dark.

## Architecture in one picture

```
Browser -> Next.js (BFF, Zitadel login) -> api-go -> Postgres (RLS)
                                              |  \-> MinIO (documents, exports)
                                              v
                                   NATS JetStream  <->  ai-py (agent runtime, model gateway)
                                              |
                                              v
                                   validator-rs (gRPC): rules engine + XML exporter
```

One trace spans web -> Go -> NATS -> Python -> NATS -> Rust -> Go (OpenTelemetry, Grafana/Tempo).
Details: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Stack

Go (chi, pgx, sqlc, connect-go) · Rust (tonic, rust_decimal, quick-xml) · Python 3.14 (agent runtime, Anthropic
SDK behind a gateway) · Next.js 16 / React / Tailwind v4 / next-intl · PostgreSQL 17 · NATS JetStream · MinIO ·
Valkey · Zitadel · OpenTelemetry · Docker Compose, Helm on k3d, Terraform skeleton.

## Run the demo

Needs Docker (Docker Desktop with WSL integration on Windows). **No API key is needed** for the demo path.

```bash
bash scripts/demo.sh          # = make up + health check; first build takes several minutes
```

| What | Where |
|---|---|
| App | http://localhost:3000 — sign in `a@firm-a.test` / `Password1!` (second firm: `b@firm-b.test`) |
| Invoice review | http://localhost:3000/en/invoices |
| Agent activity | http://localhost:3000/en/agents |
| Traces | http://localhost:3001 (Grafana) |

Stop with `make down`.

### Real extraction without an Anthropic key

The demo runs a canned fake model by default. To read real PDFs and images through any OpenAI-compatible
endpoint (a free OpenRouter or Google AI Studio key is enough), set three variables before `make up`:

```bash
export AI_PROVIDER=openai_compat
export AI_OPENAI_BASE_URL=https://openrouter.ai/api/v1   # or https://generativelanguage.googleapis.com/v1beta/openai/
export AI_OPENAI_API_KEY=...                              # your key; never commit it
```

For Gemini also set `AI_OPENAI_MODEL_FAST` and `AI_OPENAI_MODEL_SMART` (for example `gemini-2.5-flash`).
Extracted invoices still land as `needs_review` unless the verifier independently agrees; free-model accuracy is
unmeasured. See [`docs/STATUS.md`](docs/STATUS.md).

### 60–90 second demo

1. **Invoices -> New demo invoice -> "Valid invoice"** (the official PINT-AE example). It validates in
   milliseconds with zero findings. **Approve**, then **Export PINT-AE XML**; open the audit trail.
2. **New demo invoice -> "Totals mismatch".** One error: `ibr-co-16`, business term IBT-115, with a
   deterministic suggested value. **Apply suggestion**, re-validate, approve, export.
3. **"Missing mandatory fields".** Four errors with official terms and Arabic messages. Type the missing
   values, re-validate.
4. Switch the language to Arabic (RTL), then open **Agents** for the live orchestra feed.
5. Open Grafana to show the single trace across the four services.

The four sample invoices are in [`demo/samples/`](demo/samples) and derive from the official example.

### Seed demo data

So the app does not open empty, `scripts/seed-demo.sh` fills a running stack with synthetic UAE data through the
real path only (no direct database writes): it signs in through the normal Zitadel login in a headless browser and
calls the app's own `/api` routes, so everything is validated by the Rust engine, goes through the state machine and
lands in the audit log.

```bash
bash scripts/seed-demo.sh                      # http://localhost:3000, a@firm-a.test / b@firm-b.test, dev password
BASE_URL=https://demo.example SEED_PASSWORD=... bash scripts/seed-demo.sh     # another stack
bash scripts/seed-demo.sh --dry-run            # print the plan, touch nothing
```

Firm A gets 10 fictional client companies (English and Arabic names, all seven emirates, synthetic 15-digit TRNs)
and 30 invoices derived from the canonical samples with deterministic variation (all in 2026): 16 valid, 6 totals
mismatches, 4 bad seller TRNs, 4 missing buyer fields. Then the human steps run through the API: 10 valid invoices are
approved (5 of them exported to PINT-AE XML) and 2 mismatches get the suggested correction, which leaves
10 `ready`, 8 `validated` and 12 `has_issues`, with audit trails on the touched ones. Firm B gets 2 clients and 3 invoices,
and the script ends with an isolation check (neither firm sees the other's data).

It is idempotent: clients are matched by TRN and invoices by their deterministic number before anything is created, each
human step runs only when the invoice is in the state it needs, and nothing is ever deleted. Settings (environment,
all optional): `BASE_URL`, `SEED_EMAIL`, `SEED_PASSWORD` (falls back to `SEED_USER_PASSWORD`, then the local dev password;
never committed), `SEED_EMAIL_B`, `SEED_PASSWORD_B`, `SEED_FIRM_B=0`, `SEED_BRAND=0`. Needs Node 22.18+ and the
Playwright Chromium from `e2e/` (installed on first run; on a minimal Linux also `npx playwright install-deps chromium`).
Documents and the AI `needs_review` queue are not seeded: presigned uploads need MinIO to be reachable from the script,
and extraction needs a model. The generator is unit-tested with `cd e2e && npm run seed:test`.

## Tests

```bash
make test                                   # Rust, Go, Python unit suites
cd apps/web && npm test                     # web unit tests
make smoke                                  # Playwright end to end against the running stack
# the demo backend flow against real Postgres and the real Rust validator, no Docker:
#   see the header of services/api-go/internal/trackc/demoflow_integration_test.go
cd services/validator-rs && cargo run --example validate -- ../../demo/samples/02-totals-mismatch.json
```

## Repository map

`services/api-go` · `services/validator-rs` · `services/ai-py` · `apps/web` · `proto/` (contracts) ·
`deploy/` (compose, helm, k3d, terraform) · `e2e/` · `demo/` · `docs/` (ADRs, design, roadmap, status).
