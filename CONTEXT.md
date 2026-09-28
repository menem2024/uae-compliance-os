# CONTEXT — Domain Language

This file defines the words this codebase uses. Code, APIs, database tables, agent prompts and docs must use these terms with these meanings. If a new concept appears, add it here before naming it in code.

## The product in one sentence

**Compliance OS** helps UAE accounting firms get their client companies' invoices ready for the mandatory UAE e-invoicing regime (Peppol / PINT-AE) — ingesting, extracting, validating, explaining and fixing invoices at scale.

## Regulatory context (facts the design depends on)

- UAE e-invoicing is mandatory under Ministerial Decisions No. 243 and 244 of 2025. Invoices must be structured XML sent through an **ASP** over Peppol (the "5-corner" DCTCE model).
- Businesses with revenue ≥ AED 50M: appoint an ASP by 2026-10-31, go live on 2027-01-01.
- Smaller businesses: appoint an ASP by 2027-03-31, go live on **2027-07-01**. This is our core market deadline.
- Record retention: 7 years under Corporate Tax (Federal Decree-Law No. 47 of 2022) and 5 years under VAT. We retain for **7 years**.

## Tenancy and people

| Term | Meaning | Not to be confused with |
|---|---|---|
| **Firm** | An accounting or PRO firm. It is the top-level tenant and the paying customer. | "Organization" (a Zitadel concept that maps 1:1 to a Firm) |
| **ClientCompany** | A UAE business served by a Firm. It owns invoices. It belongs to exactly one Firm. | "Client" alone (ambiguous; never use it bare) |
| **FirmUser** | A Firm staff member. Roles: `firm_admin`, `accountant`. | |
| **ClientUser** | A ClientCompany staff member who uses the **Client Portal**. Role: `client_member`. Sees only their own ClientCompany. | |
| **Client Portal** | The restricted UI where ClientUsers upload documents and answer **DataRequests**. | |

## Documents and invoices

| Term | Meaning |
|---|---|
| **Document** | Any uploaded file (PDF, image, XLSX, CSV) or record synced from an **Integration**. It is immutable once stored and identified by its content hash (`sha256`). |
| **DocumentKind** | The Intake agent's classification: `invoice`, `credit_note`, `contract`, `other`. |
| **Invoice** | The structured, canonical representation of one tax invoice or credit note. It is extracted from a Document or imported. |
| **InvoiceLine** | One line item of an Invoice. |
| **TRN** | Tax Registration Number: a 15-digit UAE VAT identifier. |
| **InvoiceStatus** | The lifecycle: `uploaded → classified → extracted → needs_review → validated → has_issues → fixed → ready`. `ready` means that exporting to PINT-AE XML is allowed. |
| **Export** | A generated PINT-AE XML file for a `ready` Invoice. The Firm hands it to their ASP. In v1 we do not transmit to the ASP. |
| **ASP** | Accredited Service Provider. A licensed Peppol provider that transmits e-invoices to the Federal Tax Authority (FTA). **We are not an ASP.** |

## Validation

| Term | Meaning |
|---|---|
| **Rule** | One deterministic check (for example, "TRN must be 15 digits" or "line VAT = net × rate, rounded per spec"). It has a stable ID such as `AE-TRN-001`. |
| **RuleSet** | A versioned bundle of Rules, for example `pint-ae@1.1`. Every validation records which RuleSet it used. |
| **ValidationRun** | One execution of a RuleSet against one Invoice. It is immutable. |
| **ValidationIssue** | A failed Rule inside a ValidationRun. It has a severity (`error` or `warning`), a field path, and the Rule ID. |
| **FixSuggestion** | An agent-proposed change that resolves a ValidationIssue. It stays `proposed` until a human sets it to `accepted` or `rejected`. |

The core invariant: **only a ValidationRun decides validity. An LLM never does.**

## Agents and AI

| Term | Meaning |
|---|---|
| **Agent** | An LLM-driven loop with tools, running inside the agent runtime of `services/ai-py`. It reads, proposes and explains. It never commits a write or sends anything without a human **Approval**. |
| **Orchestrator** | Routes work between agents for a given job. |
| **Intake / Extraction / Fix / Legal / AnomalyInvestigator / ReadinessAdvisor / ClientComms / RegulationWatcher** | The named agents. See `docs/adr/007`. |
| **Approval** | A recorded human decision (accept or reject) on an agent proposal. It is written to the AuditLog. |
| **DataRequest** | A ClientComms-drafted request to a ClientCompany for missing data. A FirmUser approves it before it is sent. |
| **ModelGateway** | The provider-agnostic LLM abstraction. All model calls go through it. |
| **Source** | An official regulatory document in the RAG corpus, stored with its publisher, version and publication date. |
| **Citation** | A pointer from an agent answer to a Source chunk. A Legal answer without a Citation is invalid. |
| **Eval** | A scored test of an agent against a labelled dataset. It gates CI. |
| **SyntheticInvoice** | A generated invoice with known ground truth and injected defects. It is used for evals and development. |

## Analytics

| Term | Meaning |
|---|---|
| **Anomaly** | An invoice or pattern flagged by the anomaly models, such as a duplicate, a suspicious TRN, or an implausible VAT amount. It is a signal and never blocks export by itself. |
| **ReadinessScore** | The per-ClientCompany share of invoices that are `ready`, with its trend and a projection against 2027-07-01. |

## Operations

| Term | Meaning |
|---|---|
| **AuditEvent** | An append-only record of who (human or agent) changed what, when, and why. It is never updated or deleted. |
| **Integration** | A connector to accounting software: Zoho Books first, then QuickBooks, then Tally. |
| **Job** | An async unit of work that runs over NATS JetStream, such as a batch upload of 5,000 documents. |
