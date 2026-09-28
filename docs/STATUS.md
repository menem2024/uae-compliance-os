# STATUS

> Read this first in every session. Update it at the end of every session.

**Last updated:** 2026-09-28

## Now
- **Phase:** 0 — Walking skeleton (in progress)
- **Next action:** wire the remaining compose pieces (Tasks 6/7) and run `make up && make smoke` from a clean checkout to prove the exit criteria; see `docs/roadmap/001-roadmap.md`

## Done
- 2026-09-27: design grilled and agreed; `CONTEXT.md`, `design/001`, `adr/001–015` and `roadmap/001` written.
- Phase 0 stories complete so far (per `.ship/tasks/phase-0-walking-skeleton/dev-ledger.md`):
  - Story 1: tooling, repo skeleton, proto contracts, codegen
  - Story 2: validator-rs — AE-TRN-001, gRPC server, OTel, Dockerfile
  - Story 3: Postgres schema, roles, RLS, Go db package
  - Story 5: ai-py worker — stub extraction, NATS, trace propagation
  - Story 7a: apps/web scaffold + design foundation
  - Story 7b: apps/web — Auth.js Zitadel, BFF, demo page, OTel, Dockerfile
  - Story 8: e2e smoke suite (Playwright) for AC1 (flow), AC2 (one trace across 4 services) and AC3 (tenancy); `tsc --noEmit` and `playwright test --list` verified, not yet run against a live stack
- apps/web runs on **Next.js 16.3.6**.

## Blocked / open questions
- None yet. Verify the PINT-AE spec and schematron availability at the start of Phase 2.
- `e2e/lib/auth.ts`'s Zitadel hosted-login selectors are written defensively against the pinned v4.3.0 UI (deploy/compose/compose.yaml) but unverified live — no compose stack has run in this environment yet. Re-check with `npx playwright codegen http://zitadel.localhost:8085` once it's up.

## Phase checklist
| Phase | State |
|---|---|
| 0 Walking skeleton | 🟨 in progress |
| 1 Ingestion & extraction | ⬜ |
| 2 Validation, fix, export | ⬜ |
| 3 Legal knowledge | ⬜ |
| 4 Client Portal & comms | ⬜ |
| 5 Data science | ⬜ |
| 6 Integrations | ⬜ |
| 7 Hardening | ⬜ |
