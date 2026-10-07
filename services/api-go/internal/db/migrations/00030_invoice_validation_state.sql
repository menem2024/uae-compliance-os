-- +goose Up
-- Track C (spec §5.5). Shared by every append-only table (00031-00033): rejects UPDATE, DELETE and
-- TRUNCATE for every role. compliance_app already fails on the missing privilege; the owner, who
-- holds every privilege, fails here. Both get SQLSTATE 42501.
-- +goose StatementBegin
CREATE FUNCTION trackc_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION '% on % is not allowed: the table is append-only', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'insufficient_privilege';
END $$;
-- +goose StatementEnd

ALTER TABLE invoices
  ADD COLUMN payload_version          integer NOT NULL DEFAULT 1
    CONSTRAINT invoices_payload_version_positive CHECK (payload_version >= 1),
  ADD COLUMN latest_run_id            uuid,
  ADD COLUMN approved_payload_version integer,
  ADD COLUMN approved_by              text,
  ADD COLUMN approved_at              timestamptz;

ALTER TABLE invoices
  -- Target of the composite (invoice_id, firm_id) foreign keys of every Track C table.
  ADD CONSTRAINT invoices_id_firm_uniq UNIQUE (id, firm_id),
  ADD CONSTRAINT invoices_approval_consistent CHECK (
    (approved_payload_version IS NULL) = (approved_by IS NULL)
    AND (approved_by IS NULL) = (approved_at IS NULL)),
  -- Plan finding F2: the IS NOT NULL term is required. Without it the expression is NULL for
  -- status = 'ready' with no approval, and a CHECK whose expression is NULL passes.
  ADD CONSTRAINT invoices_ready_needs_approval CHECK (
    status <> 'ready'
    OR (approved_payload_version IS NOT NULL AND approved_payload_version = payload_version));

CREATE INDEX invoices_firm_status_created_idx ON invoices (firm_id, status, created_at DESC, id DESC);

-- +goose Down
-- Without the approval columns no invoice is approved, so 'ready' becomes 'validated' again (otherwise
-- a later Up could not re-add invoices_ready_needs_approval). Under FORCE ROW LEVEL SECURITY the owner
-- sees no rows, so FORCE is lifted for this one statement, inside the migration's transaction.
ALTER TABLE invoices NO FORCE ROW LEVEL SECURITY;
UPDATE invoices SET status = 'validated' WHERE status = 'ready';
ALTER TABLE invoices FORCE ROW LEVEL SECURITY;
DROP INDEX invoices_firm_status_created_idx;
ALTER TABLE invoices
  DROP CONSTRAINT invoices_ready_needs_approval,
  DROP CONSTRAINT invoices_approval_consistent,
  DROP CONSTRAINT invoices_id_firm_uniq,
  DROP COLUMN approved_at,
  DROP COLUMN approved_by,
  DROP COLUMN approved_payload_version,
  DROP COLUMN latest_run_id,
  DROP COLUMN payload_version;
DROP FUNCTION trackc_forbid_mutation();
