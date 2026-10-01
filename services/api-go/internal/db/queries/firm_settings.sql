-- name: GetFirm :one
SELECT id, name, brand_color, updated_at FROM firms WHERE id = @id;

-- name: UpdateFirm :one
-- Run inside WithFirm: the firms_update_own policy makes another Firm's row invisible (0 rows).
UPDATE firms SET name = @name, brand_color = sqlc.narg('brand_color'), updated_at = now()
WHERE id = @id
RETURNING id, name, brand_color, updated_at;

-- name: ListFirmIDs :many
SELECT id FROM firms ORDER BY id;
