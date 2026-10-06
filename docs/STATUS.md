# STATUS

> Honest state of the repository. Updated 2026-10-05 on branch `demo` (phase-1 + phase-2 merged). Nothing has
> been pushed; no GitHub remote exists, so CI has never run.

## VERIFIED / WORKING

Verified on a real running stack (Docker Compose, all 10 services healthy, `compose-check.sh` OK) with
Playwright against the real UI: **all 8 end-to-end tests pass** (2026-10-05, `--retries=0`):
- invoice review: valid official invoice -> validated -> approved -> exported as PINT-AE XML;
- invoice review: totals mismatch -> validator's suggested value applied -> re-validated -> validated;
- Firm user flow: create a client, brand colour, upload a document, see the agent run;
- one connected trace across the four services (Tempo), cross-firm isolation through the BFF (404),
  signed-out landing, RTL, mandate countdown.
The legacy `/demo` skeleton invoice no longer demonstrates the TRN rule (the official `ibr-132-ae` applies to
AE parties); TRN checking is shown by the "Bad TRN" sample on `/invoices`.

Demo UI fixes verified on the public stack (compose project `compliance-pub`, wiped and re-seeded, 2026-10-06,
Playwright screenshots in en and ar): the dashboard shows live client and invoice counts, the pipeline counts and
the five newest invoices (counts come from the list endpoints, bounded to 500 records each, there is no counts
endpoint); the invoice list shows the invoice issue date, totals with thousands separators and ICU plural error and
warning counts (Arabic has the six plural forms, covered by a unit test); the clients table hides the Documents and
Needs review columns while no uploaded documents exist (`POST /v1/invoices` has no client id, so API-created
invoices cannot be linked to a client). web: 199 unit tests, `tsc` and eslint clean. The local-stack e2e suite was
not run in this pass (the local stack was down). The empty-dashboard hero (no clients, no invoices) was not
re-checked live because both seeded firms have data.

Verified without Docker against real Postgres 17 and the real Rust validator (`TestDemoFlow`):
all four sample invoices through POST -> validate -> correct -> re-validate -> approve -> export; XML digest
matches; 9 cross-tenant routes return 404; audit trail asserted.

Unit/integration suites green at the time of the last run: api-go 24 packages (incl. DB integration), validator-rs
225 lib tests, ai-py 397, web 188. Rule engine compared with the official Schematron: 0 differences on the
official examples and on tens of thousands of mutated documents (per rule family).

## IMPLEMENTED

- PINT-AE 1.0.4 rules engine: 302 official assertions (header 69, parties 71, lines 46, codelists 18,
  totals 41, vat 57) classified as implemented/structural/upstream_noop, plus platform rules; exact-decimal totals
  with deterministic suggested values; UBL 2.1 XML exporter; gRPC export service.
- Validation service with status state machine, optimistic payload lock, sweeper, `revalidate` command.
- Audit log (append-only, DB role cannot UPDATE/DELETE), exports table, fix-task tables.
- Agent runtime, model gateway (spend cap, cache, record/replay), intake/extraction/verifier agents, document
  ingestion workflow, signed uploads with sha256 dedup, SSE agent feed, `/agents` and `/documents` pages,
  landing page, clients and settings, eval harness with four suites.
