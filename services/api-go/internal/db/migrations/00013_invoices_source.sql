-- +goose Up
-- Phase 1 invoices come from a Document. Phase 0 rows keep NULLs; MATCH SIMPLE composite FKs
-- ignore rows with a NULL column, so they stay valid.
ALTER TABLE invoices
  ADD COLUMN client_company_id     uuid,
  ADD COLUMN document_id           uuid,
  ADD COLUMN source_ordinal        integer CHECK (source_ordinal >= 0),
  ADD COLUMN source_ref            text NOT NULL DEFAULT '',
  ADD COLUMN extraction_confidence numeric(4,3)
    CHECK (extraction_confidence >= 0 AND extraction_confidence <= 1),
  ADD COLUMN extraction_run_id     uuid;
ALTER TABLE invoices
  ADD CONSTRAINT invoices_document_fk
    FOREIGN KEY (document_id, firm_id) REFERENCES documents(id, firm_id),
  ADD CONSTRAINT invoices_client_company_fk
    FOREIGN KEY (client_company_id, firm_id) REFERENCES client_companies(id, firm_id),
  ADD CONSTRAINT invoices_document_ordinal_uniq UNIQUE (document_id, source_ordinal),
  ADD CONSTRAINT invoices_source_consistent CHECK ((document_id IS NULL) = (source_ordinal IS NULL));
CREATE INDEX invoices_firm_cc_idx ON invoices(firm_id, client_company_id) WHERE client_company_id IS NOT NULL;

-- +goose Down
DROP INDEX invoices_firm_cc_idx;
ALTER TABLE invoices
  DROP CONSTRAINT invoices_source_consistent,
  DROP CONSTRAINT invoices_document_ordinal_uniq,
  DROP CONSTRAINT invoices_client_company_fk,
  DROP CONSTRAINT invoices_document_fk,
  DROP COLUMN extraction_run_id,
  DROP COLUMN extraction_confidence,
  DROP COLUMN source_ref,
  DROP COLUMN source_ordinal,
  DROP COLUMN document_id,
  DROP COLUMN client_company_id;
