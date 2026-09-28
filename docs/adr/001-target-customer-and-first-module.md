---
title: "Target customer and first module"
description: "Why we serve accounting firms first, starting with e-invoicing readiness plus a legal explainer."
category: "adr"
number: "001"
status: draft
related: ["design/001", "roadmap/001"]
last_modified: "2026-09-27"
---

# 001 — Target customer and first module

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
UAE e-invoicing becomes mandatory for smaller businesses on 2027-07-01, and they must appoint an ASP by 2027-03-31. Most SMEs have no compliance staff and depend on accounting or PRO firms, and each of those firms must prepare dozens of clients at once.

## Decision
- **Customer:** accounting and PRO Firms that manage many ClientCompanies. ClientCompanies take part through a restricted Client Portal.
- **First module:** e-invoicing readiness. That means ingesting invoices, extracting them, validating them against PINT-AE, explaining issues with official citations, proposing fixes, and exporting XML.
- **Later modules:** document expiry tracking, Emiratisation, WPS and Corporate Tax helpers (see `docs/BACKLOG.md`).

## Trade-offs
- *Selling direct to SMEs:* easier to explain, but a shallower tenancy model and a slower route to many invoices. Rejected.
- *Real estate or tender intelligence:* interesting data, but no regulatory deadline creating pull. Rejected.
- The risk is that accounting software vendors and ASPs will add readiness features. Our edge is the multi-client workflow for a Firm.

## Revisit triggers
- MoF moves the SME date or changes the model.
- No Firm is willing to try the product after Phase 1.
