package documents

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// Pending is the outcome of preparing one upload: a (reset) pending_upload row, or the existing
// Document with the same (client company, sha256) when Deduplicated.
type Pending struct {
	Doc          sqlc.Document
	Deduplicated bool
}

// Store is the persistence the Service needs. Every method runs inside db.WithFirm; missing or
// hidden rows are db.ErrNotFound.
type Store interface {
	// PreparePending checks the ClientCompany (ErrClientNotFound, ErrClientArchived) and upserts one
	// pending_upload row per file, in one transaction.
	PreparePending(ctx context.Context, firmID, clientCompanyID uuid.UUID, uploadedBy string, files []FileIn) ([]Pending, error)
	Get(ctx context.Context, firmID, id uuid.UUID) (sqlc.Document, error)
	// FinishUpload moves a pending_upload row to uploaded (reason == "") or rejected. moved is false when
	// the row was not pending_upload any more (a concurrent or repeated complete); doc is then its
	// current state.
	FinishUpload(ctx context.Context, firmID, id uuid.UUID, reason string) (doc sqlc.Document, moved bool, err error)
	// Candidates are the Firm's active ClientCompanies (at most 500) for document.uploaded.
	Candidates(ctx context.Context, firmID uuid.UUID) ([]*compliancev1.ClientCompanyRef, error)
	// MarkPublished records the JetStream ack of document.uploaded for this nonce.
	MarkPublished(ctx context.Context, firmID, id uuid.UUID, nonce string) error
	List(ctx context.Context, firmID uuid.UUID, f ListFilter) ([]sqlc.Document, error)
	Invoices(ctx context.Context, firmID, id uuid.UUID) ([]InvoiceRef, error)

	// ApplyExtracted applies one document.extracted result. applied is false when the Document's
	// status was no longer uploaded/processing (a redelivery, or a superseded run already applied a
	// different result): the caller then does nothing else and only acks.
	ApplyExtracted(ctx context.Context, firm uuid.UUID, p ExtractedParams) (applied bool, err error)
	// ApplyFailed applies one document.failed result; applied has the same meaning as ApplyExtracted's.
	ApplyFailed(ctx context.Context, firm, id, runID uuid.UUID, reason string) (applied bool, err error)
	// InsertInvoices inserts each item (idempotent on (document_id, source_ordinal): ON CONFLICT DO
	// NOTHING), then returns the Document's full, authoritative invoice set.
	InsertInvoices(ctx context.Context, firm, documentID uuid.UUID, items []InvoiceIn) ([]InvoiceOut, error)
	// Reprocess moves a failed/not_invoice/needs_review(invoice_count=0) Document back to uploaded with
	// a new nonce. ErrNotReprocessable when the Document's current status does not allow it.
	Reprocess(ctx context.Context, firm, id uuid.UUID) (sqlc.Document, error)
	// Unpublished lists uploaded Documents whose document.uploaded publish was never acknowledged, past
	// the 1-minute floor.
	Unpublished(ctx context.Context, firm uuid.UUID) ([]sqlc.Document, error)
	// BumpPublishAttempt increments publish_attempts and returns the new value. pgx.ErrNoRows means the
	// Document moved on since Unpublished listed it (another path already finished it): the caller skips
	// it without error.
	BumpPublishAttempt(ctx context.Context, firm, id uuid.UUID) (int32, error)
	// FailUnpublished moves a Document to failed/publish_exhausted.
	FailUnpublished(ctx context.Context, firm, id uuid.UUID) error
	// ResetStuck moves Documents stuck processing (no running run) back to uploaded with a new nonce.
	ResetStuck(ctx context.Context, firm uuid.UUID) ([]uuid.UUID, error)
}

// FirmLister lists every Firm's id, outside any tenant scope (the firms table is readable by every
// role; the reconciler runs one pass per Firm).
type FirmLister interface {
	FirmIDs(ctx context.Context) ([]uuid.UUID, error)
}

// PGStore is the Postgres Store.
type PGStore struct{ Pool *pgxpool.Pool }

var _ Store = PGStore{}

