-- name: CreateClientCompany :one
INSERT INTO client_companies (firm_id, name, name_ar, trn, tin, emirate)
VALUES (@firm_id, @name, @name_ar, sqlc.narg('trn'), sqlc.narg('tin'), sqlc.narg('emirate'))
RETURNING *;

-- name: GetClientCompany :one
SELECT * FROM client_companies WHERE id = @id;

-- name: UpdateClientCompany :one
UPDATE client_companies
SET name = @name, name_ar = @name_ar, trn = sqlc.narg('trn'), tin = sqlc.narg('tin'), emirate = sqlc.narg('emirate'),
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SetClientCompanyStatus :one
UPDATE client_companies SET status = @status, updated_at = now() WHERE id = @id RETURNING *;

-- name: ListClientCompanies :many
-- status: 'active' | 'archived' | 'all'. pattern: an escaped ILIKE pattern ('%foo%') or NULL.
-- Keyset on (name, id); after_name/after_id come from the previous page's last row.
SELECT c.id, c.firm_id, c.name, c.name_ar, c.trn, c.tin, c.emirate, c.status, c.created_at, c.updated_at,
       coalesce(d.total, 0)::bigint AS documents_total,
       coalesce(d.needs_review, 0)::bigint AS documents_needs_review
FROM client_companies c
LEFT JOIN LATERAL (
  SELECT count(*) AS total, count(*) FILTER (WHERE status = 'needs_review') AS needs_review
  FROM documents WHERE documents.client_company_id = c.id
) d ON true
WHERE (@status::text = 'all' OR c.status = @status::text)
  AND (sqlc.narg('pattern')::text IS NULL
       OR c.name ILIKE sqlc.narg('pattern')::text
       OR c.name_ar ILIKE sqlc.narg('pattern')::text
       OR c.trn LIKE sqlc.narg('pattern')::text)
  AND (sqlc.narg('after_name')::text IS NULL
       OR (c.name, c.id) > (sqlc.narg('after_name')::text, sqlc.narg('after_id')::uuid))
ORDER BY c.name, c.id
LIMIT @page_limit;

-- name: ListActiveClientCompanyRefs :many
SELECT id, name, name_ar, trn FROM client_companies WHERE status = 'active' ORDER BY name, id LIMIT 500;
