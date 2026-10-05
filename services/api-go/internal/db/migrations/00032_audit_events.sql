-- +goose Up
-- Track C (spec §5.5). Who changed, approved or exported what. Validation runs are their own log
-- (validation_runs) and are not audit events.
CREATE TABLE audit_events (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  firm_id      uuid NOT NULL REFERENCES firms(id),
  occurred_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
  actor_type   text NOT NULL CHECK (actor_type IN ('user','agent','system')),
  actor_id     text NOT NULL CHECK (actor_id <> ''),
  agent        text NOT NULL DEFAULT '' CHECK (agent = '' OR agent ~ '^[a-z][a-z0-9_]{1,40}$'),
  proposal_id  uuid,              -- soft reference: proposals is Track B's table
  action       text NOT NULL CHECK (action ~ '^[a-z][a-z_]*\.[a-z][a-z_]*$'),
  entity_type  text NOT NULL CHECK (entity_type IN ('invoice','proposal','export')),
  entity_id    uuid NOT NULL,
  invoice_id   uuid,
  changes      jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(changes) = 'array'),
  before       jsonb,
  after        jsonb,
  reason       text NOT NULL DEFAULT '',
  trace_id     text NOT NULL DEFAULT '',
  CONSTRAINT audit_events_invoice_fk FOREIGN KEY (invoice_id, firm_id) REFERENCES invoices(id, firm_id)
);
CREATE INDEX audit_events_firm_invoice_occurred_idx
  ON audit_events (firm_id, invoice_id, occurred_at DESC, id DESC) WHERE invoice_id IS NOT NULL;
CREATE INDEX audit_events_firm_occurred_idx ON audit_events (firm_id, occurred_at DESC, id DESC);

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON audit_events
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

-- Append-only (E3: the DB role cannot UPDATE or DELETE audit_events; nor can the owner).
CREATE TRIGGER audit_events_no_update_delete BEFORE UPDATE OR DELETE ON audit_events
  FOR EACH ROW EXECUTE FUNCTION trackc_forbid_mutation();
CREATE TRIGGER audit_events_no_truncate BEFORE TRUNCATE ON audit_events
  FOR EACH STATEMENT EXECUTE FUNCTION trackc_forbid_mutation();

REVOKE ALL ON audit_events FROM PUBLIC;
GRANT SELECT, INSERT ON audit_events TO compliance_app;

-- +goose Down
DROP TABLE audit_events;
