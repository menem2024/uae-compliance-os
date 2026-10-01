-- name: UpsertPendingDocument :one
-- Inserts a pending_upload row, or resets an existing pending_upload/rejected row with the same
-- (firm, client company, sha256). Returns no row (pgx.ErrNoRows) when the existing row is in any
-- other status: the caller then reports deduplicated=true (spec 5.1 step 2). object_key must be
-- documents.ObjectKey(firm_id, id); the documents_object_key_shape CHECK rejects anything else.
INSERT INTO documents (id, firm_id, client_company_id, sha256, object_key, filename, content_type,
                       size_bytes, uploaded_by)
VALUES (@id, @firm_id, @client_company_id, @sha256, @object_key, @filename, @content_type, @size_bytes,
        @uploaded_by)
ON CONFLICT (firm_id, client_company_id, sha256) DO UPDATE
  SET filename = EXCLUDED.filename, content_type = EXCLUDED.content_type, size_bytes = EXCLUDED.size_bytes,
      uploaded_by = EXCLUDED.uploaded_by, status = 'pending_upload', status_reason = '',
      reprocess_nonce = gen_random_uuid()::text, published_at = NULL, publish_attempts = 0,
      updated_at = now()
  WHERE documents.status IN ('pending_upload', 'rejected')
RETURNING *;

-- name: GetDocumentByHash :one
SELECT * FROM documents WHERE client_company_id = @client_company_id AND sha256 = @sha256;

-- name: GetDocument :one
SELECT * FROM documents WHERE id = @id;

-- name: LockDocument :one
SELECT * FROM documents WHERE id = @id FOR UPDATE;

-- name: MarkDocumentUploaded :one
UPDATE documents SET status = 'uploaded', status_reason = '', updated_at = now()
WHERE id = @id AND status = 'pending_upload'
RETURNING *;

-- name: RejectDocument :one
UPDATE documents SET status = 'rejected', status_reason = @reason, updated_at = now()
WHERE id = @id AND status = 'pending_upload'
RETURNING *;

-- name: MarkDocumentPublished :execrows
-- Guarded by the nonce so a stale publish ack never marks a newer reprocess as published.
UPDATE documents SET published_at = now(), publish_attempts = publish_attempts + 1, updated_at = now()
WHERE id = @id AND status = 'uploaded' AND reprocess_nonce = @reprocess_nonce AND published_at IS NULL;

-- name: ListDocuments :many
SELECT * FROM documents
WHERE (sqlc.narg('client_company_id')::uuid IS NULL OR client_company_id = sqlc.narg('client_company_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @page_limit;

-- name: ListDocumentInvoices :many
SELECT id, status, source_ordinal, extraction_confidence FROM invoices
WHERE document_id = @document_id
ORDER BY source_ordinal;

-- name: MarkDocumentProcessing :execrows
-- agent.run.started for a document subject. Never moves a terminal Document backwards.
UPDATE documents SET status = 'processing', latest_run_id = @run_id, updated_at = now()
WHERE id = @id AND status IN ('uploaded', 'processing');

-- name: ApplyDocumentExtracted :execrows
-- First result wins; a redelivered or superseded run's result is a no-op (0 rows).
UPDATE documents
SET status = @status, status_reason = @status_reason, kind = @kind, direction = @direction,
    language = @language, extraction_method = @extraction_method, review_reasons = @review_reasons,
    invoice_count = @invoice_count, latest_run_id = @run_id, updated_at = now()
WHERE id = @id AND status IN ('uploaded', 'processing');

-- name: ApplyDocumentFailed :execrows
UPDATE documents SET status = 'failed', status_reason = @reason, latest_run_id = sqlc.narg('run_id'), updated_at = now()
WHERE id = @id AND status IN ('uploaded', 'processing');

-- name: ReprocessDocument :one
UPDATE documents
SET status = 'uploaded', status_reason = '', kind = '', direction = '', language = '', extraction_method = '',
    review_reasons = '{}', reprocess_nonce = gen_random_uuid()::text, published_at = NULL,
    publish_attempts = 0, updated_at = now()
WHERE id = @id
  AND (status IN ('failed', 'not_invoice') OR (status = 'needs_review' AND invoice_count = 0))
RETURNING *;

-- name: ListUnpublishedDocuments :many
-- Reconciler: committed but never acknowledged by JetStream (api-go died between commit and publish).
SELECT * FROM documents
WHERE status = 'uploaded' AND published_at IS NULL AND updated_at < now() - interval '1 minute'
ORDER BY updated_at
LIMIT 100;

-- name: BumpDocumentPublishAttempt :one
UPDATE documents SET publish_attempts = publish_attempts + 1, updated_at = now()
WHERE id = @id AND status = 'uploaded' AND published_at IS NULL
RETURNING publish_attempts;

-- name: FailUnpublishedDocument :execrows
UPDATE documents SET status = 'failed', status_reason = 'publish_exhausted', updated_at = now()
WHERE id = @id AND status = 'uploaded' AND published_at IS NULL;

-- name: ResetStuckDocuments :many
-- A Document processing for 30 minutes with no running run lost its message: back to uploaded with a
-- new nonce, so the unpublished path above republishes it.
UPDATE documents d
SET status = 'uploaded', status_reason = '', reprocess_nonce = gen_random_uuid()::text,
    published_at = NULL, publish_attempts = 0, updated_at = now()
WHERE d.status = 'processing' AND d.updated_at < now() - interval '30 minutes'
  AND NOT EXISTS (
    SELECT 1 FROM agent_runs r
    WHERE r.subject_type = 'document' AND r.subject_id = d.id::text AND r.status = 'running'
      AND r.updated_at > now() - interval '30 minutes')
RETURNING d.id;

-- name: ReattributeDocument :execrows
-- document.attribution applier: direction is relative to the ClientCompany, so it moves with it.
UPDATE documents SET client_company_id = @client_company_id, direction = @direction, updated_at = now()
WHERE id = @id;
