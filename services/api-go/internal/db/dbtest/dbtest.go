// Package dbtest sets up Postgres for the integration tests of Track B
// packages (TEST_OWNER_URL, TEST_APP_URL; skipped when unset).
package dbtest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
)

// Env is a migrated database with two fresh Firms.
type Env struct {
	App   *pgxpool.Pool // compliance_app: RLS applies
	Owner *pgxpool.Pool // compliance_owner: seeding and assertions only
	FirmA uuid.UUID
	FirmB uuid.UUID
	OrgA  string
	OrgB  string
}

// Setup migrates the database and seeds two Firms with unique org ids.
func Setup(t testing.TB) Env {
	t.Helper()
	ownerURL, appURL := os.Getenv("TEST_OWNER_URL"), os.Getenv("TEST_APP_URL")
	if ownerURL == "" || appURL == "" {
		t.Skip("TEST_OWNER_URL/TEST_APP_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.Migrate(ctx, ownerURL); err != nil {
		t.Fatal(err)
	}
	owner, err := pgxpool.New(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	app, err := db.Open(ctx, appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	e := Env{App: app, Owner: owner, OrgA: uuid.NewString(), OrgB: uuid.NewString()}
	for _, f := range []struct {
		org string
		id  *uuid.UUID
	}{{e.OrgA, &e.FirmA}, {e.OrgB, &e.FirmB}} {
		if err := owner.QueryRow(ctx, `INSERT INTO firms(zitadel_org_id, name) VALUES ($1, 'Firm') RETURNING id`, f.org).Scan(f.id); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// ClientCompany inserts an active ClientCompany for firm as the owner role.
// client_companies is FORCE ROW LEVEL SECURITY, so even the owner role must
// carry app.firm_id to satisfy the firm_isolation policy's WITH CHECK; that
// is set transaction-locally around the insert.
func ClientCompany(t testing.TB, e Env, firm uuid.UUID, name, trn string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	var trnArg any
	if trn != "" {
		trnArg = trn
	}
	err := pgx.BeginFunc(ctx, e.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, firm.String()); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`INSERT INTO client_companies(firm_id, name, trn) VALUES ($1, $2, $3) RETURNING id`, firm, name, trnArg).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// ExecFirm runs a statement as the owner role with app.firm_id set for the transaction, so it
// satisfies FORCE ROW LEVEL SECURITY (the owner role has no BYPASSRLS).
func ExecFirm(ctx context.Context, e Env, firm uuid.UUID, sql string, args ...any) (pgconn.CommandTag, error) {
	var tag pgconn.CommandTag
	err := pgx.BeginFunc(ctx, e.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, firm.String()); err != nil {
			return err
		}
		var err error
		tag, err = tx.Exec(ctx, sql, args...)
		return err
	})
	return tag, err
}

type firmRow struct {
	ctx  context.Context
	e    Env
	firm uuid.UUID
	sql  string
	args []any
}

// Scan runs the query inside a firm-scoped transaction and scans the single row.
func (r firmRow) Scan(dst ...any) error {
	return pgx.BeginFunc(r.ctx, r.e.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.ctx, `SELECT set_config('app.firm_id', $1, true)`, r.firm.String()); err != nil {
			return err
		}
		return tx.QueryRow(r.ctx, r.sql, r.args...).Scan(dst...)
	})
}

// QueryRowFirm is the firm-scoped counterpart of Owner.QueryRow.
func QueryRowFirm(ctx context.Context, e Env, firm uuid.UUID, sql string, args ...any) pgx.Row {
	return firmRow{ctx: ctx, e: e, firm: firm, sql: sql, args: args}
}
