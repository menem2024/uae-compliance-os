-- +goose Up
-- firms is not a tenant table: every FirmUser resolves its Firm by Zitadel org id outside WithFirm
-- (db.FirmByOrg) and the owner role seeds Firms, so RLS is ENABLEd but not FORCEd. The app role may
-- read every Firm row (name and accent only) and update only its own.
ALTER TABLE firms ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE firms ENABLE ROW LEVEL SECURITY;
CREATE POLICY firms_read ON firms FOR SELECT USING (true);
CREATE POLICY firms_update_own ON firms FOR UPDATE
  USING (id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

GRANT UPDATE (name, brand_color, updated_at) ON firms TO compliance_app;

-- +goose Down
REVOKE UPDATE (name, brand_color, updated_at) ON firms FROM compliance_app;
DROP POLICY firms_update_own ON firms;
DROP POLICY firms_read ON firms;
ALTER TABLE firms DISABLE ROW LEVEL SECURITY;
ALTER TABLE firms DROP COLUMN updated_at;
