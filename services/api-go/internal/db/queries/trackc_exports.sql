-- Track C exports (spec §5.5, §5.6.5). Append-only; object_key is checked by exports_object_key_shape.

-- name: TrackCInsertExport :one
INSERT INTO exports (id, firm_id, invoice_id, run_id, payload_version, ruleset_version, format, document_kind,
                     object_key, sha256, size_bytes, created_by)
VALUES (@id, @firm_id, @invoice_id, @run_id, @payload_version, @ruleset_version, @format, @document_kind,
        @object_key, @sha256, @size_bytes, @created_by)
RETURNING created_at;

-- name: TrackCGetExport :one
-- invoice_number is the invoice's current number (for the download filename).
SELECT x.id, x.invoice_id, x.run_id, x.payload_version, x.ruleset_version, x.format, x.document_kind,
       x.object_key, x.sha256, x.size_bytes, x.created_by, x.created_at,
       COALESCE(i.payload->>'invoice_number', '')::text AS invoice_number
FROM exports x
JOIN invoices i ON i.id = x.invoice_id
WHERE x.id = @id;

-- name: TrackCListExports :many
-- An invoice's exports, newest first.
SELECT x.id, x.invoice_id, x.run_id, x.payload_version, x.ruleset_version, x.format, x.document_kind,
       x.object_key, x.sha256, x.size_bytes, x.created_by, x.created_at,
       COALESCE(i.payload->>'invoice_number', '')::text AS invoice_number
FROM exports x
JOIN invoices i ON i.id = x.invoice_id
WHERE x.invoice_id = @invoice_id
ORDER BY x.created_at DESC, x.id DESC;
