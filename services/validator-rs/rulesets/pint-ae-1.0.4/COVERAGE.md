# Rule coverage: pint-ae@1.0.4+r1

Generated from `services/validator-rs/rulesets/pint-ae-1.0.4/coverage/*.tsv` by `cargo run --bin conformance -- coverage-md`. Do not edit; CI fails when it is stale (`coverage-md --check`).

Every one of the 302 official PINT-AE 1.0.4 asserts is listed once, with one of three statuses: `implemented` (a registered Rust rule with a passing and a failing fixture), `structural` (the canonical model plus the exporter make a violation impossible; the note names the test that proves it) or `upstream_noop` (the official test can never fail). Platform rules (`AE-*`) are listed after the official ones.

Official asserts: 302 (233 implemented, 65 structural, 4 upstream_noop, 0 pending). Platform rules: 12.

| Family | Rules | Implemented | Structural | Upstream no-op | Pending |
|---|---:|---:|---:|---:|---:|
| header | 69 | 38 | 31 | 0 | 0 |
| parties | 71 | 50 | 18 | 3 | 0 |
| lines | 46 | 35 | 10 | 1 | 0 |
| totals | 41 | 37 | 4 | 0 | 0 |
| vat | 57 | 56 | 1 | 0 | 0 |
| codelists | 18 | 17 | 1 | 0 | 0 |
| platform | 12 | 12 | 0 | 0 | 0 |
| **total** | **314** | **245** | **65** | **4** | **0** |

## header (69)

