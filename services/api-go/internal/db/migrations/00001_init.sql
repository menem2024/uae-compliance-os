-- +goose Up
CREATE TABLE firms (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  zitadel_org_id  text NOT NULL UNIQUE,
  name            text NOT NULL,
  brand_color     text CHECK (brand_color ~ '^#[0-9A-Fa-f]{6}$'),
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE invoices (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  firm_id          uuid NOT NULL REFERENCES firms(id),
  status           text NOT NULL CHECK (status IN ('uploaded','classified','extracted','needs_review','validated','has_issues','fixed','ready')),
  payload          jsonb NOT NULL,
  ruleset_version  text,
  issues           jsonb,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invoices_firm_id_idx ON invoices(firm_id);

ALTER TABLE invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE invoices FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON invoices
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

GRANT SELECT ON firms TO compliance_app;
GRANT SELECT, INSERT, UPDATE ON invoices TO compliance_app;

-- +goose Down
DROP TABLE invoices;
DROP TABLE firms;
