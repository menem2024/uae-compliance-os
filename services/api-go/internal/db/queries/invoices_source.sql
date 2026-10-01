-- name: InsertExtractedInvoice :execrows
-- Idempotent on (document_id, source_ordinal): a redelivered document.extracted inserts nothing.
INSERT INTO invoices (firm_id, status, payload, client_company_id, document_id, source_ordinal, source_ref,
                      extraction_confidence, extraction_run_id)
VALUES (@firm_id, @status, @payload, @client_company_id, @document_id, @source_ordinal, @source_ref,
        @extraction_confidence, @extraction_run_id)
ON CONFLICT (document_id, source_ordinal) DO NOTHING;

-- name: ListDocumentInvoicesForPublish :many
SELECT id, status, payload, extraction_confidence FROM invoices
WHERE document_id = @document_id
ORDER BY source_ordinal;

-- name: ReattributeDocumentInvoices :execrows
UPDATE invoices SET client_company_id = @client_company_id, updated_at = now() WHERE document_id = @document_id;

-- name: CountDocumentInvoices :one
SELECT count(*)::bigint FROM invoices WHERE document_id = @document_id;
