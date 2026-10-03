// Package trackctest prepares a migrated, throwaway Postgres for the integration tests of the
// Track C packages (validation, audit, invoicefix, exports, fixtasks, fixapply, trackc).
//
// Tests read TEST_OWNER_URL (compliance_owner) and TEST_APP_URL (compliance_app) and fail when
// either is unset. Each task runs its integration tests against one throwaway postgres:17.6
// container named p2-t<NN>-pg on host port 456<NN> (NN = two-digit task number) with
// deploy/compose/postgres/init.sql applied, and removes it afterwards. For task 02, from the
// repository root:
//
//	docker run -d --name p2-t02-pg -e POSTGRES_PASSWORD=pg -p 45602:5432 postgres:17.6
//	until docker exec p2-t02-pg pg_isready -U postgres; do sleep 1; done
//	docker exec -i p2-t02-pg psql -U postgres -v ON_ERROR_STOP=1 < deploy/compose/postgres/init.sql
//	cd services/api-go && \
//	  TEST_OWNER_URL='postgres://compliance_owner:owner_dev_pw@localhost:45602/compliance?sslmode=disable' \
//	  TEST_APP_URL='postgres://compliance_app:app_dev_pw@localhost:45602/compliance?sslmode=disable' \
//	  go test -tags integration -p 1 ./internal/db/...
//	docker rm -f p2-t02-pg
package trackctest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// Env is a migrated database with two fresh Firms.
type Env struct {
	App   *pgxpool.Pool // compliance_app: grants and RLS apply
	Owner *pgxpool.Pool // compliance_owner: owns the tables, still subject to FORCE ROW LEVEL SECURITY
	FirmA uuid.UUID
	FirmB uuid.UUID
}

// Setup migrates the database and seeds two Firms. Both pools close when the test ends.
func Setup(t testing.TB) Env {
	t.Helper()
	ownerURL, appURL := os.Getenv("TEST_OWNER_URL"), os.Getenv("TEST_APP_URL")
	if ownerURL == "" || appURL == "" {
		t.Fatal("trackctest: TEST_OWNER_URL and TEST_APP_URL must be set (see the package doc)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.Migrate(ctx, ownerURL); err != nil {
		t.Fatalf("trackctest: migrate: %v", err)
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
	e := Env{App: app, Owner: owner}
	for _, id := range []*uuid.UUID{&e.FirmA, &e.FirmB} {
		if err := owner.QueryRow(ctx, `INSERT INTO firms(zitadel_org_id, name) VALUES ($1, 'Firm') RETURNING id`,
			uuid.NewString()).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// AppTx runs fn in a compliance_app transaction with app.firm_id set, like db.WithFirm but with the
// raw transaction (for statements that are not sqlc queries).
func (e Env) AppTx(ctx context.Context, firm uuid.UUID, fn func(pgx.Tx) error) error {
	return firmTx(ctx, e.App, firm, fn)
}

// OwnerTx runs fn in a compliance_owner transaction with app.firm_id set, so FORCE ROW LEVEL
// SECURITY shows the Firm's rows to the owner role as well.
func (e Env) OwnerTx(ctx context.Context, firm uuid.UUID, fn func(pgx.Tx) error) error {
	return firmTx(ctx, e.Owner, firm, fn)
}

func firmTx(ctx context.Context, pool *pgxpool.Pool, firm uuid.UUID, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, firm.String()); err != nil {
			return err
		}
		return fn(tx)
	})
}

// SeedInvoice inserts an invoice (status "uploaded", payload_version 1) for firm as compliance_app.
func (e Env) SeedInvoice(t testing.TB, firm uuid.UUID, payload string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := db.WithFirm(ctx, e.App, firm, func(q *sqlc.Queries) error {
		inv, err := q.CreateInvoice(ctx, sqlc.CreateInvoiceParams{FirmID: firm, Payload: []byte(payload)})
		id = inv.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// SeedRun inserts a validation run (payload_version 1, the given counts) for invoice as compliance_app.
// It does not touch the invoice.
func (e Env) SeedRun(t testing.TB, firm, invoice uuid.UUID, rulesetVersion string, errors, warnings int32) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := db.WithFirm(ctx, e.App, firm, func(q *sqlc.Queries) error {
		r, err := q.TrackCInsertRun(ctx, sqlc.TrackCInsertRunParams{
			FirmID: firm, InvoiceID: invoice, PayloadVersion: 1, RulesetVersion: rulesetVersion, Trigger: "manual",
			ErrorCount: errors, WarningCount: warnings,
		})
		id = r.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
