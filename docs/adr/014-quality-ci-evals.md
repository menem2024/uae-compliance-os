---
title: "Quality gates: per-service CI, contract checks, agent evals, synthetic data"
description: "GitHub Actions per service, buf breaking, RLS tests, agent evals gating CI on synthetic + real data."
category: "adr"
number: "014"
status: draft
related: ["adr/007", "adr/006"]
last_modified: "2026-09-27"
---

# 014 — Quality gates: per-service CI, contract checks, agent evals, synthetic data

## Status

Accepted 2026-09-27. Not yet implemented.

## Decision
- GitHub Actions uses path-filtered workflows per service, running lint, test and build, plus `buf lint` and `buf breaking`, plus gitleaks.
- The RLS isolation test suite is mandatory (`adr/004`).
- The **SyntheticInvoice generator** (in `ai-py`) produces Arabic and English PDF, image and XLSX invoices with known ground truth and injected defects. It is the base dataset for evals from day one.
- Real, anonymised invoices from a pilot Firm are added once they are available.
- **Any change to an agent prompt or tool runs its evals.** CI fails if a score drops below its threshold. Examples: extraction field accuracy ≥ 90%, and Legal citation rate = 100%.

## Trade-offs
- Evals cost LLM tokens on every agent pull request. Mitigated by caching and by a small per-PR eval subset, with the full suite running nightly.
