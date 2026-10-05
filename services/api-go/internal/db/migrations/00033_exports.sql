-- +goose Up
-- Track C (spec §5.5). One row per exported PINT-AE XML document; the bytes live in the object store.
CREATE TABLE exports (
  id               uuid PRIMARY KEY,
  firm_id          uuid NOT NULL REFERENCES firms(id),
  invoice_id       uuid NOT NULL,
  run_id           uuid NOT NULL,
  payload_version  integer NOT NULL,
  ruleset_version  text NOT NULL,
  format           text NOT NULL,
  document_kind    text NOT NULL CHECK (document_kind IN ('invoice','credit_note')),
  object_key       text NOT NULL,
  sha256           text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes       integer NOT NULL CHECK (size_bytes > 0),
  created_by       text NOT NULL,
  created_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
  -- The key never contains user input, and one export can never overwrite another's object.
  CONSTRAINT exports_object_key_shape
    CHECK (object_key = 'firms/' || firm_id::text || '/exports/' || id::text || '.xml'),
  CONSTRAINT exports_invoice_fk FOREIGN KEY (invoice_id, firm_id) REFERENCES invoices(id, firm_id),
  CONSTRAINT exports_run_fk
    FOREIGN KEY (run_id, invoice_id, firm_id) REFERENCES validation_runs(id, invoice_id, firm_id)
);
CREATE INDEX exports_firm_invoice_created_idx ON exports (firm_id, invoice_id, created_at DESC);

ALTER TABLE exports ENABLE ROW LEVEL SECURITY;
ALTER TABLE exports FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON exports
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

CREATE TRIGGER exports_no_update_delete BEFORE UPDATE OR DELETE ON exports
  FOR EACH ROW EXECUTE FUNCTION trackc_forbid_mutation();
CREATE TRIGGER exports_no_truncate BEFORE TRUNCATE ON exports
  FOR EACH STATEMENT EXECUTE FUNCTION trackc_forbid_mutation();

REVOKE ALL ON exports FROM PUBLIC;
GRANT SELECT, INSERT ON exports TO compliance_app;

-- +goose Down
DROP TABLE exports;
