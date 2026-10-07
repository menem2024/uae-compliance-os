-- Track C validation runs and issues (spec §5.5, §5.6.1). Both tables are append-only.

-- name: TrackCInsertRun :one
INSERT INTO validation_runs (firm_id, invoice_id, payload_version, ruleset_version, trigger, error_count,
                             warning_count, rules_evaluated, duration_us, requested_by, trace_id)
VALUES (@firm_id, @invoice_id, @payload_version, @ruleset_version, @trigger, @error_count,
        @warning_count, @rules_evaluated, @duration_us, @requested_by, @trace_id)
RETURNING id, created_at;

-- name: TrackCInsertIssues :exec
-- All issues of one run in one statement. Plan finding F1: COPY FROM is not supported on a table with
-- row-level security, so this is INSERT ... SELECT unnest(...), never :copyfrom. The arrays are parallel
-- (element n of each is issue n; set-returning functions in one select list advance in lockstep, and a
-- shorter array yields NULLs, which the NOT NULL columns reject). message_args elements are JSON object
-- texts.
INSERT INTO validation_issues (id, firm_id, run_id, invoice_id, seq, rule_id, severity, path, business_term,
                               message, message_ar, message_args, fixable, suggested_value)
SELECT gen_random_uuid(), @firm_id::uuid, @run_id::uuid, @invoice_id::uuid,
       unnest(@seqs::integer[]), unnest(@rule_ids::text[]), unnest(@severities::text[]),
       unnest(@paths::text[]), unnest(@business_terms::text[]), unnest(@messages::text[]),
       unnest(@messages_ar::text[]), unnest(@message_args::text[])::jsonb, unnest(@fixables::boolean[]),
       unnest(@suggested_values::text[]);

-- name: TrackCGetRun :one
SELECT id, invoice_id, payload_version, ruleset_version, trigger, error_count, warning_count, rules_evaluated,
       duration_us, requested_by, trace_id, created_at
FROM validation_runs
WHERE id = @id AND invoice_id = @invoice_id;

-- name: TrackCListRuns :many
-- An invoice's runs, newest first, keyset cursor (before_created_at, before_id). With a run's
-- (created_at, id) as the cursor and page_limit 1 this is the run before it (the diff default).
SELECT id, invoice_id, payload_version, ruleset_version, trigger, error_count, warning_count, rules_evaluated,
       duration_us, requested_by, trace_id, created_at
FROM validation_runs
WHERE invoice_id = @invoice_id
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, @before_id::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @page_limit;

-- name: TrackCListIssues :many
-- A run's issues in issue order (seq).
SELECT id, seq, rule_id, severity, path, business_term, message, message_ar, message_args, fixable,
       suggested_value
FROM validation_issues
WHERE run_id = @run_id
ORDER BY seq;
