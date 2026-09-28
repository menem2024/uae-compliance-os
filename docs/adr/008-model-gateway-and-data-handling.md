---
title: "ModelGateway, provider choice and LLM data handling"
description: "Provider-agnostic LLM gateway; zero-data-retention provider; PII redacted from traces and logs."
category: "adr"
number: "008"
status: draft
related: ["adr/007", "adr/013"]
last_modified: "2026-09-27"
---

# 008 — ModelGateway, provider choice and LLM data handling

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
Invoices contain personal and commercial data, and the UAE PDPL applies. Extraction needs the full document, so redacting before the model breaks accuracy.

## Decision
- All LLM and embedding calls go through **ModelGateway** in `ai-py`. No provider SDK is imported anywhere else.
- The default provider is a hosted frontier model (Anthropic Claude) under zero-data-retention terms.
- Full documents are sent to the model for extraction.
- PII (names, IBANs, phone numbers, emails) is redacted before traces reach Langfuse or the logs.
- Responses are cached by (prompt version, input hash).

## Trade-offs
- *Local models only:* weaker Arabic extraction and heavy GPU cost. Rejected for v1. The gateway keeps this option open.
- *Redacting before the model:* hurts extraction. Rejected.

## Revisit triggers
- A Firm requires in-country processing. In that case, add a UAE-hosted or local provider behind the gateway.
