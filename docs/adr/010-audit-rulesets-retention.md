---
title: "Append-only audit log, versioned rulesets, 7-year retention"
description: "Every change is an AuditEvent; every ValidationRun records its RuleSet; data kept 7 years."
category: "adr"
number: "010"
status: draft
related: ["adr/006", "adr/004"]
last_modified: "2026-09-27"
---

# 010 — Append-only audit log, versioned rulesets, 7-year retention

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
A Firm must be able to answer "who changed this field, when, why, and was it an agent's suggestion?" UAE law requires tax records to be kept for 7 years (Corporate Tax) and 5 years (VAT).

## Decision
- The `audit_events` table is append-only. The DB role cannot run UPDATE or DELETE on it. Each event records the actor (user or agent + approval), the entity, the before/after diff and the reason.
- Every ValidationRun stores `ruleset_version`. A RuleSet upgrade triggers re-validation and a diff report.
- Documents, Invoices, Exports and AuditEvents are retained for 7 years.

## Trade-offs
- Storage grows without bound within the retention window. This is acceptable at the numbers in `design/001`.

## Revisit triggers
- A legal change to retention periods.
