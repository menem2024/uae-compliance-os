//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// Needs TEST_OWNER_URL (compliance_owner) and TEST_APP_URL (compliance_app) on a fresh DB with init.sql applied.
func setup(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := db.Migrate(ctx, os.Getenv("TEST_OWNER_URL")); err != nil {
		t.Fatal(err)
	}
	owner, err := pgxpool.New(ctx, os.Getenv("TEST_OWNER_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var a, b uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO firms(zitadel_org_id,name) VALUES ($1,'A') RETURNING id`, uuid.NewString()).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `INSERT INTO firms(zitadel_org_id,name) VALUES ($1,'B') RETURNING id`, uuid.NewString()).Scan(&b); err != nil {
		t.Fatal(err)
	}
	app, err := db.Open(ctx, os.Getenv("TEST_APP_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return app, a, b
}

func TestRLSIsolation(t *testing.T) {
	ctx := context.Background()
	app, a, b := setup(t)

	var invA uuid.UUID
	if err := db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
		inv, err := q.CreateInvoice(ctx, sqlc.CreateInvoiceParams{FirmID: a, Payload: []byte(`{}`)})
		invA = inv.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// (a) firm B cannot see A's invoice
	err := db.WithFirm(ctx, app, b, func(q *sqlc.Queries) error {
		_, err := q.GetInvoice(ctx, invA)
		return err
	})
	if err == nil {
		t.Fatal("firm B read firm A invoice")
	}

	// (b) no firm set means zero rows
	var n int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM invoices`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows without app.firm_id, got %d", n)
	}

	// (c) WITH CHECK blocks cross-tenant insert
	err = db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
		_, err := q.CreateInvoice(ctx, sqlc.CreateInvoiceParams{FirmID: b, Payload: []byte(`{}`)})
		return err
	})
	if err == nil {
		t.Fatal("cross-tenant insert succeeded")
	}

	// (d) every table with firm_id has RLS enabled and forced
	rows, err := app.Query(ctx, `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
		FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid
		JOIN pg_namespace ns ON ns.oid = c.relnamespace
		WHERE a.attname = 'firm_id' AND c.relkind = 'r' AND ns.nspname = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name string
		var rls, force bool
		if err := rows.Scan(&name, &rls, &force); err != nil {
			t.Fatal(err)
		}
		seen++
		if !rls || !force {
			t.Errorf("table %s: rowsecurity=%v force=%v", name, rls, force)
		}
	}
	if seen == 0 {
		t.Fatal("no firm_id tables found")
	}
}

func TestAppRoleCannotBypass(t *testing.T) {
	ctx := context.Background()
	app, _, _ := setup(t)
	var super, bypass bool
	if err := app.QueryRow(ctx, `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&super, &bypass); err != nil {
		t.Fatal(err)
	}
	if super || bypass {
		t.Fatalf("app role has super=%v bypassrls=%v", super, bypass)
	}
}
