---
title: "RAG corpus: official sources only, versioned, citation required"
description: "Legal agent answers only from versioned official MoF/FTA sources with mandatory citations."
category: "adr"
number: "009"
status: draft
related: ["adr/007"]
last_modified: "2026-09-27"
---

# 009 — RAG corpus: official sources only, versioned, citation required

## Status

Accepted 2026-09-27. Not yet implemented.

## Context
Wrong legal guidance is worse than no guidance.

## Decision
- The corpus contains only official publications: MoF e-invoicing guidelines and the data dictionary, the PINT-AE specification, the VAT Law and its Executive Regulation, the Corporate Tax Law, Ministerial and Cabinet Decisions, and FTA public clarifications. Both Arabic and English versions are included.
- Each `sources` row records its publisher, URL, language, version and publication date.
- Chunks are embedded in pgvector with hybrid retrieval (vector + full-text).
- Every Legal answer must cite at least one chunk. If there is no citation, the answer is "I don't know."
- RegulationWatcher proposes new or updated Sources, and a human approves ingestion.

## Trade-offs
- Blogs and vendor guides are often clearer, but they are unofficial, so they are excluded.

## Revisit triggers
- The citation-faithfulness eval falls below target.
