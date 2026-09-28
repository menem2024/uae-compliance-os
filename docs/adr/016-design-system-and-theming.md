---
title: "Design system, visual identity and per-Firm theming"
description: "World-class fintech polish with a subtle UAE identity; light/dark plus white-label Firm theming; UI toolkit."
category: "adr"
number: "016"
status: draft
related: ["design/001", "roadmap/001", "adr/005"]
last_modified: "2026-09-27"
---

# 016 — Design system, visual identity and per-Firm theming

## Status

Accepted on 2026-09-27. Not yet implemented. This decision was missed in the first grilling round and added afterwards.

## Context

The owner wants a UI that looks exceptional and is clearly professional, not a stock SaaS template. It must be Arabic-first with RTL and also work in English. Accounting Firms show the Client Portal to their own clients.

## Decision

- **Visual direction:** Linear, Stripe and Vercel level polish, meaning disciplined spacing, soft gradients, restrained glass surfaces and fluid motion, combined with a subtle UAE identity: a gold accent, deep neutrals, and faint Islamic geometric patterns on the auth screens, empty states and hero areas. The identity is felt rather than shown as a theme.
- **Themes:**
  - Light, dark and system.
  - **Per-Firm white-label theming.** Each Firm sets an accent colour and a logo. The Client Portal and the Firm's workspace render with them. This is implemented as CSS custom properties (design tokens) resolved per tenant at request time. Contrast is validated so that any Firm colour still meets WCAG AA; if it doesn't, it is auto-adjusted.
- **Toolkit:**
  - Tailwind CSS v4 with tokens in `@theme`, and shadcn/ui on Radix for accessible primitives.
  - Motion (Framer Motion) for animation, TanStack Table with virtualisation for large invoice tables, Recharts for charts, cmdk for a Ctrl+K command palette, Sonner for toasts, and Lucide for icons.
- **Typography:** IBM Plex Sans Arabic paired with IBM Plex Sans, so there is one family across both scripts. The UI uses **Western digits** in both locales, and `tabular-nums` in tables and money columns.
- **RTL:** Tailwind logical utilities only (`ms-`, `me-`, `ps-`, `pe-`, `start-`, `end-`, `text-start`), enforced by lint. Directional icons flip in RTL.
- **Process:** a clickable prototype of the key screens is approved before the screens are built. Every new screen in every phase uses the design-system tokens and components. One-off styling is not allowed.
- **Phasing:**
  - Phase 0 ships the design foundation: tokens, fonts, light/dark, Firm accent plumbing, the app shell (sidebar, header, command palette) and motion primitives.
  - Phase 1 adds a public landing page.

## Trade-offs

- White-label theming adds token plumbing and contrast validation. This is accepted because it is a real selling point for Firms.
- Motion and glass effects can hurt performance and accessibility. `prefers-reduced-motion` is respected, and glass is limited to non-content surfaces.

## Revisit triggers

- Lighthouse performance below 90 or accessibility below 95 on key screens.
- Firms ask for a full custom theme rather than an accent colour.
