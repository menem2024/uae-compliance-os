-- name: FirmByOrg :one
SELECT * FROM firms WHERE zitadel_org_id = $1;
