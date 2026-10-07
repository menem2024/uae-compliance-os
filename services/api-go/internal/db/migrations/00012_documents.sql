-- +goose Up
CREATE TABLE documents (
  id                 uuid PRIMARY KEY,
  firm_id            uuid NOT NULL REFERENCES firms(id),
  client_company_id  uuid NOT NULL,
  sha256             text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  object_key         text NOT NULL,
  filename           text NOT NULL CHECK (length(filename) BETWEEN 1 AND 255),
  content_type       text NOT NULL CHECK (content_type IN (
                       'application/pdf','image/png','image/jpeg','image/webp','text/csv',
                       'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')),
  size_bytes         bigint NOT NULL CHECK (size_bytes BETWEEN 1 AND 20971520),
  status             text NOT NULL DEFAULT 'pending_upload' CHECK (status IN (
                       'pending_upload','uploaded','processing','extracted','needs_review','not_invoice',
                       'failed','rejected')),
  status_reason      text NOT NULL DEFAULT '',
  kind               text NOT NULL DEFAULT '' CHECK (kind IN ('','invoice','credit_note','contract','other')),
  direction          text NOT NULL DEFAULT '' CHECK (direction IN ('','issued','received','unknown')),
  language           text NOT NULL DEFAULT '' CHECK (language IN ('','ar','en','mixed')),
  extraction_method  text NOT NULL DEFAULT '' CHECK (extraction_method IN ('','llm','xlsx','csv')),
  review_reasons     text[] NOT NULL DEFAULT '{}',
  invoice_count      integer NOT NULL DEFAULT 0 CHECK (invoice_count >= 0),
  publish_attempts   integer NOT NULL DEFAULT 0 CHECK (publish_attempts >= 0),
  published_at       timestamptz,
  reprocess_nonce    text NOT NULL DEFAULT gen_random_uuid()::text,
  latest_run_id      uuid,
  uploaded_by        text NOT NULL DEFAULT '',
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT documents_id_firm_uniq UNIQUE (id, firm_id),
  CONSTRAINT documents_dedup_uniq UNIQUE (firm_id, client_company_id, sha256),
  CONSTRAINT documents_client_company_fk
    FOREIGN KEY (client_company_id, firm_id) REFERENCES client_companies(id, firm_id),
  -- The key never contains user input, and one Document can never overwrite another's object.
  CONSTRAINT documents_object_key_shape
    CHECK (object_key = 'firms/' || firm_id::text || '/docs/' || id::text)
);
CREATE INDEX documents_firm_created_idx ON documents(firm_id, created_at DESC, id DESC);
CREATE INDEX documents_firm_cc_created_idx ON documents(firm_id, client_company_id, created_at DESC, id DESC);
CREATE INDEX documents_reconcile_idx ON documents(firm_id, status, updated_at)
  WHERE status IN ('uploaded','processing');

ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents FORCE ROW LEVEL SECURITY;
CREATE POLICY firm_isolation ON documents
  USING (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid)
  WITH CHECK (firm_id = NULLIF(current_setting('app.firm_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON documents TO compliance_app;

-- +goose Down
DROP TABLE documents;