- `openai_compat` model provider (`services/ai-py/src/ai/gateway/openai_compat_gw.py`): runs extraction through
  any OpenAI-compatible chat-completions endpoint (OpenRouter free models, Google Gemini's OpenAI endpoint)
  with no Anthropic key. Selected with `AI_PROVIDER=openai_compat` (alias of `AI_GATEWAY`); it is a live
  gateway, so the spend cap, fail-closed limiter and response cache wrap it, and its results still land as
  `needs_review` unless the verifier agrees. Tested against a mocked HTTP transport only.
- Independent security and agent-core reviews done and their fixes merged.

## IN PROGRESS

- Phase 2 task 15 (conformance suite, completeness test, `COVERAGE.md`): fixes for `ibr-016`, exact cents in
  every sum, per-document Schematron errors are in; the completeness test, fuzz corpus and CI job are not.
- Phase 1 sign-off (task 31): needs the chaos run and a live-model evaluation (see BLOCKED).

## BLOCKED

| Blocker | Needs | Effect |
|---|---|---|
| `ANTHROPIC_API_KEY` | owner | No real extraction from PDFs/images, no measured accuracy (the exit criterion is 90% on 200 invoices), no Fix agent. Scores in `make evals` come from a scripted fake model and prove the harness, not the AI. With the fake gateway, results are forced to `needs_review`. |
| Free-tier quota for live measurement | owner | Measured on a real Gemini key (2026-10-06). Cause of the earlier 7/24 errors: the free tier allows only 20 requests per day per model (`GenerateRequestsPerDayPerProjectPerModel-FreeTier`, HTTP 429 RESOURCE_EXHAUSTED, reset ~16 h), plus an occasional 503 UNAVAILABLE overload. Per-minute pacing cannot fix a daily quota. A 24-case sample needs about 40 calls, so it cannot finish on one free model per day: `gemini-3.5-flash` finished 16/24 cases (field accuracy 88.0%, exact match 78.6% of the 14 that ran, PDFs 100%, images 79.7%, 0 transient errors with pacing) before the 20/day limit. Earlier `gemini-2.5-flash` run: 95.1% on cases that ran, 68.6% overall. Needs a paid key (or several models over several days) for a full, honest 24- or 200-case number. Reports: `services/ai-py/evals/reports/openai_compat/`. |
| Docker Desktop running | owner | Live stack, Playwright, and the 1000-document chaos test (`scripts/phase1-chaos.sh`) need it. |
| GitHub remote | owner | CI never ran. |
| Licence decision D-3 | owner | Official OpenPeppol artefacts are fetched by pinned sha256 script, not vendored. |
| Native Arabic review | owner | Arabic UI strings and rule messages are machine-assisted and unreviewed. |

## PLANNED

- Phase 2 remainder: performance gate (p99 < 5 ms), `/invoices` list polish and `/review` queue, proposals
  decide flow, Fix agent (needs the key), fix evals, sign-off.
- Phase 3: legal knowledge (RAG over official sources, Legal agent, RegulationWatcher) — plan written, no code.
- Phase 4: client portal and communications. Phase 5: data science. Phase 6: Zoho/QuickBooks/Tally.
  Phase 7: hardening, load test, security review, cloud dry run.
- Verifier scope (by design, ADR 006): it checks that the extraction matches the document, not that the invoice is
  compliant. A source with a missing buyer TRN or amounts off by a cent is accepted when extracted faithfully
  (arithmetic findings are marked `confirmed_by_critic`); validator-rs judges the document. Real residual gap:
  the critic only runs when a check flags something or a critical field's confidence is below 0.8, so a wrong
  value with no arithmetic effect (e.g. invoice_number) can be accepted (1 of 3 wrong extractions in the
  partial sample). Fixing it means always running the critic, which doubles calls against a 20/day quota.
- Known gaps recorded in `.ship/tasks/*/concerns.md`: SSE stream lifetime/re-auth, node timeout equals provider
  timeout, spend counters on a non-persistent Valkey, a few review P3s, no visual QA of the UI (RTL, contrast; first screenshot pass done 2026-10-05: sample titles showed raw i18n keys (fixed); invoices table narrowed so it fits at 1280px; a few agent-feed lines stay English in /ar because ai-py emits that text, not the web).

## Phase checklist

| Phase | State |
|---|---|
| 0 Walking skeleton | verified locally; k3d/CI/push outstanding |
| 1 Ingestion, extraction, orchestra | built (30/31 tasks), reviewed; live-model and chaos evidence outstanding |
| 2 Validation, fix, export | core verified (rules, export, review flow); ~15/25 tasks |
| 3 Legal knowledge | planned |
| 4–7 | not planned in detail |
