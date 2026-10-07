-- +goose Up
-- Track C (spec §5.5, §5.6.8). One Fix agent request per (validation run, mode).
CREATE TABLE fix_tasks (
  id             uuid PRIMARY KEY,
  firm_id        uuid NOT NULL REFERENCES firms(id),
  invoice_id     uuid NOT NULL,
  run_id         uuid NOT NULL,
  mode           text NOT NULL CHECK (mode IN ('auto','on_demand')),
  status         text NOT NULL DEFAULT 'requested'
                   CHECK (status IN ('requested','succeeded','failed','budget_exceeded','cancelled')),
  outcome        text NOT NULL DEFAULT '' CHECK (outcome IN ('','proposed','no_fix','not_improving')),
  proposal_id    uuid,
  agent_run_id   uuid,
  error_code     text NOT NULL DEFAULT '',
  requested_by   text NOT NULL DEFAULT '',
  requested_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
  published_at   timestamptz,
  completed_at   timestamptz,
  CONSTRAINT fix_tasks_invoice_fk FOREIGN KEY (invoice_id, firm_id) REFERENCES invoices(id, firm_id),
  CONSTRAINT fix_tasks_run_fk
    FOREIGN KEY (run_id, invoice_id, firm_id) REFERENCES validation_runs(id, invoice_id, firm_id),
  CONSTRAINT fix_tasks_run_mode_uniq UNIQUE (run_id, mode),
  CONSTRAINT fix_tasks_completed_consistent CHECK ((status = 'requested') = (completed_at IS NULL))
);
CREATE INDEX fix_tasks_firm_invoice_requested_idx ON fix_tasks (firm_id, invoice_id, requested_at DESC);

ALTER TABLE fix_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE fix_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON fix_tasks
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

-- A requested task may be (re)published, and it moves to a terminal status exactly once, writing its
-- result fields in that same update. A terminal task never changes again. Identity columns never change.
-- +goose StatementBegin
CREATE FUNCTION trackc_fix_tasks_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status <> 'requested' THEN
    RAISE EXCEPTION 'fix task % is %, it cannot change', OLD.id, OLD.status
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.id <> OLD.id OR NEW.firm_id <> OLD.firm_id OR NEW.invoice_id <> OLD.invoice_id
     OR NEW.run_id <> OLD.run_id OR NEW.mode <> OLD.mode OR NEW.requested_by <> OLD.requested_by
     OR NEW.requested_at <> OLD.requested_at THEN
    RAISE EXCEPTION 'fix task % identity is immutable', OLD.id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.status = 'requested' AND (NEW.outcome <> OLD.outcome
     OR NEW.proposal_id IS DISTINCT FROM OLD.proposal_id OR NEW.agent_run_id IS DISTINCT FROM OLD.agent_run_id
     OR NEW.error_code <> OLD.error_code) THEN
    RAISE EXCEPTION 'fix task % result fields change only with its terminal status', OLD.id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER fix_tasks_guard BEFORE UPDATE ON fix_tasks
  FOR EACH ROW EXECUTE FUNCTION trackc_fix_tasks_guard();

REVOKE ALL ON fix_tasks FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON fix_tasks TO compliance_app;   -- never DELETE

-- +goose Down
DROP TABLE fix_tasks;
DROP FUNCTION trackc_fix_tasks_guard();
