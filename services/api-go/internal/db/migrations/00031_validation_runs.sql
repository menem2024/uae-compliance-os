-- +goose Up
-- Track C (spec §5.5). One row per validator call; its issues are append-only children.
CREATE TABLE validation_runs (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  firm_id          uuid NOT NULL REFERENCES firms(id),
  invoice_id       uuid NOT NULL,
  payload_version  integer NOT NULL,
  ruleset_version  text NOT NULL,
  trigger          text NOT NULL CHECK (trigger IN
                     ('extracted','manual','fix_accepted','correction','sweeper','ruleset_upgrade')),
  error_count      integer NOT NULL CHECK (error_count >= 0),
  warning_count    integer NOT NULL CHECK (warning_count >= 0),
  rules_evaluated  integer NOT NULL CHECK (rules_evaluated >= 0),
  duration_us      bigint NOT NULL CHECK (duration_us >= 0),
  requested_by     text NOT NULL DEFAULT '',
  trace_id         text NOT NULL DEFAULT '',
  created_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT validation_runs_invoice_fk FOREIGN KEY (invoice_id, firm_id) REFERENCES invoices(id, firm_id),
  -- Target of the (run_id, invoice_id, firm_id) foreign keys: a child row's run belongs to the
  -- same invoice and Firm as the child.
  CONSTRAINT validation_runs_id_invoice_firm_uniq UNIQUE (id, invoice_id, firm_id)
);
CREATE INDEX validation_runs_firm_invoice_created_idx
  ON validation_runs (firm_id, invoice_id, created_at DESC, id DESC);

CREATE TABLE validation_issues (
  id               uuid PRIMARY KEY,
  firm_id          uuid NOT NULL REFERENCES firms(id),
  run_id           uuid NOT NULL,
  invoice_id       uuid NOT NULL,
  seq              integer NOT NULL,
  rule_id          text NOT NULL CHECK (rule_id ~ '^([a-z]+(-[a-z0-9]+)+|AE-[A-Z]+-[0-9]{3})$'),
  severity         text NOT NULL CHECK (severity IN ('error','warning')),
  path             text NOT NULL,
  business_term    text NOT NULL DEFAULT '',
  message          text NOT NULL,
  message_ar       text NOT NULL DEFAULT '',
  message_args     jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(message_args) = 'object'),
  fixable          boolean NOT NULL DEFAULT false,
  suggested_value  text NOT NULL DEFAULT '',
  CONSTRAINT validation_issues_run_fk
    FOREIGN KEY (run_id, invoice_id, firm_id) REFERENCES validation_runs(id, invoice_id, firm_id),
  CONSTRAINT validation_issues_run_seq_uniq UNIQUE (run_id, seq)
);
CREATE INDEX validation_issues_firm_rule_idx ON validation_issues (firm_id, rule_id);
CREATE INDEX validation_issues_firm_invoice_idx ON validation_issues (firm_id, invoice_id);

-- An invoice's latest run is one of its own runs (MATCH SIMPLE: a NULL latest_run_id is not checked).
ALTER TABLE invoices ADD CONSTRAINT invoices_latest_run_fk
  FOREIGN KEY (latest_run_id, id, firm_id) REFERENCES validation_runs(id, invoice_id, firm_id);

ALTER TABLE validation_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE validation_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON validation_runs
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

ALTER TABLE validation_issues ENABLE ROW LEVEL SECURITY;
ALTER TABLE validation_issues FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON validation_issues
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

-- Append-only: SELECT and INSERT for the app role, and the trigger for every role (the owner too).
CREATE TRIGGER validation_runs_no_update_delete BEFORE UPDATE OR DELETE ON validation_runs
  FOR EACH ROW EXECUTE FUNCTION trackc_forbid_mutation();
CREATE TRIGGER validation_runs_no_truncate BEFORE TRUNCATE ON validation_runs
  FOR EACH STATEMENT EXECUTE FUNCTION trackc_forbid_mutation();
CREATE TRIGGER validation_issues_no_update_delete BEFORE UPDATE OR DELETE ON validation_issues
  FOR EACH ROW EXECUTE FUNCTION trackc_forbid_mutation();
CREATE TRIGGER validation_issues_no_truncate BEFORE TRUNCATE ON validation_issues
  FOR EACH STATEMENT EXECUTE FUNCTION trackc_forbid_mutation();

REVOKE ALL ON validation_runs, validation_issues FROM PUBLIC;
GRANT SELECT, INSERT ON validation_runs, validation_issues TO compliance_app;

-- +goose Down
ALTER TABLE invoices DROP CONSTRAINT invoices_latest_run_fk;
DROP TABLE validation_issues;
DROP TABLE validation_runs;
