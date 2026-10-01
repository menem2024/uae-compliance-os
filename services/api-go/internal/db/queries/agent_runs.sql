-- name: UpsertRunStarted :exec
-- Never changes status: a run.finished processed earlier keeps its final status.
INSERT INTO agent_runs (id, firm_id, client_company_id, workflow, subject_type, subject_id, status,
                        delivery_attempt, trace_id, plan, budget, started_at)
VALUES (@id, @firm_id, sqlc.narg('client_company_id'), @workflow, @subject_type, @subject_id, 'running',
        @delivery_attempt, @trace_id, @plan, @budget, @started_at)
ON CONFLICT (id) DO UPDATE
  SET client_company_id = EXCLUDED.client_company_id, workflow = EXCLUDED.workflow,
      delivery_attempt = EXCLUDED.delivery_attempt, trace_id = EXCLUDED.trace_id, plan = EXCLUDED.plan,
      budget = EXCLUDED.budget, started_at = EXCLUDED.started_at, updated_at = now();

-- name: AbandonSupersededRuns :many
-- A newer run for the same subject exists: the older running ones were redelivered away (ack_wait
-- expired, worker died). Order-independent, so it runs after every run.started.
UPDATE agent_runs r SET status = 'abandoned', error_code = 'superseded', updated_at = now()
WHERE r.subject_type = @subject_type AND r.subject_id = @subject_id AND r.status = 'running'
  AND EXISTS (SELECT 1 FROM agent_runs n
              WHERE n.subject_type = r.subject_type AND n.subject_id = r.subject_id
                AND n.started_at > r.started_at)
RETURNING r.id;

-- name: UpsertRunFinished :exec
INSERT INTO agent_runs (id, firm_id, subject_type, subject_id, status, error_code, steps, llm_calls,
                        response_cache_hits, input_tokens, output_tokens, cost_micro_usd, finished_at)
VALUES (@id, @firm_id, @subject_type, @subject_id, @status, @error_code, @steps, @llm_calls,
        @response_cache_hits, @input_tokens, @output_tokens, @cost_micro_usd, @finished_at)
ON CONFLICT (id) DO UPDATE
  SET status = EXCLUDED.status, error_code = EXCLUDED.error_code, steps = EXCLUDED.steps,
      llm_calls = EXCLUDED.llm_calls, response_cache_hits = EXCLUDED.response_cache_hits,
      input_tokens = EXCLUDED.input_tokens, output_tokens = EXCLUDED.output_tokens,
      cost_micro_usd = EXCLUDED.cost_micro_usd, finished_at = EXCLUDED.finished_at, updated_at = now();

-- name: UpsertStep :exec
-- For one step_id the event with the highest seq wins (contract 4.1).
INSERT INTO agent_steps (id, firm_id, run_id, seq, node_id, depends_on, agent, action, kind, status, attempt,
                         at, duration_ms, model, prompt_id, prompt_version, input_tokens, output_tokens,
                         cache_read_input_tokens, cache_creation_input_tokens, cost_micro_usd,
                         response_cache_hit, llm_calls, message_key, message_args, error_code, subject_type,
                         subject_id, client_company_id)
VALUES (@id, @firm_id, @run_id, @seq, @node_id, @depends_on, @agent, @action, @kind, @status, @attempt,
        @at, @duration_ms, @model, @prompt_id, @prompt_version, @input_tokens, @output_tokens,
        @cache_read_input_tokens, @cache_creation_input_tokens, @cost_micro_usd,
        @response_cache_hit, @llm_calls, @message_key, @message_args, @error_code, @subject_type,
        @subject_id, sqlc.narg('client_company_id'))
ON CONFLICT (id) DO UPDATE
  SET seq = EXCLUDED.seq, status = EXCLUDED.status, at = EXCLUDED.at, duration_ms = EXCLUDED.duration_ms,
      depends_on = EXCLUDED.depends_on, model = EXCLUDED.model, prompt_id = EXCLUDED.prompt_id,
      prompt_version = EXCLUDED.prompt_version, input_tokens = EXCLUDED.input_tokens,
      output_tokens = EXCLUDED.output_tokens, cache_read_input_tokens = EXCLUDED.cache_read_input_tokens,
      cache_creation_input_tokens = EXCLUDED.cache_creation_input_tokens,
      cost_micro_usd = EXCLUDED.cost_micro_usd, response_cache_hit = EXCLUDED.response_cache_hit,
      llm_calls = EXCLUDED.llm_calls, message_key = EXCLUDED.message_key,
      message_args = EXCLUDED.message_args, error_code = EXCLUDED.error_code, updated_at = now()
  WHERE agent_steps.seq < EXCLUDED.seq;

-- name: GetRun :one
SELECT * FROM agent_runs WHERE id = @id;

-- name: ListRuns :many
SELECT * FROM agent_runs
WHERE (sqlc.narg('subject_type')::text IS NULL OR subject_type = sqlc.narg('subject_type')::text)
  AND (sqlc.narg('subject_id')::text IS NULL OR subject_id = sqlc.narg('subject_id')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @page_limit;

-- name: ListRunSteps :many
SELECT * FROM agent_steps WHERE run_id = @run_id ORDER BY seq;

-- name: RecentSteps :many
-- SSE backfill: steps changed after @since (Last-Event-ID) or the latest ones.
SELECT * FROM agent_steps WHERE updated_at > @since ORDER BY updated_at DESC LIMIT @page_limit;

-- name: RecentRuns :many
SELECT * FROM agent_runs WHERE updated_at > @since ORDER BY updated_at DESC LIMIT @page_limit;

-- name: RunSummary :one
SELECT count(*)::bigint AS runs,
       count(*) FILTER (WHERE status = 'running')::bigint AS running,
       count(DISTINCT subject_id) FILTER (WHERE subject_type = 'document')::bigint AS documents,
       coalesce(sum(llm_calls), 0)::bigint AS llm_calls,
       coalesce(sum(response_cache_hits), 0)::bigint AS response_cache_hits,
       coalesce(sum(cost_micro_usd), 0)::bigint AS cost_micro_usd
FROM agent_runs
WHERE created_at >= sqlc.arg('day_start')::timestamptz AND created_at < sqlc.arg('day_end')::timestamptz;

-- name: AgentSummary :many
SELECT agent, model, coalesce(sum(llm_calls), 0)::bigint AS calls,
       coalesce(avg(duration_ms), 0)::bigint AS avg_ms
FROM agent_steps
WHERE at >= sqlc.arg('day_start')::timestamptz AND at < sqlc.arg('day_end')::timestamptz
  AND status IN ('succeeded', 'failed')
GROUP BY agent, model
ORDER BY agent, model;
