# STATUS

> Honest state of the repository. Updated 2026-10-05 on branch `demo` (phase-1 + phase-2 merged). Nothing has
> been pushed; no GitHub remote exists, so CI has never run.

## VERIFIED / WORKING

Verified on a real running stack (Docker Compose, all 10 services healthy, `compose-check.sh` OK) with
Playwright against the real UI: **7 of 9 end-to-end tests passed**, including
- invoice review: valid official invoice -> validated -> approved -> exported as PINT-AE XML;
- invoice review: totals mismatch -> validator's suggested value applied -> re-validated -> validated;
- Firm user flow: create a client, brand colour, upload a document, see the agent run;
- cross-firm isolation through the BFF (404), signed-out landing, RTL, mandate countdown.
The two failing tests were legacy Phase 0 `/demo` tests that assumed the old one-rule engine; they were
rewritten for the full engine but **not re-run** (Docker Desktop stopped afterwards). A second full run
during the same session failed en masse while Docker was going down and is not counted.

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
- Independent security and agent-core reviews done and their fixes merged.

## IN PROGRESS

- Phase 2 task 15 (conformance suite, completeness test, `COVERAGE.md`): fixes for `ibr-016`, exact cents in
  every sum, per-document Schematron errors are in; the completeness test, fuzz corpus and CI job are not.
- Phase 1 sign-off (task 31): needs the chaos run and a live-model evaluation (see BLOCKED).

## BLOCKED

| Blocker | Needs | Effect |
|---|---|---|
| `ANTHROPIC_API_KEY` | owner | No real extraction from PDFs/images, no measured accuracy (the exit criterion is 90% on 200 invoices), no Fix agent. Scores in `make evals` come from a scripted fake model and prove the harness, not the AI. With the fake gateway, results are forced to `needs_review`. |
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
- Known gaps recorded in `.ship/tasks/*/concerns.md`: SSE stream lifetime/re-auth, node timeout equals provider
  timeout, spend counters on a non-persistent Valkey, a few review P3s, no visual QA of the UI (RTL, contrast).

## Phase checklist

| Phase | State |
|---|---|
| 0 Walking skeleton | verified locally; k3d/CI/push outstanding |
| 1 Ingestion, extraction, orchestra | built (30/31 tasks), reviewed; live-model and chaos evidence outstanding |
| 2 Validation, fix, export | core verified (rules, export, review flow); ~15/25 tasks |
| 3 Legal knowledge | planned |
| 4–7 | not planned in detail |
