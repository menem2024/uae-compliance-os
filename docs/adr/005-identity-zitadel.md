---
title: "Identity with Zitadel and two-level tenancy"
description: "Zitadel (self-hosted OIDC) for authn; Firm = Zitadel org; roles and ClientCompany scope in our DB."
category: "adr"
number: "005"
status: draft
related: ["adr/004"]
last_modified: "2026-09-27"
---

# 005 — Identity with Zitadel and two-level tenancy

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
Firms, their staff and their ClientCompanies' staff all need to log in. Hand-rolled auth on a tax platform is a needless security risk.

## Decision
- **Zitadel**, self-hosted and written in Go, provides authentication through OIDC. Each Firm maps to one Zitadel Organization.
- Authorization lives in our `memberships` table with the roles `firm_admin`, `accountant` and `client_member`. `client_member` is always bound to one ClientCompany.
- `api-go` validates tokens and sets `app.firm_id` for RLS.

## Trade-offs
- *Build our own:* this is where the risk lives. Rejected.
- *Clerk or Auth0:* cost, plus vendor lock-in for data residency. Rejected.
- *Keycloak:* heavier (JVM) and a weaker fit for the multi-org model. Rejected.
- Zitadel adds another stateful service to operate.

## Revisit triggers
- Firms require SSO with their own IdP (Zitadel supports it, but the setup cost needs checking).