| Rule | Status | Severity | Business term | Fix | Note |
|---|---|---|---|---|---|
| `aligned-ibrp-001-ae` | implemented | error | IBT-024 | llm | evaluated on the defaulted, trimmed IBT-024; case-sensitive prefix match (AE-SCOPE-001 rejects the self-billing prefix separately) |
| `aligned-ibrp-002-ae` | implemented | error | IBT-023 | llm | evaluated on the defaulted, trimmed IBT-023; the exporter always writes ProfileID, so the exists() half cannot fail |
| `ibr-001` | structural | error | IBT-024 | none | the exporter writes CustomizationID always (the default urn:peppol:pint:billing-1@ae-1 when IBT-024 is empty), so it is never blank; aligned-ibrp-001-ae still checks the value. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-001-ae` | implemented | error | BTAE-03 | llm | BTAE-03 is written as DiscrepancyResponse/ResponseCode only for credit notes (381/81); inline list of 6 codes (codelists.tsv list ibr-001-ae) |
| `ibr-002` | implemented | error | IBT-001 | none | absent or blank IBT-001 |
| `ibr-002-ae` | implemented | error | BTAE-04 | llm | regex ^[0-9]+(\.[0-9]{1,6})?$ on the written decimal text: a sign or more than 6 decimals fails; an invalid decimal is absent here (AE-FMT-001 reports it) |
| `ibr-003` | implemented | error | IBT-002 | none | absent or blank IBT-002 |
| `ibr-004` | implemented | error | IBT-003 | none | absent or blank IBT-003 (InvoiceTypeCode and CreditNoteTypeCode are both absent) |
| `ibr-005` | implemented | error | IBT-005 | none | absent or blank IBT-005 |
| `ibr-005-ae` | implemented | error | BTAE-06 | llm | BTAE-06 is InvoicePeriod/DescriptionCode of the document-level period; inline list of 10 codes (list ibr-005-ae) |
| `ibr-029` | implemented | error | IBT-074 | llm | document-level period only: the line periods (priority 1029 in the same pattern) are ibr-030's. xs:date() raises a dynamic error on a date that is not castable; the rule then reports nothing (ibr-073 reports the date) and dates with a time zone are not compared |
| `ibr-049` | implemented | error | IBT-081 | none | fires per PaymentMeans element the exporter writes (an instruction with any content, and for a credit note the first instruction when IBT-009 is set) that has no code |
| `ibr-052` | implemented | error | IBT-122 | none | the context also matches the invoiced-object (130), project (50) and BTAE-20 references, which always carry an ID; a supporting document is written only with IBT-122, IBT-123 or IBT-124, so an attachment-only one is not a context node (attachments are not exported) |
| `ibr-054` | implemented | error | IBT-161 | none | an attribute without a name is not written (omitted aggregate), so the rule fires on a name without a value only |
| `ibr-055` | implemented | error | IBT-025 | none | a BillingReference is written when IBT-025 or IBT-026 is present; fires when only IBT-026 is. ibr-sr-07 has the same condition |
| `ibr-055-ae` | implemented | error | IBT-025 | none | credit notes only; a missing reason code makes both branches false. Path and term are credit_note_reason_code / BTAE-03 when the reason is absent or is VD while references exist (the user must change the reason or drop the references) |
| `ibr-057` | implemented | error | IBT-080 | none | the address element exists when any of its seven fields is present |
| `ibr-066` | implemented | error | IBG-18 | none | a CardAccount is written for a card with IBT-087, IBT-088 or a network id; one finding, at the second card account |
| `ibr-067` | implemented | error | IBG-19 | none | a PaymentMandate is written for IBT-089 or IBT-091; one finding, at the second mandate |
| `ibr-071` | structural | error | IBG-24 | none | the only AdditionalDocumentReference with type code 130 is the invoiced object (IBT-018), which carries an ID and the type code only; attachments are never exported. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-072` | structural | error | IBG-24 | none | the only AdditionalDocumentReference with type code 130 is the invoiced object (IBT-018), which carries an ID and the type code only; supporting documents carry no type code. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-073` | implemented | error | IBT-002 | llm | one finding per date element (path and term per field, F7): IBT-002, IBT-007, IBT-009 (invoices: DueDate; a credit note's PaymentDueDate is AE-EXP-008's), IBT-026, IBT-072, IBT-073/074, IBT-134/135. castable as xs:date follows XML Schema 1.1: year 0000 passes (AE-EXP-009 reports it for the XSD) |
| `ibr-074` | structural | error | IBT-125 | none | the exporter never writes an element whose name ends in BinaryObject (attachments are not exported, spec non-goal 2). Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-075` | structural | error | IBT-125 | none | the exporter never writes an element whose name ends in BinaryObject (attachments are not exported, spec non-goal 2). Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-076` | structural | error | IBT-023 | none | the exporter writes ProfileID always (default urn:peppol:bis:billing); aligned-ibrp-002-ae still checks the value. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-077` | implemented | error | IBT-006 | none | normalize-space comparison of the two codes |
| `ibr-078` | structural | error | IBT-018 | none | IBT-018 is one Identifier (references.invoiced_object), written as at most one AdditionalDocumentReference with type code 130. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-079` | structural | error |  | none | El::push drops an element with neither text nor children, and every leaf is built from trimmed text. Proof: rules::header::tests::structural_rules_hold_on_every_export and export::tests::every_example_exports_in_xsd_order_without_empty_elements. |
| `ibr-090` | structural | error | IBT-011 | none | IBT-011 is one string: one cac:ProjectReference (invoice) or one AdditionalDocumentReference with type code 50 (credit note), never both. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-093` | structural | error | IBT-180 | none | the canonical model has no prepaid-payment aggregate and the exporter never writes cac:PrepaidPayment (IBT-113 is LegalMonetaryTotal/PrepaidAmount only). Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-094` | structural | error | IBT-012 | none | IBT-012 is one string, written as one ContractDocumentReference/ID. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-095` | structural | error | IBT-015 | none | IBT-015 is one string, written as one ReceiptDocumentReference/ID. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-096` | structural | error | IBT-016 | none | IBT-016 is one string, written as one DespatchDocumentReference/ID. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-097` | structural | error | IBG-14 | none | invoicing_period is one message and BTAE-06 shares its single cac:InvoicePeriod; line periods are not children of the root. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-107` | structural | error | IBG-13 | none | delivery is one message, written as one cac:Delivery. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-108` | structural | error | IBT-070 | none | IBT-070 is one string, written as one DeliveryParty/PartyName/Name. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-119` | implemented | error | IBT-168 | llm | IBT-168 is the only cbc element whose name ends in Time; castable as xs:time (24:00:00 accepted, offset up to +-14:59 as Saxon accepts it; AE-EXP-010 reports the offsets beyond 14:00 for the XSD) |
| `ibr-124-ae` | implemented | error | IBT-007 | none | credit notes (381/81) with IBT-007 |
| `ibr-127-ae` | implemented | error | IBT-009 | none | invoices only (the 381/81 and CreditNoteTypeCode 261 disjuncts: 261 is never a credit note root); PayableAmount > 0 needs a valid IBT-115 |
| `ibr-138-ae` | implemented | error | IBT-073 | none | a period exists when IBT-073, IBT-074 or BTAE-06 is present |
| `ibr-140-ae` | implemented | error | IBT-006 | llm | exact comparison with AED |
| `ibr-141-ae` | implemented | error | IBT-007 | none | < between two untyped values is a string comparison in XPath; an absent IBT-002 makes it false |
| `ibr-142-ae` | implemented | error | IBT-075 | none | BTAE-02 position 7; one finding at the first missing of line1 / city / country_subdivision (term IBT-075 / IBT-077 / IBT-079) |
| `ibr-152-ae` | implemented | error | IBT-075 | none | BTAE-02 position 8 and a deliver-to country other than AE (or absent); one finding at the first missing of line1 / city / country_subdivision |
| `ibr-154-ae` | implemented | error | BTAE-02 | llm | XPath is authoritative: exactly 8 characters of 0/1 (the message says no more than 8); an absent code fails |
| `ibr-157-ae` | implemented | error | BTAE-02 | none | IBT-003 480 or 81 with BTAE-02 position 2, 3 or 4 |
| `ibr-158-ae` | implemented | error | BTAE-03 | none | IBT-003 exactly 381 without BTAE-03 (81 is ibr-055-ae's) |
| `ibr-159-ae` | implemented | error | BTAE-04 | none | an absent IBT-005 fails both branches |
| `ibr-160-ae` | implemented | error | IBT-022 | none | BTAE-06 exactly OTH without IBT-022 |
| `ibr-191-ae` | implemented | error | IBT-081 | none | invoices only, no deemed supply (BTAE-02 position 2), and no instruction with IBT-081 |
| `ibr-192-ae` | implemented | error | IBT-084 | none | IBT-081 exactly 30 without PayeeFinancialAccount/ID |
| `ibr-193-ae` | implemented | error | BTAE-07 | none | absent or blank BTAE-07 |
| `ibr-196-ae` | structural | error | BTAE-22 | none | DeliveryTerms is written only for a present BTAE-22, as its ID. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-co-19` | structural | error | IBG-14 | none | a document-level cac:InvoicePeriod is written only with a child: IBT-073 or IBT-074 satisfy the test, and BTAE-06 alone satisfies its third disjunct (line periods are ibr-co-20's). Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-05` | structural | error | IBT-020 | none | IBT-020 is one string per PaymentTerms element. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-06` | structural | error | IBT-025 | none | each BillingReference holds one InvoiceDocumentReference. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-07` | implemented | error | IBT-025 | none | same condition as ibr-055 |
| `ibr-sr-27` | structural | error | IBT-081 | none | IBT-081 is one string per PaymentMeans element. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-28` | structural | error | IBT-089 | none | IBT-089 is one string per PaymentMandate (the debited account is PayerFinancialAccount/ID, a grandchild). Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-33` | structural | error | IBT-123 | none | IBT-123 is one string per AdditionalDocumentReference. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-39` | structural | error | IBT-011 | none | IBT-011 is one string, written as one cac:ProjectReference/ID. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-46` | structural | error | IBT-082 | none | IBT-082 is one string, the name attribute of the single PaymentMeansCode. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-49` | structural | error | IBT-007 | none | the test counts InvoicePeriod/DescriptionCode (BTAE-06), one string in the single document-level period. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-51` | structural | error | IBT-022 | none | IBT-022 is one string, written as one root cbc:Note. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-52` | structural | error | IBT-017 | none | IBT-017 is one string, written as one OriginatorDocumentReference/ID. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-56` | structural | error | IBT-165 | none | the address has one line3 field, written as one AddressLine/Line. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-59` | structural | error | IBT-174 | none | the institution address has one line3 field, written as one AddressLine/Line. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-60` | structural | error | IBT-187 | none | IBT-187 is one string per PaymentTerms element. Proof: rules::header::tests::structural_rules_hold_on_every_export. |
| `ibr-sr-63` | implemented | error | IBT-024 | llm | any '*' in the defaulted, trimmed IBT-024 |

## parties (71)

| Rule | Status | Severity | Business term | Fix | Note |
|---|---|---|---|---|---|
| `aligned-ibrp-sr-12` | structural | error | IBT-031 | none | structural: the exporter writes the seller VAT PartyTaxScheme at most once (seller_trn is one field); test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-006` | implemented | error | IBT-027 | llm | root context: seller.name is absent (RegistrationName is not written); fires even when no supplier Party exists |
| `ibr-007` | implemented | error | IBT-044 | llm | root context: buyer.name is absent |
| `ibr-007-ae` | implemented | error | BTAE-01 | llm | transaction_type_code matches ^1[01]{7}$ and beneficiary_id is absent (BuyerCustomerParty is not written) |
| `ibr-008` | implemented | error | IBG-05 | llm | root context: all seven seller.postal_address fields are empty, so no PostalAddress is written; the path names the first of the three fields ibr-143-ae requires |
| `ibr-009` | implemented | error | IBT-040 | llm | a seller PostalAddress is written and country_code is absent |
| `ibr-010` | implemented | error | IBG-08 | llm | root context: all seven buyer.postal_address fields are empty; the path names the first of the three fields ibr-144-ae requires |
| `ibr-010-ae` | implemented | error | BTAE-19 | llm | buyer CompanyID written with schemeAgencyID PAS (legal_registration.type, exact) and a schemeAgencyName (passport_issuing_country when PAS) that is absent or not in code list ibr-010-ae; the context is the buyer Party, which exists whenever it has a CompanyID |
| `ibr-011` | implemented | error | IBT-055 | llm | a buyer PostalAddress is written and country_code is absent |
| `ibr-011-ae` | upstream_noop | error | BTAE-19 | none | upstream_noop: the context is an attribute node (@schemeAgencyName) and the compiled XSLT applies templates to elements only, so the assert never fires on any document; the official run agrees on fixtures ibr-010-ae#2 and ibr-012-ae#2 (a code in neither list, no ibr-011-ae); test rules::parties::tests::upstream_noop_rules_are_never_expected |
| `ibr-012-ae` | implemented | error | BTAE-18 | llm | same as ibr-010-ae for the seller (code list ibr-012-ae); a lower-case or padded type is not PAS, and the authority name is never read for PAS |
| `ibr-013-ae` | upstream_noop | error | BTAE-18 | none | upstream_noop: the context is an attribute node (@schemeAgencyName) and the compiled XSLT applies templates to elements only, so the assert never fires on any document; the official run agrees on fixtures ibr-010-ae#2 and ibr-012-ae#2 (a code in neither list, no ibr-013-ae); test rules::parties::tests::upstream_noop_rules_are_never_expected |
| `ibr-017` | implemented | error | IBT-059 | llm | a PayeeParty is written and its name is absent, or equals the seller TRADING name (PartyName, not RegistrationName), or its identifier id equals one of the seller identifier ids (schemes ignored) |
| `ibr-018` | implemented | error | IBT-062 | llm | a TaxRepresentativeParty is written (any of name, address, VAT identifier) and its name is absent |
| `ibr-019` | implemented | error | IBG-12 | llm | a TaxRepresentativeParty is written and all seven address fields are empty |
| `ibr-020` | implemented | error | IBT-069 | llm | a tax representative PostalAddress is written and country_code is absent |
| `ibr-056` | implemented | error | IBT-063 | llm | a TaxRepresentativeParty is written and vat_identifier is absent |
| `ibr-062` | implemented | error | IBT-034-1 | llm | the seller EndpointID is written (id present) without schemeID |
| `ibr-063` | implemented | error | IBT-049-1 | llm | the buyer EndpointID is written (id present) without schemeID |
| `ibr-068` | implemented | error | IBT-034 | llm | scheme 0088: only digits and the u:gln check digit; scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-069` | implemented | error | IBT-034 | llm | scheme 0192: nine digits and u:mod11; scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-070` | implemented | error | IBT-034 | llm | scheme 0184: ten characters, DK and eight digits, on the text itself (text(), no normalize-space); scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-080` | implemented | error | IBT-049 | llm | a buyer Party is written (any buyer content or buyer_trn) and has no EndpointID; no buyer content at all means no context |
| `ibr-081` | implemented | error | IBT-034 | llm | a supplier Party is written and has no EndpointID; no seller content at all means no context |
| `ibr-098` | structural | error | IBT-027 | none | structural: RegistrationName is one field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-099` | structural | error | IBT-028 | none | structural: the supplier PartyName is one field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-100` | structural | error | IBT-030 | none | structural: the supplier PartyLegalEntity has one CompanyID; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-101` | structural | error | IBT-033 | none | structural: CompanyLegalForm is one field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-101-ae` | implemented | error | BTAE-11 | llm | buyer CompanyID with schemeAgencyID TL (exact) and no schemeAgencyName (authority_name for every type but PAS) |
| `ibr-102` | structural | error | IBT-044 | none | structural: RegistrationName is one field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-103` | structural | error | IBT-047 | none | structural: the buyer PartyLegalEntity has one CompanyID; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-104` | implemented | error | IBT-048 | none | the exporter writes buyer_trn (VAT) and buyer.tax_registration_identifier (TIN) as two PartyTaxScheme/CompanyID, so both present fire; same condition as ibr-179-ae |
| `ibr-105` | structural | error | IBT-060 | none | structural: the payee has one identifier field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-106` | structural | error | IBT-061 | none | structural: the payee has one legal registration field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-112` | structural | error | IBT-045 | none | structural: the buyer PartyName is one field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-113` | implemented | error | IBT-034 | llm | scheme 0208: ten digits and u:mod97-0208; scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-114` | implemented | error | IBT-034 | llm | scheme 0201: u:checkCodiceIPA, six ASCII letters or digits; scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-115` | implemented | error | IBT-034 | llm | schemes 0210 and 9907: u:checkCF; scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-116` | implemented | error | IBT-034 | llm | schemes 0211 and 9906: u:checkPIVAseIT; a sign-prefixed value is a dynamic error in the official XSLT and is reported here; scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-120` | implemented | error | IBT-034 | llm | scheme 0151: eleven digits and u:abn; scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-127` | implemented | error | IBT-034 | llm | scheme 0007: ten characters after normalize-space and a valid xs:double lexical form (number() is not NaN); scheme-specific identifier format; context nodes: seller and buyer endpoint, seller and buyer identifiers, payee identifier, seller, buyer and payee legal registration identifier whose scheme_id is the scheme; one finding per failing node at that node's own path with its own term (F7); an empty or scheme-less node is not a context |
| `ibr-128-ae` | implemented | error | IBT-068 | llm | country_code AE and country_subdivision absent or not one of the seven emirates; in the compiled XSLT the seller and buyer PostalAddress nodes are taken by ibr-143-ae and ibr-144-ae (higher priority in the same mode), so only the tax representative address is ever checked (fixture ibr-128-ae#2); the only context therefore has one path |
| `ibr-132-ae` | implemented | error | IBT-031 | llm | path varies by context (F7): seller_trn (IBT-031) and buyer_trn (IBT-048) when that party's PostalAddress country_code is exactly AE and the VAT identifier is not 15 ASCII digits starting with 1 and ending with 03; the official context is cac:Party/PartyTaxScheme, so the tax representative (no Party element) and the TIN schemes are not checked |
| `ibr-134-ae` | implemented | error | IBT-031 | llm | seller_trn absent and invoice_type_code is neither 480 nor 81 (including an absent code) |
| `ibr-135-ae` | implemented | error | IBT-046 | llm | a buyer Party is written, transaction_type_code is not ^[01]{7}1$, the endpoint scheme is 0235 and the endpoint is not ^1\d{9}$ (Unicode digits), and the buyer has neither a PartyIdentification nor a PartyTaxScheme/CompanyID (buyer_trn or buyer TIN) |
| `ibr-136-ae` | implemented | error | IBT-047 | llm | invoice_type_code is 480 or 81 and the buyer has no CompanyID |
| `ibr-137-ae` | implemented | error | BTAE-14 | llm | transaction_type_code matches ^[01]{5}1[01]{2}$ and principal_id is absent |
| `ibr-143-ae` | implemented | error | IBG-05 | llm | path and term vary by field (F7): a seller PostalAddress is written and one of line1 (IBT-035), city (IBT-037), country_subdivision (IBT-039) is absent; one finding per address, at the first absent field |
| `ibr-144-ae` | implemented | error | IBG-08 | llm | path and term vary by field (F7): a buyer PostalAddress is written and one of line1 (IBT-050), city (IBT-052), country_subdivision (IBT-054) is absent; one finding per address, at the first absent field |
| `ibr-148-ae` | implemented | error | IBT-032 | llm | the seller TIN (PartyTaxScheme with a scheme other than VAT) is present and is not ^1[0-9]{9}$ |
| `ibr-149-ae` | implemented | error | IBT-048 | llm | the XPath, not the message, decides: a buyer Party is written and (no endpoint scheme other than 0235) and the endpoint does not start with 1 or 9 and the buyer has no PartyTaxScheme/CompanyID (buyer_trn or buyer TIN); an absent endpoint or scheme counts as failing the first two |
| `ibr-150-ae` | implemented | error | IBT-030 | llm | a supplier Party is written, its endpoint scheme is 0235 and it has no CompanyID |
| `ibr-172-ae` | implemented | error | BTAE-12 | llm | seller CompanyID with schemeAgencyID TL (exact) and no schemeAgencyName |
| `ibr-173-ae` | implemented | error | BTAE-15 | llm | seller CompanyID present, endpoint scheme 0235, seller country_code exactly AE, and schemeAgencyID absent or not one of TL, EID, PAS, CD |
| `ibr-176-ae` | implemented | error | BTAE-14 | llm | disclosed agent billing and there is no supplier PartyTaxScheme/CompanyID (VAT or TIN) different from the principal PartyIdentification/ID; an absent principal id also fails (existential !=) |
| `ibr-177-ae` | implemented | error | IBT-032 | llm | disclosed agent billing, a supplier Party is written and it has neither seller_trn nor a seller TIN |
| `ibr-178-ae` | structural | error | IBT-031-1 | none | structural: the exporter writes the seller PartyTaxScheme with scheme VAT (seller_trn) and TIN (TIN identifier), at most two with exactly one VAT; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-179-ae` | implemented | error | IBT-048 | none | buyer_trn and buyer.tax_registration_identifier both present: two PartyTaxScheme/CompanyID in the buyer Party; same condition as ibr-104 |
| `ibr-180-ae` | implemented | error | BTAE-16 | llm | buyer endpoint scheme 0235, buyer CompanyID present, schemeAgencyID absent |
| `ibr-181-ae` | implemented | error | BTAE-15 | llm | seller endpoint scheme 0235, seller CompanyID present, schemeAgencyID absent |
| `ibr-183-ae` | implemented | error | BTAE-16 | llm | buyer CompanyID present, endpoint scheme 0235, the endpoint does not start with 1 or 9 and schemeAgencyID is absent or not one of TL, CL, EID, PAS, CD |
| `ibr-co-26` | implemented | error | IBT-031 | llm | a supplier Party is written and has no PartyTaxScheme/CompanyID (VAT or TIN), no PartyIdentification/ID and no PartyLegalEntity/CompanyID |
| `ibr-sr-16` | implemented | error | IBT-046 | none | the exporter writes every buyer identifier that has an id, so a second one fires; the path is the second written identifier |
| `ibr-sr-19` | structural | error | IBT-059 | none | structural: the payee name is one field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-sr-22` | structural | error | IBT-062 | none | structural: the tax representative name is one field; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-sr-23` | upstream_noop | error | IBT-063 | none | upstream_noop: the XPath counts cac:Party/cac:PartyTaxScheme/cbc:CompanyID below TaxRepresentativeParty, which has no cac:Party child in the UBL 2.1 XSD, so it is 0 on every valid document; test rules::parties::tests::upstream_noop_rules_are_never_expected |
| `ibr-sr-42` | structural | error |  | none | structural: the supplier Party has a VAT and a TIN PartyTaxScheme at most; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-sr-53` | structural | error | IBT-162 | none | structural: line3 is one AddressLine; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-sr-54` | structural | error | IBT-163 | none | structural: line3 is one AddressLine; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-sr-55` | structural | error | IBT-164 | none | structural: line3 is one AddressLine; test rules::parties::tests::structural_rules_hold_on_every_exported_party |
| `ibr-sr-57` | structural | error |  | none | structural: the exporter writes a PartyTaxScheme only together with its CompanyID; test rules::parties::tests::structural_rules_hold_on_every_exported_party |

## lines (46)

| Rule | Status | Severity | Business term | Fix | Note |
|---|---|---|---|---|---|
| `aligned-ibrp-004` | implemented | error | IBT-146 | value | fires when IBT-148 is present and IBT-146 or IBT-147 is absent or net != gross - discount (exact decimals); suggested_value = gross - discount when both are present |
| `ibr-006-ae` | implemented | error | BTAE-09 | llm | membership in codelists.tsv list ibr-006-ae (exact, case-sensitive); every written NatureCode is a line's BTAE-09 |
| `ibr-016` | implemented | error | IBG-25 | none | root-level: fires when the invoice has no written line (a line without a written child is dropped by the exporter); there is no field for a missing aggregate, so the path points at the first line's identifier |
| `ibr-021` | implemented | error | IBT-126 | llm | whitespace-only counts as absent |
| `ibr-022` | implemented | error | IBT-129 | llm | an invalid decimal is not exported, so it is absent here and AE-FMT-001 reports it |
| `ibr-023` | implemented | error | IBT-130 | llm | the unit attribute is written only with the quantity element, so a line without a quantity fails this rule too |
| `ibr-024` | implemented | error | IBT-131 | llm |  |
| `ibr-025` | implemented | error | IBT-153 | llm |  |
| `ibr-026` | implemented | error | IBT-146 | llm |  |
| `ibr-027` | implemented | error | IBT-146 | llm | the XPath (cac:Price/cbc:PriceAmount) >= 0 is false for an absent price, so a line without IBT-146 fails this rule as well as ibr-026 |
| `ibr-028` | implemented | error | IBT-148 | llm | fires only for a present, negative gross price |
| `ibr-030` | implemented | error | IBT-135 | llm | both dates present and real calendar dates (AE-EXP-008 reports the others) and end < start |
| `ibr-064` | implemented | error | IBT-157-1 | llm | the identifier is written only with a non-empty id |
| `ibr-065` | implemented | error | IBT-158-1 | llm | one finding per written classification code (non-empty code) without a scheme |
| `ibr-083` | structural | error | IBT-147 | none | structural: the exporter always writes the price-level ChargeIndicator false (doc::PRICE_CHARGE_INDICATOR); test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-085` | implemented | error | IBT-134 | llm | both start dates present and real calendar dates and line start < invoicing-period start |
| `ibr-086` | implemented | error | IBT-135 | llm | both end dates present and real calendar dates and line end > invoicing-period end |
| `ibr-087` | implemented | error | IBT-149 | llm | fires for a present base quantity that is zero or negative |
| `ibr-088` | implemented | error | IBT-150 | value | context is a base quantity with a unit; fires when the line has a quantity and its unit differs or is absent; suggested_value = IBT-130 when present |
| `ibr-089` | structural | error | IBT-128 | none | structural: object_identifier is a single message, so at most one DocumentReference 130 per line is written; test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-092` | implemented | error | IBT-184 | none | one finding per line with IBT-184 while IBT-016 is present |
| `ibr-104-ae` | implemented | error | BTAE-10 | llm | path varies: BTAE-10 when absent, else BTAE-08 (F7); the tax category must exist with a code other than E (an absent code counts) |
| `ibr-109` | structural | error | IBT-132 | none | structural: one OrderLineReference with one LineID per line; test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-110` | structural | error | IBG-26 | none | structural: period is a single message, so at most one InvoicePeriod per line is written; test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-111` | structural | error | IBT-147 | none | structural: one price AllowanceCharge with one Amount; test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-111-ae` | implemented | error | IBT-152 | llm | category code N with a rate that is absent, zero or negative |
| `ibr-123-ae` | implemented | error | IBT-151 | llm | the XPath constrains only type codes 81 and 480 (contract rule 13): fires for those without any line VAT information; the repeat limit cannot fail (a line has one tax message) |
| `ibr-125-ae` | implemented | error | IBT-154 | llm | context is an Item element, written when any item field, the line VAT information or the batch number is present |
| `ibr-126-ae` | implemented | error | IBT-149 | llm | path varies (F7): the base quantity when absent, else the gross price; one finding per Price element |
| `ibr-145-ae` | implemented | error | IBT-151 | llm | fires when no ClassifiedTaxCategory is written, that is when none of IBT-151, IBT-152, IBT-186, IBT-185 and IBT-167 is present |
| `ibr-147-ae` | implemented | error | IBT-131 | value | exact-decimal (contract upstream defect 5) with XPath round() to two decimals; an absent term or a zero base quantity makes the quotient empty or infinite, so the rule fires; suggested_value = the computed amount when it is defined |
| `ibr-166-ae` | implemented | error | BTAE-09 | llm | category code AE and no written NatureCode |
| `ibr-167-ae` | implemented | error | IBT-186 | llm | category code E without an exemption reason code |
| `ibr-184-ae` | implemented | error | IBT-158 | llm | item type G and no written classification code (a classification with an empty code is not written) |
| `ibr-185-ae` | implemented | error | BTAE-17 | llm | item type S and no written service accounting code |
| `ibr-186-ae` | implemented | error | IBT-158 | llm | item type B; path varies: the classification when missing, else the service accounting code (BTAE-17) |
| `ibr-187-ae` | upstream_noop | error | IBT-158 | none | upstream_noop: the official test is the constant true() (contract upstream defect 4); test rules::lines::tests::ibr_187_ae_is_a_constant_true_upstream and the constant UPSTREAM_NOOP |
| `ibr-188-ae` | implemented | error | IBT-158-1 | value | fires when a classification code is written and no written code has scheme HS; the path is the first written code; suggested_value = HS |
| `ibr-189-ae` | implemented | error | BTAE-17-1 | value | fires when a service accounting code is written and no written code has scheme SAC; the path is the first written code; suggested_value = SAC |
| `ibr-194-ae` | implemented | error | BTAE-10 | llm | the ItemPriceExtension is written only with BTAE-10 |
| `ibr-co-20` | structural | error | IBG-26 | none | structural: the exporter never writes an empty element, so an InvoicePeriod without a date is not written; test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-sr-34` | structural | error | IBT-127 | none | structural: note is a single string; test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-sr-38` | structural | error | IBT-185 | none | structural: one tax message with one exemption reason text; test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-sr-50` | structural | error | IBT-154 | none | structural: description is a single string; test rules::lines::tests::structural_rules_hold_on_every_exported_line |
| `ibr-sr-58` | implemented | error | IBT-151 | llm | context is a written ClassifiedTaxCategory: fires when VAT information has fields (rate, reason, scheme) but no category code; invoices only: the official context has no CreditNoteLine alternative (upstream defect 6), so the official schematron never fires it on a credit note |
| `ibr-sr-62` | structural | error | IBT-184 | none | structural: one DocumentReference per DespatchLineReference; test rules::lines::tests::structural_rules_hold_on_every_exported_line |

## totals (41)

| Rule | Status | Severity | Business term | Fix | Note |
|---|---|---|---|---|---|
| `aligned-ibrp-032` | implemented | error | IBT-095 | llm | document-level allowances only (a line allowance's parent is the line): fails without a written category code in the VAT scheme (upper-cased, normalize-space); the category is written whenever one of its fields is set |
| `aligned-ibrp-037` | implemented | error | IBT-102 | llm | document-level charges only: fails without a written category code in the VAT scheme |
| `aligned-ibrp-057` | implemented | error | IBT-093 | llm | path varies (F7): document and line allowances (IBT-093/IBT-094, IBT-137/IBT-138), at the missing one of base amount and percentage |
| `aligned-ibrp-058` | implemented | error | IBT-100 | llm | path varies (F7): document and line charges (IBT-100/IBT-101, IBT-142/IBT-143), at the missing one of base amount and percentage |
| `ibr-012` | implemented | error | IBT-106 | llm | context LegalMonetaryTotal, written when any of IBT-106 to IBT-109 or IBT-112 to IBT-115 parses (otherwise AE-EXP-007 reports it); ibr-co-10 suggests the value |
| `ibr-013` | implemented | error | IBT-109 | llm | context LegalMonetaryTotal; ibr-co-13 suggests the value |
| `ibr-014` | implemented | error | IBT-112 | llm | context LegalMonetaryTotal; ibr-co-15 suggests the value |
| `ibr-015` | implemented | error | IBT-115 | llm | context LegalMonetaryTotal; ibr-co-16 suggests the value |
| `ibr-031` | implemented | error | IBT-092 | llm | document-level allowances only (a line allowance passes by the ancestor clause) |
| `ibr-033` | implemented | error | IBT-097 | llm | document-level allowances only |
| `ibr-036` | implemented | error | IBT-099 | llm | document-level charges only |
| `ibr-038` | implemented | error | IBT-104 | llm | document-level charges only; ibr-044 fails with it |
| `ibr-041` | implemented | error | IBT-136 | llm | line allowances only; the price discount is matched first by the cac:Price/cac:AllowanceCharge rule of the same pattern |
| `ibr-042` | implemented | error | IBT-139 | llm | line allowances only, not the price discount |
| `ibr-043` | implemented | error | IBT-141 | llm | line charges only |
| `ibr-044` | implemented | error | IBT-144 | llm | path varies (F7): the XPath has no line condition, so a document-level charge without reason or reason code fails it too (at allowances_charges[#].reason, IBT-104) |
| `ibr-053` | implemented | error | IBT-111 | llm | fails when IBT-006 is present and no TaxTotal/TaxAmount anywhere has currencyID IBT-006: IBT-111, IBT-110 when IBT-005 equals IBT-006, and the line BTAE-08 amounts (currencyID AED) all count |
| `ibr-082` | structural | error |  | none | structural: every exported AllowanceCharge has exactly one ChargeIndicator, true or false (is_charge is a bool; the price discount is always false), so the generic cac:AllowanceCharge rule is never reached; test: src/rules/totals.rs::tests::structural_rules_hold_on_every_exported_allowance_charge |
| `ibr-084` | implemented | error | IBT-111 | llm | existential comparisons over the root TaxTotal amounts whose currencyID equals normalize-space(IBT-006) and normalize-space(IBT-005) (an empty IBT-005 selects currencyID ""); fails when either set is empty |
| `ibr-091` | implemented | error | IBT-115 | llm | lexical: characters after the first point of the written value (decimal::fraction_digits), so 1050.000 fails |
| `ibr-114-ae` | implemented | error | IBT-102 | llm | document-level charges (a line charge has no tax category); the trimmed code equals N |
| `ibr-115-ae` | implemented | error | IBT-095 | llm | document-level allowances; the trimmed code equals N |
| `ibr-121` | implemented | error | IBT-107 | llm | lexical decimals of the written value |
| `ibr-122` | implemented | error | IBT-108 | llm | lexical decimals of the written value |
| `ibr-123` | implemented | error | IBT-109 | llm | lexical decimals of the written value |
| `ibr-125` | implemented | error | IBT-112 | llm | lexical decimals of the written value |
| `ibr-131-ae` | implemented | error | IBT-092 | llm | path varies (F7): document and line allowances; exact decimal instead of xs:double (contract upstream defect 5): the amount equals base x percentage / 100 or that value rounded half up to two decimals; not fix=value because a new amount changes ibr-co-11, ibr-147-ae and the VAT breakdown, which may have passed |
| `ibr-146-ae` | implemented | error | IBT-099 | llm | path varies (F7): document and line charges; exact decimal (contract upstream defect 5), as ibr-131-ae |
| `ibr-153-ae` | implemented | error | BTAE-04 | llm | TaxExchangeRate is written only with BTAE-04 and then always carries IBT-005 to IBT-006, so the rule fails exactly when IBT-006 is AED, IBT-005 is present and not AED, and BTAE-04 is absent |
| `ibr-168-ae` | implemented | error | IBT-098 | llm | the XPath tests the allowance reason code (cbc:AllowanceChargeReasonCode, IBT-098), not the exemption reason code its message names (contract rule 13) |
| `ibr-169-ae` | implemented | error | IBT-105 | llm | the XPath tests the charge reason code (IBT-105), as ibr-168-ae |
| `ibr-175-ae` | implemented | error | BTAE-20 | llm | path varies (F7), first failing condition: currency (IBT-005, absent fails too), tax_currency (IBT-006 not AED), totals.tax_amount_accounting_currency (IBT-111), else BTAE-20 |
| `ibr-co-10` | implemented | error | IBT-106 | value | suggested_value = xpath_round2(sum of IBT-131) when every line's IBT-131 parses; chain IBT-106 -> IBT-109: the suggestion can break only ibr-co-13, which then suggests from the new value |
| `ibr-co-11` | implemented | error | IBT-107 | value | suggested_value = xpath_round2(sum of IBT-092) when there is a document-level allowance and every amount parses; none without allowances (zero and absent both pass); can break only ibr-co-13 |
| `ibr-co-12` | implemented | error | IBT-108 | value | suggested_value = xpath_round2(sum of IBT-099) when there is a document-level charge and every amount parses; can break only ibr-co-13 |
| `ibr-co-13` | implemented | error | IBT-109 | value | holds when IBT-200 is written (true, with IBT-110); branches by the presence of IBT-107 and IBT-108; suggested_value = xpath_round2(IBT-106 + IBT-108 - IBT-107) from the current values, or IBT-106 itself when neither total is present and it has at most two decimals; can break only ibr-co-15 |
| `ibr-co-15` | implemented | error | IBT-112 | value | VAT term = the first root TaxTotal's amount in IBT-005 (IBT-110, or IBT-111 when IBT-110 is absent and IBT-006 = IBT-005); suggested_value = xpath_round2(IBT-109 + that amount); can break only ibr-co-16 |
| `ibr-co-16` | implemented | error | IBT-115 | value | IBT-113 and IBT-114 count only when non-zero (decimal::ebv); suggested_value = the one two-decimal IBT-115 that passes (with a rounding amount: the two-decimal value nearest IBT-112 - IBT-113 + IBT-114, halves down), withheld when it would newly make ibr-127-ae fail |
| `ibr-sr-30` | structural | error | IBT-097 | none | structural: reason is a single string, so every allowance carries at most one AllowanceChargeReason; test: src/rules/totals.rs::tests::structural_rules_hold_on_every_exported_allowance_charge |
| `ibr-sr-31` | structural | error | IBT-104 | none | structural: reason is a single string, so every charge carries at most one AllowanceChargeReason; test: src/rules/totals.rs::tests::structural_rules_hold_on_every_exported_allowance_charge |
| `ibr-sr-61` | structural | error | IBT-197 | none | structural: exemption_reason_text is a single string and only document-level allowances and charges carry a TaxCategory; test: src/rules/totals.rs::tests::structural_rules_hold_on_every_exported_allowance_charge |

## vat (57)

| Rule | Status | Severity | Business term | Fix | Note |
|---|---|---|---|---|---|
| `aligned-ibrp-045` | implemented | error | IBT-116 | llm | fires on every written TaxSubtotal without a valid IBT-116; a breakdown entry is written only together with IBT-110 (the document-currency TaxTotal) |
| `aligned-ibrp-046` | implemented | error | IBT-117 | llm | fires on every written TaxSubtotal without a valid IBT-117 |
| `aligned-ibrp-047` | implemented | error | IBT-118 | llm | fires when the breakdown entry has no category, a category whose scheme is not VAT (normalize-space, upper-case) or a category without a code |
| `aligned-ibrp-048` | implemented | error | IBT-119 | llm | passes with a valid rate or a VAT-scheme category whose code is O or E |
| `aligned-ibrp-ae-01-ae` | implemented | error | IBT-118 | none | root-level: fires when an AE category (VAT scheme) is written on a breakdown entry, a document allowance/charge or a line but no breakdown entry carries one; no field for a missing entry, so the path points at the first breakdown entry's code |
| `aligned-ibrp-ae-05-ae` | implemented | error | IBT-152 | llm | line category AE (VAT scheme) without a valid rate |
| `aligned-ibrp-ae-06` | implemented | error | IBT-096 | value | document allowance category AE (VAT scheme): the rate must be present and 0; suggested_value 0 |
| `aligned-ibrp-ae-07` | implemented | error | IBT-103 | value | document charge category AE (VAT scheme): the rate must be present and 0; suggested_value 0 |
| `aligned-ibrp-ae-08-ae` | implemented | error | IBT-116 | value | exact equality (no slack) with the lines of code AE plus charges minus allowances of code AE; no written line fails it; suggested_value = that sum when a line is written |
| `aligned-ibrp-ae-09-ae` | implemented | error | IBT-117 | value | an absent tax amount is not equal to 0; suggested_value 0.00 |
| `aligned-ibrp-e-01` | implemented | error | IBT-118 | none | root-level: an E category (VAT scheme) written anywhere needs exactly one E breakdown entry; the path points at the first breakdown entry's code |
| `aligned-ibrp-e-05` | implemented | error | IBT-152 | llm | line category E (VAT scheme) with a valid rate |
| `aligned-ibrp-e-06` | implemented | error | IBT-096 | value | document allowance category E (VAT scheme): the rate must be present and 0; suggested_value 0 |
| `aligned-ibrp-e-07` | implemented | error | IBT-103 | value | document charge category E (VAT scheme): the rate must be present and 0; suggested_value 0 |
| `aligned-ibrp-e-08` | implemented | error | IBT-116 | value | exact equality (no slack) with the lines of code E plus charges minus allowances of code E; no written line fails it; suggested_value = that sum when a line is written |
| `aligned-ibrp-e-09` | implemented | error | IBT-117 | value | an absent tax amount is not equal to 0; suggested_value 0.00 |
| `aligned-ibrp-o-01` | implemented | error | IBT-118 | none | root-level: an O category (VAT scheme) written anywhere needs exactly one O breakdown entry; the path points at the first breakdown entry's code |
| `aligned-ibrp-o-05` | implemented | error | IBT-152 | llm | line category O (VAT scheme) with a valid rate |
| `aligned-ibrp-o-06` | implemented | error | IBT-096 | llm | document allowance category O (VAT scheme) with a valid rate |
| `aligned-ibrp-o-07` | implemented | error | IBT-103 | llm | document charge category O (VAT scheme) with a valid rate |
| `aligned-ibrp-o-08` | implemented | error | IBT-116 | value | exact equality (no slack) with the lines of code O plus charges minus allowances of code O; no written line fails it; suggested_value = that sum when a line is written |
| `aligned-ibrp-o-09` | implemented | error | IBT-117 | value | an absent tax amount is not equal to 0; suggested_value 0.00 |
| `aligned-ibrp-o-11-ae` | implemented | error | IBT-119 | llm | breakdown category O (VAT scheme) with a valid rate |
| `aligned-ibrp-s-01` | implemented | error | IBT-118 | none | root-level, no scheme test: fires when S is used by a line or a document allowance/charge but no breakdown entry is S, or when an S breakdown entry exists although nothing else is S; the path points at the first breakdown entry's code |
| `aligned-ibrp-s-05` | implemented | error | IBT-152 | llm | line category S (VAT scheme) whose rate is absent or not greater than 0 |
| `aligned-ibrp-s-06` | implemented | error | IBT-096 | llm | document allowance category S (VAT scheme) whose rate is absent or not greater than 0 |
| `aligned-ibrp-s-07` | implemented | error | IBT-103 | llm | document charge category S (VAT scheme) whose rate is absent or not greater than 0 |
| `aligned-ibrp-s-08` | implemented | error | IBT-116 | llm | slack 0.02 against lines (code S, rate equal) plus charges minus allowances (code S, rate equal); the official test also passes through the branch of the other line kind, where no line exists and the line sum is 0 (replicated, see the rule's doc comment); a breakdown entry without rate or taxable amount fails (the official stylesheet raises a type error on the latter) |
| `aligned-ibrp-s-09` | implemented | error | IBT-117 | llm | rate, taxable amount and tax amount must all be valid; \|tax\| within 0.02 of round2(\|taxable\| * rate / 100) |
| `aligned-ibrp-s-10` | implemented | error | IBT-121 | llm | one finding per breakdown entry: at the reason code when present, else at the reason text (IBT-120) |
| `aligned-ibrp-z-01` | implemented | error | IBT-118 | none | root-level: a Z category (VAT scheme) written anywhere needs exactly one Z breakdown entry; the path points at the first breakdown entry's code |
| `aligned-ibrp-z-05` | implemented | error | IBT-152 | value | line category Z (VAT scheme) whose rate is absent or not 0; suggested_value 0 |
| `aligned-ibrp-z-06` | implemented | error | IBT-096 | value | document allowance category Z (VAT scheme) whose rate is absent or not 0; suggested_value 0 |
| `aligned-ibrp-z-07` | implemented | error | IBT-103 | value | document charge category Z (VAT scheme) whose rate is absent or not 0; suggested_value 0 |
| `aligned-ibrp-z-08` | implemented | error | IBT-116 | value | exact equality (no slack) with the lines of code Z plus charges minus allowances of code Z; no written line fails it; suggested_value = that sum when a line is written |
| `aligned-ibrp-z-09` | implemented | error | IBT-117 | value | an absent tax amount is not equal to 0; suggested_value 0.00 |
| `ibr-102-ae` | implemented | error | IBT-116 | llm | category N (VAT scheme), ASCII N only (upstream defect 1: U+039D is not N): the rate must be valid and a line of code N with that rate must exist; slack 0.02 against the sum of those lines |
| `ibr-103-ae` | implemented | error | IBT-151 | none | one finding per line of category AE (VAT scheme) when the buyer has no PartyTaxScheme CompanyID (buyer_trn or the buyer's TIN) |
| `ibr-105-ae` | implemented | error | IBT-118 | none | root-level: counts TaxCategory elements only (breakdown entries and document allowances/charges), never the lines' ClassifiedTaxCategory; fires when an N category (VAT scheme) is written there and the number of breakdown entries with code N is not 1; the path points at the first breakdown entry's code |
| `ibr-108-ae` | implemented | error | IBT-117 | value | category N (VAT scheme): an absent tax amount is not equal to 0; suggested_value 0.00 |
| `ibr-116-ae` | implemented | error | BTAE-02 | none | root-level: third BTAE-02 flag set and any written category code (breakdown, document allowance/charge or line, any scheme) other than N |
| `ibr-119-ae` | implemented | error | IBT-119 | llm | no scheme test: fires for a code O or E with a rate, and for any other code (or none) without a rate |
| `ibr-120-ae` | implemented | error | IBT-119 | value | category Z (VAT scheme): an absent rate is not equal to 0; suggested_value 0 |
| `ibr-121-ae` | implemented | error | IBT-119 | llm | category E (VAT scheme) with a valid rate |
| `ibr-122-ae` | implemented | error | IBT-003 | none | root-level: type code 81 or 480 and any written category code (breakdown, document allowance/charge or line, any scheme) outside E, O, Z |
| `ibr-124` | implemented | error | IBT-110 | llm | defect 2: applied to credit notes too (allow-listed against the official run by rule id and document kind); the context covers both TaxTotals, so the accounting-currency one is reported at totals.tax_amount_accounting_currency (IBT-111); counts the characters after the first '.' of the written text |
| `ibr-126` | implemented | error | IBT-005 | none | path varies by context: one finding per written amount outside ItemPriceExtension, at that amount's own field and term; fails only when the document currency is absent (every currencyID is the written IBT-005, so none matches a missing DocumentCurrencyCode); tax amounts are not in the context then (their predicate needs DocumentCurrencyCode) |
| `ibr-133-ae` | implemented | error | IBT-118-1 | llm | path varies by context: one finding per TaxScheme/ID that is not exactly VAT (case-sensitive; the TIN party schemes of seller and buyer included) unless a seller CompanyID is ten ASCII digits starting with 1 |
| `ibr-139-ae` | implemented | error | IBT-118 | llm | path varies by context: one finding per written category code outside S, E, O, AE, Z, N (ASCII; the U+039D of the upstream .gc file is rejected, defect 1) |
| `ibr-151-ae` | implemented | error | IBT-003 | none | root-level: type code 380 or 381 and no written line category whose code is neither E nor O (a category without a code counts as neither) |
| `ibr-162-ae` | implemented | error | BTAE-08 | llm | category AE (VAT scheme): BTAE-08 is written only with BTAE-10 (ItemPriceExtension), so it must be present, with BTAE-10, and 0 |
| `ibr-163-ae` | implemented | error | BTAE-08 | llm | category E (VAT scheme): fires when BTAE-08 is written (it needs BTAE-10 too) |
| `ibr-165-ae` | implemented | error | BTAE-08 | llm | category Z (VAT scheme): BTAE-08 is written only with BTAE-10, so it must be present, with BTAE-10, and 0 |
| `ibr-174-ae` | implemented | error | IBT-157 | llm | category AE (VAT scheme): the standard identifier must be written; a written scheme other than 0160 fails at ...standard_id.scheme_id (IBT-157-1), an absent scheme is accepted |
| `ibr-190-ae` | implemented | error | IBT-119 | llm | root-level, one finding: the official test reads TaxCategory elements only (breakdown entries and document allowances/charges, no scheme test), never the lines; fails when a code S category has a written rate other than 5; the path points at the first such rate |
| `ibr-co-14` | implemented | error | IBT-110 | value | defect 2: applied to credit notes too (allow-listed against the official run by rule id and document kind); checked when a breakdown entry is written; vat_amount must equal XPath round2 of the sum of the written IBT-117; suggested_value = that sum |
| `ibr-sr-32` | structural | error | IBT-120 | none | structural: every breakdown entry has one category and the exporter writes at most one TaxExemptionReason per category; test rules::vat::tests::structural_rules_hold_on_every_exported_breakdown |

## codelists (18)

| Rule | Status | Severity | Business term | Fix | Note |
|---|---|---|---|---|---|
| `ibr-cl-01` | implemented | error | IBT-003 | llm | invoice root: 380 or 480; the credit-note root (381, 81) is written only for those codes, so it cannot fail; short lists stay literal (F10) |
| `ibr-cl-03` | implemented | error | IBT-005 | none | path and term vary (F7): one finding per written amount whose currencyID is invalid; IBT-005 is the currencyID of every amount except BTAE-10 and BTAE-08 (AED) and IBT-111 (IBT-006) |
| `ibr-cl-04` | implemented | error | IBT-005 | llm | list ibr-cl-04 of codelists.tsv |
| `ibr-cl-05` | implemented | error | IBT-006 | llm | list ibr-cl-05 of codelists.tsv |
| `ibr-cl-07` | implemented | error | IBT-018-1 | llm | path varies: IBT-018-1 at the document level, IBT-128-1 (lines[#].object_identifier.scheme_id) per line; the scheme is written only with an id |
| `ibr-cl-10` | implemented | error | IBT-046-1 | llm | path varies: seller.identifiers (IBT-029-1), buyer.identifiers (IBT-046-1), payee.identifier (IBT-060-1); SEPA is also valid for the seller and the payee only (literal) |
| `ibr-cl-11` | implemented | error | IBT-030-1 | llm | path varies: seller (IBT-030-1), buyer (IBT-047-1), payee.legal_registration (IBT-061-1); written only with an id |
| `ibr-cl-13` | implemented | error | IBT-158-1 | llm | written only with a non-empty code |
| `ibr-cl-14` | implemented | error | IBT-040 | llm | path varies: seller IBT-040, buyer IBT-055, tax_representative IBT-069, delivery.address IBT-080, payment_instructions[#].credit_transfer.institution_address IBT-175 |
| `ibr-cl-15` | implemented | error | IBT-159 | llm | list ibr-cl-15 (the OriginCountry element, not cac:Country) |
| `ibr-cl-16` | implemented | error | IBT-081 | llm | list ibr-cl-16 |
| `ibr-cl-19` | implemented | error | IBT-098 | llm | path varies: document allowances (IBT-098) and line allowances (lines[#].allowances_charges[#].reason_code, IBT-140); the price-level AllowanceCharge has no reason code |
| `ibr-cl-20` | implemented | error | IBT-105 | llm | path varies: document charges (IBT-105) and line charges (lines[#].allowances_charges[#].reason_code, IBT-145) |
| `ibr-cl-21` | implemented | error | IBT-157 | llm | written only with an id |
| `ibr-cl-23` | implemented | error | IBT-130 | llm | path varies: the quantity unit (IBT-130, written with the quantity) and the base quantity unit (lines[#].price.base_quantity_unit_code, IBT-150, written with the base quantity) |
| `ibr-cl-24` | structural | error | IBT-125-1 | none | structural: the exporter never embeds an attachment (EmbeddedDocumentBinaryObject), see its Not exported list; test rules::codelists::tests::ibr_cl_24_cannot_fail_because_no_attachment_is_exported |
| `ibr-cl-25` | implemented | error | IBT-034-1 | llm | path varies: seller (IBT-034-1) and buyer (IBT-049-1); written only with an id |
| `ibr-cl-26` | implemented | error | IBT-071-1 | llm | written only with an id |

## platform (12)

| Rule | Status | Severity | Business term | Fix | Note |
|---|---|---|---|---|---|
| `AE-FMT-001` | implemented | error | IBT-112 | llm | path and term vary by field (F7): one issue per non-empty field of doc::DECIMAL_FIELDS that fails decimal::parse (contract rule 10); that field is absent for every other rule and the invoice is never exported |
| `AE-SCOPE-001` | implemented | error | IBT-024 | none | process.specification_identifier, trimmed and defaulted, starts with urn:peppol:pint:selfbilling-1@ae-1 (contract rule 9, spec 5.2.7); the official aligned-ibrp-001-ae accepts it |
| `AE-EXP-001` | implemented | warning | IBT-009 | none | credit note (381/81) with IBT-009 and no payment_instructions: the CreditNote XSD has no root DueDate, so IBT-009 is written as PaymentMeans/PaymentDueDate of payment_instructions[0] (CI IBT-009 row; export::B::payment_means) |
| `AE-EXP-002` | implemented | error | IBT-110 | value | vat_amount empty while tax_breakdown is non-empty: the document-currency TaxTotal is written only with IBT-110 and TaxTotal/TaxAmount is XSD-mandatory (contract 0.2.1); suggested value xpath_round2(sum of tax_breakdown[#].tax_amount), the IBT-110 ibr-co-14 accepts, when every IBT-117 parses; an invalid IBT-110 is AE-FMT-001's |
| `AE-EXP-003` | implemented | error | IBT-006 | none | exchange_rate parses and tax_currency is empty: TaxExchangeRate/TargetCurrencyCode is XSD-mandatory (S7); no official rule requires IBT-006 for an AED invoice (XSD audit) |
| `AE-EXP-004` | implemented | error | IBT-087 | none | a card with holder_name or network_id and no primary_account_number: CardAccount/PrimaryAccountNumberID is XSD-mandatory and no official rule requires it (XSD audit); NetworkID gets the NA placeholder |
| `AE-EXP-005` | implemented | error |  | none | path varies by field (F7): one issue per string field the exporter can write (rules::platform::each_exported_string) whose trimmed text holds U+0000-U+0008, U+000B, U+000C, U+000E-U+001F, U+FFFE or U+FFFF; such a document cannot be serialised, so this rule keeps export from failing on a run without errors |
| `AE-EXP-006` | implemented | error | IBT-012 | none | XSD audit: contract_value parses and contract_reference is empty; ContractDocumentReference/ID is XSD-mandatory and no official rule requires IBT-012 (ibr-094 only counts it) |
| `AE-EXP-007` | implemented | error | IBT-115 | none | XSD audit: no LegalMonetaryTotal amount (IBT-106 to IBT-109, IBT-112 to IBT-115) parses, so the XSD-mandatory LegalMonetaryTotal cannot be written; ibr-012 to ibr-015 need the element and ibr-co-15 passes when IBT-200 is true |
| `AE-EXP-008` | implemented | error | IBT-009 | llm | path and term vary (F7): a credit note's IBT-009 (PaymentMeans/cbc:PaymentDueDate, written when there are payment instructions) and every IBT-177 (cbc:InstallmentDueDate) are xsd:date but outside the ibr-073 context; checked as YYYY-MM-DD with a year from 0001 and a real day (XSD audit) |
| `AE-EXP-009` | implemented | error | IBT-002 | llm | path and term vary (F7): the ibr-073 dates (IBT-002, IBT-007, an invoice's IBT-009, IBT-026, IBT-072, IBT-073, IBT-074, IBT-134, IBT-135); ibr-073 casts with XML Schema 1.1, which accepts the year 0000, while the UBL 2.1 XSD is validated as XML Schema 1.0, which does not (XSD audit) |
| `AE-EXP-010` | implemented | error | IBT-168 | llm | XSD audit: an otherwise valid time with the offset ±14:01 to ±14:59; ibr-119 (castable as xs:time, Saxon) accepts it and the UBL 2.1 XSD (XML Schema 1.0) bounds the offset at ±14:00; that is the whole measured difference (5,040 combinations), every other invalid time is ibr-119's |
