package httpapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// PGStore implements Store over Postgres. Tenant rows are read and written
// only through db.WithFirm so RLS applies.
type PGStore struct{ Pool *pgxpool.Pool }

var _ Store = PGStore{}
var _ ValidationStore = PGStore{}

// FirmIDForOrg resolves a firm id from a Zitadel org id.
func (s PGStore) FirmIDForOrg(ctx context.Context, orgID string) (uuid.UUID, error) {
	f, err := db.FirmByOrg(ctx, s.Pool, orgID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("firm by org: %w", err)
	}
	return f.ID, nil
}

// Firm returns the caller's firm for theming.
func (s PGStore) Firm(ctx context.Context, orgID string) (FirmView, error) {
	f, err := db.FirmByOrg(ctx, s.Pool, orgID)
	if err != nil {
		return FirmView{}, fmt.Errorf("firm by org: %w", err)
	}
	return FirmView{ID: f.ID, Name: f.Name, BrandColor: textPtr(f.BrandColor)}, nil
}

// Create inserts an invoice with status uploaded.
func (s PGStore) Create(ctx context.Context, firmID uuid.UUID, payload []byte) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		inv, err := q.CreateInvoice(ctx, sqlc.CreateInvoiceParams{FirmID: firmID, Payload: payload})
		if err != nil {
			return fmt.Errorf("create invoice: %w", err)
		}
		id = inv.ID
		return nil
	})
	return id, err
}

// Get returns the invoice if visible to firmID, else db.ErrNotFound.
func (s PGStore) Get(ctx context.Context, firmID, id uuid.UUID) (InvoiceView, error) {
	var inv sqlc.Invoice
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		var err error
		inv, err = q.GetInvoice(ctx, id)
		return err
	})
	if err != nil {
		return InvoiceView{}, fmt.Errorf("get invoice %s: %w", id, err)
	}
	v := InvoiceView{ID: inv.ID.String(), Status: inv.Status, RulesetVersion: textPtr(inv.RulesetVersion), Issues: []IssueView{}}
	if len(inv.Issues) > 0 {
		if err := json.Unmarshal(inv.Issues, &v.Issues); err != nil {
			return InvoiceView{}, fmt.Errorf("decode issues of %s: %w", id, err)
		}
		if v.Issues == nil { // jsonb 'null'
			v.Issues = []IssueView{}
		}
	}
	return v, nil
}

// Status returns the invoice's current status if visible to firmID, else
// db.ErrNotFound. Used to skip redelivered invoice.extracted events for an
// invoice already in a terminal status.
func (s PGStore) Status(ctx context.Context, firmID, id uuid.UUID) (string, error) {
	var status string
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		inv, err := q.GetInvoice(ctx, id)
		if err != nil {
			return err
		}
		status = inv.Status
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("status %s: %w", id, err)
	}
	return status, nil
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}
