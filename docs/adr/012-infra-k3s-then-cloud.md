---
title: "Infrastructure: k3s on a VPS now, UAE managed cloud at first paying customer"
description: "k3s + Cloudflare on one VPS; everything in Terraform/Helm; move to UAE cloud region on first paying Firm."
category: "adr"
number: "012"
status: draft
related: ["adr/011", "adr/013"]
last_modified: "2026-09-27"
---

# 012 — Infrastructure: k3s on a VPS now, UAE managed cloud at first paying customer

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
The budget is USD 50–100/month. A UAE-region managed stack costs roughly USD 250–500/month (a rough estimate that has not been verified).

## Decision
- **Dev/staging, which is also the public demo:** k3s on one VPS behind Cloudflare. Local development uses Docker Compose.
- **Everything is code:** Terraform for the VPS, DNS and R2, and Helm charts per service. Secrets use sealed-secrets.
- **Production:** moves to a UAE-region managed cloud (Azure UAE North or AWS me-central-1) **only when the first Firm pays.** Because the infrastructure is code, the move should take days, not weeks.
- Postgres is backed up nightly to R2, and restores are tested monthly.

## Trade-offs
- A single VPS is a single point of failure. This is acceptable for a pilot or demo.
- *Managed cloud now:* over budget. Rejected.

## Revisit triggers
- The first paying Firm, or an uptime commitment.