// PreparePending implements Store.
func (s PGStore) PreparePending(ctx context.Context, firmID, clientCompanyID uuid.UUID, uploadedBy string,
	files []FileIn) ([]Pending, error) {
	out := make([]Pending, 0, len(files))
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		cc, err := q.GetClientCompany(ctx, clientCompanyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrClientNotFound
		}
		if err != nil {
			return fmt.Errorf("get client company: %w", err)
		}
		if cc.Status != "active" {
			return ErrClientArchived
		}
		for _, f := range files {
			id := uuid.New()
			doc, err := q.UpsertPendingDocument(ctx, sqlc.UpsertPendingDocumentParams{
				ID: id, FirmID: firmID, ClientCompanyID: clientCompanyID, Sha256: f.SHA256,
				ObjectKey: ObjectKey(firmID, id), Filename: f.Filename, ContentType: f.ContentType,
				SizeBytes: f.SizeBytes, UploadedBy: uploadedBy})
			if errors.Is(err, pgx.ErrNoRows) { // exists and is uploaded or later: deduplicated (AC-E3a)
				doc, err = q.GetDocumentByHash(ctx, sqlc.GetDocumentByHashParams{ClientCompanyID: clientCompanyID,
					Sha256: f.SHA256})
				if err != nil {
					return fmt.Errorf("get deduplicated document: %w", err)
				}
				out = append(out, Pending{Doc: doc, Deduplicated: true})
				continue
			}
			if err != nil {
				return fmt.Errorf("upsert pending document: %w", err)
			}
			out = append(out, Pending{Doc: doc})
		}
		return nil
	})
	return out, err
}

// Get implements Store.
func (s PGStore) Get(ctx context.Context, firmID, id uuid.UUID) (sqlc.Document, error) {
	var d sqlc.Document
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		var err error
		d, err = q.GetDocument(ctx, id)
		return err
	})
	return d, err
}

// FinishUpload implements Store.
func (s PGStore) FinishUpload(ctx context.Context, firmID, id uuid.UUID, reason string) (sqlc.Document, bool, error) {
	var d sqlc.Document
	moved := true
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		var err error
		if reason == "" {
			d, err = q.MarkDocumentUploaded(ctx, id)
		} else {
			d, err = q.RejectDocument(ctx, sqlc.RejectDocumentParams{ID: id, Reason: reason})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			moved = false
			d, err = q.GetDocument(ctx, id)
		}
		return err
	})
	return d, moved, err
}

// Candidates implements Store.
func (s PGStore) Candidates(ctx context.Context, firmID uuid.UUID) ([]*compliancev1.ClientCompanyRef, error) {
	var out []*compliancev1.ClientCompanyRef
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		rows, err := q.ListActiveClientCompanyRefs(ctx)
		if err != nil {
			return fmt.Errorf("list candidates: %w", err)
		}
		for _, r := range rows {
			out = append(out, &compliancev1.ClientCompanyRef{ClientCompanyId: r.ID.String(), Name: r.Name,
				NameAr: r.NameAr, Trn: r.Trn.String})
		}
		return nil
	})
	return out, err
}

// MarkPublished implements Store.
func (s PGStore) MarkPublished(ctx context.Context, firmID, id uuid.UUID, nonce string) error {
	return db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		_, err := q.MarkDocumentPublished(ctx, sqlc.MarkDocumentPublishedParams{ID: id, ReprocessNonce: nonce})
		return err
	})
}

// List implements Store.
func (s PGStore) List(ctx context.Context, firmID uuid.UUID, f ListFilter) ([]sqlc.Document, error) {
	var out []sqlc.Document
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		p := sqlc.ListDocumentsParams{PageLimit: int32(f.Limit)} //nolint:gosec // limit <= 101
		if f.ClientCompanyID != uuid.Nil {
			p.ClientCompanyID = uuid.NullUUID{UUID: f.ClientCompanyID, Valid: true}
		}
		if f.Status != "" {
			p.Status = pgtype.Text{String: f.Status, Valid: true}
		}
		if f.BeforeID != uuid.Nil {
			p.BeforeCreatedAt = pgtype.Timestamptz{Time: f.BeforeCreatedAt, Valid: true}
			p.BeforeID = uuid.NullUUID{UUID: f.BeforeID, Valid: true}
		}
		var err error
		out, err = q.ListDocuments(ctx, p)
		return err
	})
	return out, err
}

// Invoices implements Store.
func (s PGStore) Invoices(ctx context.Context, firmID, id uuid.UUID) ([]InvoiceRef, error) {
	out := []InvoiceRef{}
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		rows, err := q.ListDocumentInvoices(ctx, uuid.NullUUID{UUID: id, Valid: true})
		if err != nil {
			return fmt.Errorf("list document invoices: %w", err)
		}
		for _, r := range rows {
			out = append(out, InvoiceRef{ID: r.ID, Status: r.Status, SourceOrdinal: r.SourceOrdinal.Int32,
				ExtractionConfidence: numericPtr(r.ExtractionConfidence)})
		}
		return nil
	})
	return out, err
}

func numericPtr(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	return &f.Float64
}

// NumericFromFloat converts a [0,1] confidence to numeric(4,3) (rounded to 3 places).
func NumericFromFloat(v float64) pgtype.Numeric {
	milli := int64(v*1000 + 0.5)
	if v < 0 {
		milli = 0
	}
	if milli > 1000 {
		milli = 1000
	}
	return pgtype.Numeric{Int: big.NewInt(milli), Exp: -3, Valid: true}
}

