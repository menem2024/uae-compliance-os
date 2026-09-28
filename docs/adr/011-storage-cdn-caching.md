---
title: "Object storage, CDN and caching"
description: "R2 for documents via signed URLs; Cloudflare CDN for static only; Valkey for sessions, rate limits, caches."
category: "adr"
number: "011"
status: draft
related: ["design/001", "adr/012"]
last_modified: "2026-09-27"
---

# 011 — Object storage, CDN and caching

## Status

Accepted 2026-09-27. Not yet implemented.

## Decision
- **Documents and exports:** Cloudflare R2, which has no egress fees. Access uses only short-lived signed URLs. MinIO is used for local development.
- **CDN:** Cloudflare sits in front of `apps/web` for static assets and public pages, plus WAF and DDoS protection. **Private documents never pass through the CDN cache.**
- **Valkey** holds sessions, per-Firm rate limits and hot read caches in `api-go`.
- **Content caches:** extraction results are keyed by document `sha256` (the same file is never extracted twice), and Legal answers are keyed by (question hash, corpus version).

## Trade-offs
- *S3:* egress costs. Rejected.
- *Redis:* licence change, so we chose Valkey, which is the drop-in open fork.

## Revisit triggers
- UAE data residency is required. R2 has no UAE region, so documents would move to a UAE-region object store.
