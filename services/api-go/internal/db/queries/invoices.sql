-- name: CreateInvoice :one
INSERT INTO invoices (firm_id, status, payload) VALUES ($1, 'uploaded', $2) RETURNING *;

-- name: GetInvoice :one
SELECT * FROM invoices WHERE id = $1;

-- name: SetValidation :one
UPDATE invoices SET status = $2, ruleset_version = $3, issues = $4, updated_at = now() WHERE id = $1 RETURNING id;

-- name: ListInvoiceIDs :many
SELECT id FROM invoices ORDER BY created_at;
