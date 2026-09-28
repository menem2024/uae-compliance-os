---
title: "Public monorepo, no licence yet, Claude writes the code"
description: "Single public monorepo layout; all rights reserved for now; code authored by Claude and reviewed by the owner."
category: "adr"
number: "015"
status: draft
related: ["design/001"]
last_modified: "2026-09-27"
---

# 015 — Public monorepo, no licence yet, Claude writes the code

## Status

Accepted 2026-09-27. Not yet implemented.

## Decision
- **Monorepo layout:**
  ```
  apps/web/            services/api-go/     services/validator-rs/
  services/ai-py/      proto/               deploy/ (terraform, helm, compose)
  docs/                CONTEXT.md
  ```
- **Public on GitHub** from day one as a portfolio. There is no open-source licence yet, so all rights are reserved. The licence will be decided if the project goes commercial.
- `main` is protected. Work happens on short branches with one pull request per story.
- **Authorship:** Claude writes the code and the owner reviews it. The owner runs `/grill-me:grill-me` after each phase to keep a working mental model.

## Trade-offs
- *Multi-repo:* proto changes would span several repos. Rejected.
- Public code with no licence is visible but not reusable. This is intended for now.
