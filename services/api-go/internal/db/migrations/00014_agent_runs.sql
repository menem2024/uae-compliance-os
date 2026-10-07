-- +goose Up
-- Written only by the api-agent-events consumer. Events can arrive out of order (parallel handlers,
-- redelivery), so there is no FK from steps to runs and every upsert is order-independent.
CREATE TABLE agent_runs (
  id                   uuid PRIMARY KEY,
  firm_id              uuid NOT NULL REFERENCES firms(id),
  client_company_id    uuid,
  workflow             text NOT NULL DEFAULT '',
  subject_type         text NOT NULL,
  subject_id           text NOT NULL,
  status               text NOT NULL CHECK (status IN (
                         'running','succeeded','failed','budget_exceeded','cancelled','abandoned')),
  error_code           text NOT NULL DEFAULT '',
  delivery_attempt     integer NOT NULL DEFAULT 1,
  trace_id             text NOT NULL DEFAULT '',
  plan                 jsonb NOT NULL DEFAULT '[]'::jsonb,
  budget               jsonb NOT NULL DEFAULT '{}'::jsonb,
  steps                integer NOT NULL DEFAULT 0,
  llm_calls            integer NOT NULL DEFAULT 0,
  response_cache_hits  integer NOT NULL DEFAULT 0,
  input_tokens         bigint NOT NULL DEFAULT 0,
  output_tokens        bigint NOT NULL DEFAULT 0,
  cost_micro_usd       bigint NOT NULL DEFAULT 0 CHECK (cost_micro_usd >= 0),
  started_at           timestamptz,
  finished_at          timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_runs_firm_created_idx ON agent_runs(firm_id, created_at DESC, id DESC);
CREATE INDEX agent_runs_subject_idx ON agent_runs(firm_id, subject_type, subject_id, started_at DESC);
CREATE INDEX agent_runs_running_idx ON agent_runs(firm_id, subject_type, subject_id) WHERE status = 'running';
CREATE INDEX agent_runs_firm_updated_idx ON agent_runs(firm_id, updated_at DESC);

CREATE TABLE agent_steps (
  id                          uuid PRIMARY KEY,           -- step_id: uuid5(run_id, "<node_id>#<attempt>")
  firm_id                     uuid NOT NULL REFERENCES firms(id),
  run_id                      uuid NOT NULL,              -- soft reference to agent_runs(id)
  seq                         bigint NOT NULL CHECK (seq >= 1),
  node_id                     text NOT NULL,
  depends_on                  text[] NOT NULL DEFAULT '{}',
  agent                       text NOT NULL,
  action                      text NOT NULL,
  kind                        text NOT NULL CHECK (kind IN ('deterministic','llm','tool','router')),
  status                      text NOT NULL CHECK (status IN ('started','succeeded','failed','retrying','skipped')),
  attempt                     integer NOT NULL CHECK (attempt >= 0),
  at                          timestamptz NOT NULL,
  duration_ms                 bigint NOT NULL DEFAULT 0,
  model                       text NOT NULL DEFAULT '',
  prompt_id                   text NOT NULL DEFAULT '',
  prompt_version              integer NOT NULL DEFAULT 0,
  input_tokens                bigint NOT NULL DEFAULT 0,
  output_tokens               bigint NOT NULL DEFAULT 0,
  cache_read_input_tokens     bigint NOT NULL DEFAULT 0,
  cache_creation_input_tokens bigint NOT NULL DEFAULT 0,
  cost_micro_usd              bigint NOT NULL DEFAULT 0 CHECK (cost_micro_usd >= 0),
  response_cache_hit          boolean NOT NULL DEFAULT false,
  llm_calls                   integer NOT NULL DEFAULT 0,
  message_key                 text NOT NULL DEFAULT '',
  message_args                jsonb NOT NULL DEFAULT '{}'::jsonb,
  error_code                  text NOT NULL DEFAULT '',
  subject_type                text NOT NULL DEFAULT '',
  subject_id                  text NOT NULL DEFAULT '',
  client_company_id           uuid,
  created_at                  timestamptz NOT NULL DEFAULT now(),
  updated_at                  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_steps_run_seq_idx ON agent_steps(run_id, seq);
CREATE INDEX agent_steps_firm_updated_idx ON agent_steps(firm_id, updated_at DESC);
CREATE INDEX agent_steps_firm_at_idx ON agent_steps(firm_id, at DESC);

ALTER TABLE agent_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON agent_runs
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);
ALTER TABLE agent_steps ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_steps FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON agent_steps
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON agent_runs TO compliance_app;
GRANT SELECT, INSERT, UPDATE ON agent_steps TO compliance_app;

-- +goose Down
DROP TABLE agent_steps;
DROP TABLE agent_runs;
