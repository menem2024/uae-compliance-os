-- +goose Up
-- tin is the Corporate Tax TIN (PINT-AE IBT-032, and the 0235 electronic address IBT-034), the first 10
-- digits of the company's own CT TRN. It never derives from trn (IBT-031 may be a VAT-group TRN).
CREATE TABLE client_companies (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  firm_id     uuid NOT NULL REFERENCES firms(id),
  name        text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
  name_ar     text NOT NULL DEFAULT '' CHECK (length(name_ar) <= 200),
  trn         text CHECK (trn ~ '^[0-9]{15}$'),
  tin         text CHECK (tin ~ '^1[0-9]{9}$'),
  emirate     text CHECK (emirate IN ('AUH','DXB','SHJ','UAQ','FUJ','AJM','RAK')),
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT client_companies_id_firm_uniq UNIQUE (id, firm_id),
  CONSTRAINT client_companies_firm_trn_uniq UNIQUE (firm_id, trn)
);
CREATE INDEX client_companies_firm_status_name_idx ON client_companies(firm_id, status, name, id);

ALTER TABLE client_companies ENABLE ROW LEVEL SECURITY;
ALTER TABLE client_companies FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON client_companies
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON client_companies TO compliance_app;

-- +goose Down
DROP TABLE client_companies;