// nullUUID converts uuid.Nil to an invalid (SQL NULL) uuid.NullUUID.
func nullUUID(id uuid.UUID) uuid.NullUUID {
	if id == uuid.Nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: id, Valid: true}
}

// ApplyExtracted implements Store.
func (s PGStore) ApplyExtracted(ctx context.Context, firm uuid.UUID, p ExtractedParams) (bool, error) {
	var n int64
	err := db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
		var err error
		if p.ReviewReasons == nil {
			p.ReviewReasons = []string{} // review_reasons is NOT NULL; nil would be sent as NULL
		}
		n, err = q.ApplyDocumentExtracted(ctx, sqlc.ApplyDocumentExtractedParams{
			Status: p.Status, StatusReason: p.StatusReason, Kind: p.Kind, Direction: p.Direction,
			Language: p.Language, ExtractionMethod: p.ExtractionMethod, ReviewReasons: p.ReviewReasons,
			InvoiceCount: p.InvoiceCount, RunID: nullUUID(p.RunID), ID: p.ID})
		return err
	})
	return n == 1, err
}

// ApplyFailed implements Store.
func (s PGStore) ApplyFailed(ctx context.Context, firm, id, runID uuid.UUID, reason string) (bool, error) {
	var n int64
	err := db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
		var err error
		n, err = q.ApplyDocumentFailed(ctx, sqlc.ApplyDocumentFailedParams{Reason: reason, RunID: nullUUID(runID), ID: id})
		return err
	})
	return n == 1, err
}

// InsertInvoices implements Store.
func (s PGStore) InsertInvoices(ctx context.Context, firm, documentID uuid.UUID, items []InvoiceIn) ([]InvoiceOut, error) {
	out := []InvoiceOut{}
	err := db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
		for _, it := range items {
			if _, err := q.InsertExtractedInvoice(ctx, sqlc.InsertExtractedInvoiceParams{
				FirmID: firm, Status: it.Status, Payload: it.Payload,
				ClientCompanyID: nullUUID(it.ClientCompanyID), DocumentID: uuid.NullUUID{UUID: documentID, Valid: true},
				SourceOrdinal: pgtype.Int4{Int32: it.SourceOrdinal, Valid: true}, SourceRef: it.SourceRef,
				ExtractionConfidence: NumericFromFloat(it.Confidence)}); err != nil {
				return fmt.Errorf("insert extracted invoice: %w", err)
			}
		}
		rows, err := q.ListDocumentInvoicesForPublish(ctx, uuid.NullUUID{UUID: documentID, Valid: true})
		if err != nil {
			return fmt.Errorf("list document invoices for publish: %w", err)
		}
		for _, r := range rows {
			out = append(out, InvoiceOut{ID: r.ID, Status: r.Status, Payload: r.Payload, Confidence: numericPtr(r.ExtractionConfidence)})
		}
		return nil
	})
	return out, err
}

// Reprocess implements Store.
func (s PGStore) Reprocess(ctx context.Context, firm, id uuid.UUID) (sqlc.Document, error) {
	var d sqlc.Document
	err := db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
		var err error
		d, err = q.ReprocessDocument(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotReprocessable
		}
		return err
	})
	return d, err
}

// Unpublished implements Store.
func (s PGStore) Unpublished(ctx context.Context, firm uuid.UUID) ([]sqlc.Document, error) {
	var out []sqlc.Document
	err := db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
		var err error
		out, err = q.ListUnpublishedDocuments(ctx)
		return err
	})
	return out, err
}

// BumpPublishAttempt implements Store. It returns pgx.ErrNoRows (not db.ErrNotFound) when the
// Document moved on, so callers can share the same check against a fake Store in unit tests.
func (s PGStore) BumpPublishAttempt(ctx context.Context, firm, id uuid.UUID) (int32, error) {
	var attempts int32
	found := true
	err := db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
		var err error
		attempts, err = q.BumpDocumentPublishAttempt(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			found = false
			return nil
		}
		return err
	})
	if err == nil && !found {
		return 0, pgx.ErrNoRows
	}
	return attempts, err
}

// FailUnpublished implements Store.
func (s PGStore) FailUnpublished(ctx context.Context, firm, id uuid.UUID) error {
	return db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
		_, err := q.FailUnpublishedDocument(ctx, id)
		return err
	})
}

// ResetStuck implements Store.
func (s PGStore) ResetStuck(ctx context.Context, firm uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
		var err error
		out, err = q.ResetStuckDocuments(ctx)
		return err
	})
	return out, err
}

// FirmIDs implements FirmLister. firms carries only a read-everyone RLS policy, so this runs outside
// WithFirm (like db.FirmByOrg).
func (s PGStore) FirmIDs(ctx context.Context) ([]uuid.UUID, error) {
	return sqlc.New(s.Pool).ListFirmIDs(ctx)
}

var _ FirmLister = PGStore{}
