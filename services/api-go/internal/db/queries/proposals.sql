-- name: SupersedeOpenProposals :execrows
-- Runs before InsertProposal (the partial unique index allows one open proposal per target and kind).
-- A redelivery of an existing proposal supersedes nothing.
UPDATE proposals o SET state = 'superseded'
WHERE o.target_type = @target_type AND o.target_id = @target_id AND o.kind = @kind AND o.state = 'proposed'
  AND o.id <> @new_id
  AND NOT EXISTS (SELECT 1 FROM proposals p WHERE p.id = @new_id);

-- name: InsertProposal :execrows
INSERT INTO proposals (id, firm_id, client_company_id, run_id, agent, kind, target_type, target_id,
                       summary_key, summary_args, rationale, confidence, changes, detail_type, detail,
                       evidence, created_at, expires_at)
VALUES (@id, @firm_id, sqlc.narg('client_company_id'), sqlc.narg('run_id'), @agent, @kind, @target_type,
        @target_id, @summary_key, @summary_args, @rationale, @confidence, @changes, @detail_type, @detail,
        @evidence, @created_at, sqlc.narg('expires_at'))
ON CONFLICT (id) DO NOTHING;

-- name: LockProposal :one
SELECT * FROM proposals WHERE id = @id FOR UPDATE;

-- name: DecideProposal :one
UPDATE proposals
SET state = @state, decided_by = @decided_by, decided_at = now(), decision_reason = @decision_reason,
    applied_at = sqlc.narg('applied_at')
WHERE id = @id AND state = 'proposed'
RETURNING *;

-- name: ExpireProposal :execrows
UPDATE proposals SET state = 'expired' WHERE id = @id AND state = 'proposed';

-- name: ListProposals :many
SELECT * FROM proposals
WHERE (sqlc.narg('state')::text IS NULL OR state = sqlc.narg('state')::text)
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind')::text)
  AND (sqlc.narg('target_type')::text IS NULL OR target_type = sqlc.narg('target_type')::text)
  AND (sqlc.narg('target_id')::uuid IS NULL OR target_id = sqlc.narg('target_id')::uuid)
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @page_limit;

-- name: ListRunProposals :many
SELECT * FROM proposals WHERE run_id = @run_id ORDER BY created_at, id;
