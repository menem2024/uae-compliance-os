-- +goose Up
CREATE TABLE proposals (
  id                uuid PRIMARY KEY,
  firm_id           uuid NOT NULL REFERENCES firms(id),
  client_company_id uuid,
  run_id            uuid,                         -- soft reference to agent_runs(id): events may arrive out of order
  agent             text NOT NULL CHECK (agent ~ '^[a-z][a-z0-9_]{1,40}$'),
  kind              text NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$'),
  target_type       text NOT NULL CHECK (target_type IN ('document','invoice','validation_issue','source','client_company')),
  target_id         uuid NOT NULL,
  summary_key       text NOT NULL,
  summary_args      jsonb NOT NULL DEFAULT '{}'::jsonb,
  rationale         text NOT NULL DEFAULT '',
  confidence        numeric(4,3) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
  changes           jsonb NOT NULL DEFAULT '[]'::jsonb,   -- [{"path","old_value","new_value"}]
  detail_type       text NOT NULL DEFAULT '',             -- Any.type_url
  detail            bytea,                                -- Any.value
  evidence          jsonb NOT NULL DEFAULT '[]'::jsonb,   -- [{"kind","ref","excerpt"}]
  state             text NOT NULL DEFAULT 'proposed'
                    CHECK (state IN ('proposed','accepted','rejected','superseded','expired')),
  decided_by        text,
  decided_at        timestamptz,
  decision_reason   text,
  applied_at        timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  expires_at        timestamptz,
  CONSTRAINT proposals_decided_consistent
    CHECK ((state IN ('accepted','rejected')) = (decided_at IS NOT NULL AND decided_by IS NOT NULL)),
  CONSTRAINT proposals_applied_only_accepted CHECK (applied_at IS NULL OR state = 'accepted'),
  CONSTRAINT proposals_client_company_fk
    FOREIGN KEY (client_company_id, firm_id) REFERENCES client_companies(id, firm_id)
);
CREATE UNIQUE INDEX proposals_one_open_per_target
  ON proposals(firm_id, target_type, target_id, kind) WHERE state = 'proposed';
CREATE INDEX proposals_firm_state_created_idx ON proposals(firm_id, state, created_at DESC);
CREATE INDEX proposals_run_idx ON proposals(run_id);

ALTER TABLE proposals ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON proposals
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

-- +goose StatementBegin
CREATE FUNCTION proposals_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state <> 'proposed' AND NEW.state <> OLD.state THEN
    RAISE EXCEPTION 'proposal % is %, cannot become %', OLD.id, OLD.state, NEW.state
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.id <> OLD.id OR NEW.firm_id <> OLD.firm_id OR NEW.kind <> OLD.kind
     OR NEW.target_type <> OLD.target_type OR NEW.target_id <> OLD.target_id
     OR NEW.changes <> OLD.changes OR NEW.detail IS DISTINCT FROM OLD.detail
     OR NEW.agent <> OLD.agent OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'proposal % content is immutable', OLD.id USING ERRCODE = 'check_violation';
  END IF;
  IF OLD.applied_at IS NOT NULL AND NEW.applied_at IS DISTINCT FROM OLD.applied_at THEN
    RAISE EXCEPTION 'proposal % was already applied', OLD.id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER proposals_guard BEFORE UPDATE ON proposals
  FOR EACH ROW EXECUTE FUNCTION proposals_guard();

GRANT SELECT, INSERT, UPDATE ON proposals TO compliance_app;   -- never DELETE

-- +goose Down
DROP TABLE proposals;
DROP FUNCTION proposals_guard();
