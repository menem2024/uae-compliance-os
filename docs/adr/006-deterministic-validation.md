---
title: "Validity is decided by deterministic Rust rules, never by an LLM"
description: "Rules/RuleSets in Rust decide invoice validity; LLMs only extract, explain and propose."
category: "adr"
number: "006"
status: draft
related: ["design/001", "adr/007", "adr/010"]
last_modified: "2026-09-27"
---

# 006 — Validity is decided by deterministic Rust rules, never by an LLM

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
Compliance outcomes must be reproducible and auditable, so the same input must always give the same verdict. LLM output is probabilistic.

## Decision
- `validator-rs` implements each Rule with a stable ID (for example `AE-TRN-001`) and groups them into versioned RuleSets (for example `pint-ae@1.1`).
- Only a ValidationRun can move an Invoice to `validated` or `has_issues`, and only a clean run permits `ready`.
- All money arithmetic uses `rust_decimal`, and rounding follows the specification.
- Every Rule has unit tests, and PINT-AE XML output is checked against the official schema or schematron in CI.

## Trade-offs
- Encoding rules by hand is slower than asking an LLM. In return we get correctness, explainability and speed (< 5 ms per invoice).

## Revisit triggers
- MoF publishes an official validation artefact (such as a schematron) that we could run directly.
