-- Track C invoice queries (spec §5.5, §5.6). Every query lists its columns (plan finding F11): Track B's
-- migrations add invoice columns, and those must never change a Track C row type. Nullable uuid
-- columns and parameters go through uuid.Nil (COALESCE / NULLIF) so that the generated Go types do not
-- depend on sqlc's nullable-uuid override.

-- name: TrackCGetInvoice :one
SELECT id, status, payload, payload_version,
       COALESCE(latest_run_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS latest_run_id,
       approved_payload_version, approved_by, approved_at, ruleset_version, created_at, updated_at
FROM invoices
WHERE id = @id;

-- name: TrackCLockInvoice :one
-- Same columns as TrackCGetInvoice, with the row locked until the transaction ends.
SELECT id, status, payload, payload_version,
       COALESCE(latest_run_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS latest_run_id,
       approved_payload_version, approved_by, approved_at, ruleset_version, created_at, updated_at
FROM invoices
WHERE id = @id
FOR UPDATE;

-- name: TrackCSetValidationResult :execrows
-- Records a run's outcome on its invoice (spec §5.6.1 step 4), including the legacy ruleset_version and
-- issues columns (Phase 0 IssueView shape). Any status other than 'ready' clears the approval.
-- 0 rows: the invoice is gone or its payload_version changed.
UPDATE invoices
SET status                   = @status::text,
    latest_run_id            = @run_id::uuid,
    ruleset_version          = @ruleset_version::text,
    issues                   = @issues,
    approved_payload_version = CASE WHEN @status::text = 'ready' THEN approved_payload_version END,
    approved_by              = CASE WHEN @status::text = 'ready' THEN approved_by END,
    approved_at              = CASE WHEN @status::text = 'ready' THEN approved_at END,
    updated_at               = now()
WHERE id = @id AND payload_version = @payload_version;

-- name: TrackCApplyPayload :one
-- Stores a changed payload (invoicefix.Apply): next payload_version, status 'fixed', approval cleared.
-- No row: the invoice is gone or its payload_version is no longer expected_payload_version.
UPDATE invoices
SET payload                  = @payload,
    payload_version          = payload_version + 1,
    status                   = 'fixed',
    approved_payload_version = NULL,
    approved_by              = NULL,
    approved_at              = NULL,
    updated_at               = now()
WHERE id = @id AND payload_version = @expected_payload_version
RETURNING payload_version;

-- name: TrackCApproveInvoice :one
-- Approves a clean invoice for export (spec §5.6.4): 'validated' at the given payload_version -> 'ready'.
-- No row: the invoice is gone, not 'validated', or at another payload_version.
UPDATE invoices
SET status                   = 'ready',
    approved_payload_version = payload_version,
    approved_by              = @approved_by::text,
    approved_at              = now(),
    updated_at               = now()
WHERE id = @id AND status = 'validated' AND payload_version = @payload_version
RETURNING payload_version, approved_at;

-- name: TrackCListInvoices :many
-- GET /v1/invoices (decision D-2): newest first, keyset cursor (before_created_at, before_id).
-- q matches the invoice number as a case-insensitive substring (no LIKE wildcards). Money stays text.
SELECT i.id, i.status, i.payload_version,
       COALESCE(i.latest_run_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS latest_run_id,
       COALESCE(i.payload->>'invoice_number', '')::text AS invoice_number,
       COALESCE(i.payload->>'issue_date', '')::text AS issue_date,
       COALESCE(i.payload->>'invoice_type_code', '')::text AS invoice_type_code,
       COALESCE(i.payload->>'currency', '')::text AS currency,
       COALESCE(i.payload->>'total_amount', '')::text AS total_amount,
       COALESCE(i.payload->'seller'->>'name', '')::text AS seller_name,
       COALESCE(i.payload->'buyer'->>'name', '')::text AS buyer_name,
       COALESCE(r.error_count, 0)::integer AS error_count,
       COALESCE(r.warning_count, 0)::integer AS warning_count,
       i.created_at, i.updated_at
FROM invoices i
LEFT JOIN validation_runs r ON r.id = i.latest_run_id
WHERE (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status')::text)
  AND (sqlc.narg('q')::text IS NULL
       OR strpos(lower(COALESCE(i.payload->>'invoice_number', '')), lower(sqlc.narg('q')::text)) > 0)
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL
       OR (i.created_at, i.id) < (sqlc.narg('before_created_at')::timestamptz, @before_id::uuid))
ORDER BY i.created_at DESC, i.id DESC
LIMIT @page_limit;

-- name: TrackCListFirmIDs :many
-- firms is a global table (no RLS): the sweeper and `api revalidate` iterate Firms with it.
SELECT id FROM firms ORDER BY id;

-- name: TrackCListFixedInvoices :many
-- Sweeper (spec §5.6.2): invoices left in 'fixed' (re-validation pending) since before updated_before.
SELECT id, payload_version
FROM invoices
WHERE status = 'fixed' AND updated_at < @updated_before
ORDER BY updated_at, id
LIMIT @max_rows;

-- name: TrackCListInvoicesForRevalidation :many
-- `api revalidate` (spec §5.6.2): invoices whose latest run used another RuleSet, in id order after
-- after_id (pass uuid.Nil to start).
SELECT i.id, i.status, r.id AS run_id, r.ruleset_version
FROM invoices i
JOIN validation_runs r ON r.id = i.latest_run_id
WHERE r.ruleset_version <> @ruleset_version::text AND i.id > @after_id::uuid
ORDER BY i.id
LIMIT @max_rows;
