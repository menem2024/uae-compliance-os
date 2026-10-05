-- Track C Fix agent tasks (spec §5.5, §5.6.8). The fix_tasks_guard trigger allows publishing while
-- 'requested' and one requested -> terminal update. proposal_id and agent_run_id are nullable;
-- uuid.Nil stands for NULL in both directions.

-- name: TrackCInsertFixTask :execrows
-- Idempotent on (run_id, mode): 0 rows means the task exists (read it with TrackCGetFixTaskByRunMode).
INSERT INTO fix_tasks (id, firm_id, invoice_id, run_id, mode, requested_by)
VALUES (@id, @firm_id, @invoice_id, @run_id, @mode, @requested_by)
ON CONFLICT (run_id, mode) DO NOTHING;

-- name: TrackCGetFixTask :one
SELECT id, invoice_id, run_id, mode, status, outcome,
       COALESCE(proposal_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS proposal_id,
       COALESCE(agent_run_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS agent_run_id,
       error_code, requested_by, requested_at, published_at, completed_at
FROM fix_tasks
WHERE id = @id;

-- name: TrackCGetFixTaskByRunMode :one
SELECT id, invoice_id, run_id, mode, status, outcome,
       COALESCE(proposal_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS proposal_id,
       COALESCE(agent_run_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS agent_run_id,
       error_code, requested_by, requested_at, published_at, completed_at
FROM fix_tasks
WHERE run_id = @run_id AND mode = @mode;

-- name: TrackCLatestFixTask :one
-- The invoice's most recent task (invoice detail; fix_in_progress check).
SELECT id, invoice_id, run_id, mode, status, outcome,
       COALESCE(proposal_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS proposal_id,
       COALESCE(agent_run_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS agent_run_id,
       error_code, requested_by, requested_at, published_at, completed_at
FROM fix_tasks
WHERE invoice_id = @invoice_id
ORDER BY requested_at DESC, id DESC
LIMIT 1;

-- name: TrackCMarkFixTaskPublished :execrows
UPDATE fix_tasks SET published_at = now() WHERE id = @id AND status = 'requested';

-- name: TrackCCompleteFixTask :execrows
-- requested -> terminal with the agent's result. 0 rows: unknown task or already terminal (a duplicate).
UPDATE fix_tasks
SET status       = @status,
    outcome      = @outcome,
    proposal_id  = NULLIF(@proposal_id::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
    agent_run_id = NULLIF(@agent_run_id::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
    error_code   = @error_code,
    completed_at = clock_timestamp()
WHERE id = @id AND status = 'requested';

-- name: TrackCListUnpublishedFixTasks :many
-- Sweeper: requested tasks whose publish never succeeded, requested before requested_before.
SELECT id, invoice_id, run_id, mode, status, outcome,
       COALESCE(proposal_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS proposal_id,
       COALESCE(agent_run_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS agent_run_id,
       error_code, requested_by, requested_at, published_at, completed_at
FROM fix_tasks
WHERE status = 'requested' AND published_at IS NULL AND requested_at < @requested_before
ORDER BY requested_at, id
LIMIT @max_rows;

-- name: TrackCTimeOutFixTasks :many
-- Sweeper: closes tasks still 'requested' since before requested_before as failed/timeout.
UPDATE fix_tasks
SET status = 'failed', error_code = 'timeout', completed_at = clock_timestamp()
WHERE status = 'requested' AND requested_at < @requested_before
RETURNING id, invoice_id;
