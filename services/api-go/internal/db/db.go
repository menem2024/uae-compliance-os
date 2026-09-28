// Package db owns the Postgres connection pool, migrations and the tenancy
// boundary. Tenant tables are touched only through WithFirm.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// ErrNotFound is returned when a query matches no row (including rows hidden by RLS).
var ErrNotFound = errors.New("not found")

// ErrNilFirm is returned by WithFirm when called without a firm id.
var ErrNilFirm = errors.New("WithFirm: nil firm id")

// Open creates a connection pool. Services connect as compliance_app.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

// WithFirm is the only way to touch tenant tables: it opens a transaction,
// sets app.firm_id transaction-locally for RLS, runs fn, and commits.
// pgx.ErrNoRows returned by fn is mapped to ErrNotFound.
func WithFirm(ctx context.Context, pool *pgxpool.Pool, firmID uuid.UUID, fn func(q *sqlc.Queries) error) error {
	if firmID == uuid.Nil {
		return ErrNilFirm
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, firmID.String()); err != nil {
			return fmt.Errorf("set app.firm_id: %w", err)
		}
		err := fn(sqlc.New(tx))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
}

// FirmByOrg resolves a firm from its Zitadel organisation id. firms is not a
// tenant table, so this runs outside WithFirm.
func FirmByOrg(ctx context.Context, pool *pgxpool.Pool, orgID string) (sqlc.Firm, error) {
	f, err := sqlc.New(pool).FirmByOrg(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}
