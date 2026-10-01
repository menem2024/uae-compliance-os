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
