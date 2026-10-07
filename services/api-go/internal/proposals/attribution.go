package proposals

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// KindDocumentAttribution moves a Document (and its Invoices) to another ClientCompany of the same Firm.
// ai-py's intake.attribute node raises it when the extracted TRNs match another candidate and not the
// chosen ClientCompany. Changes: "client_company_id" (required) and "direction" (optional; the direction
// relative to the new ClientCompany).
const KindDocumentAttribution = "document.attribution"

var directions = map[string]bool{"issued": true, "received": true, "unknown": true}

// AttributionApplier implements Applier for KindDocumentAttribution.
type AttributionApplier struct{}

// Apply re-attributes the Document and its Invoices. It refuses (ErrStale) when the Document moved since
// the proposal, the new ClientCompany is gone or archived, or it already holds the same bytes.
func (AttributionApplier) Apply(ctx context.Context, q *sqlc.Queries, p Proposal) error {
	change, ok := p.Change("client_company_id")
	if p.TargetType != "document" || !ok {
		return ErrBadProposal
	}
	oldID, errOld := uuid.Parse(change.OldValue)
	newID, errNew := uuid.Parse(change.NewValue)
	if errOld != nil || errNew != nil || oldID == newID {
		return ErrBadProposal
	}
	cc, err := q.GetClientCompany(ctx, newID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return fmt.Errorf("get client company: %w", err)
	}
	if cc.Status != "active" {
		return ErrStale
	}
	doc, err := q.LockDocument(ctx, p.TargetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return fmt.Errorf("lock document: %w", err)
	}
	if doc.ClientCompanyID != oldID {
		return ErrStale
	}
	direction := "unknown"
	if d, ok := p.Change("direction"); ok && directions[d.NewValue] {
		direction = d.NewValue
	}
	if _, err := q.ReattributeDocument(ctx, sqlc.ReattributeDocumentParams{ID: doc.ID, ClientCompanyID: newID,
		Direction: direction}); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.ConstraintName == "documents_dedup_uniq" {
			return ErrStale // the new ClientCompany already has these exact bytes
		}
		return fmt.Errorf("reattribute document: %w", err)
	}
	if _, err := q.ReattributeDocumentInvoices(ctx, sqlc.ReattributeDocumentInvoicesParams{
		DocumentID:      uuid.NullUUID{UUID: doc.ID, Valid: true},
		ClientCompanyID: uuid.NullUUID{UUID: newID, Valid: true}}); err != nil {
		return fmt.Errorf("reattribute invoices: %w", err)
	}
	return nil
}
