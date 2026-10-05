//go:build integration

package validation_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// age sets updated_at of an invoice (a trigger-free column) so the sweeper's MinAge can be tested.
func age(t *testing.T, env trackctest.Env, firm, id uuid.UUID, status string, by time.Duration) {
	t.Helper()
	if err := env.AppTx(context.Background(), firm, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE invoices SET status=$2, updated_at = now() - make_interval(secs => $3) WHERE id=$1`, id, status, by.Seconds())
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSweeperRevalidatesOldFixedInvoices(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	oldA := env.SeedInvoice(t, env.FirmA, payload)
	oldB := env.SeedInvoice(t, env.FirmB, payload)
	recent := env.SeedInvoice(t, env.FirmA, payload)
	other := env.SeedInvoice(t, env.FirmA, payload)
	age(t, env, env.FirmA, oldA, "fixed", time.Minute)
	age(t, env, env.FirmB, oldB, "fixed", time.Minute)
	age(t, env, env.FirmA, recent, "fixed", 0)
	age(t, env, env.FirmA, other, "has_issues", time.Hour)

	v := cleanValidator()
	sw := &validation.Sweeper{Svc: &validation.Service{Pool: env.App, Validator: v}, Pool: env.App, Interval: time.Hour, MinAge: 30 * time.Second}
	n, err := sw.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Errorf("swept %d, want >= 2", n)
	}
	for _, c := range []struct {
		firm, id uuid.UUID
		want     string
	}{{env.FirmA, oldA, "validated"}, {env.FirmB, oldB, "validated"}, {env.FirmA, recent, "fixed"}, {env.FirmA, other, "has_issues"}} {
		if got := getInvoice(t, env, c.firm, c.id).Status; got != c.want {
			t.Errorf("invoice %s: status %s, want %s", c.id, got, c.want)
		}
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1 AND trigger='sweeper'`, oldA); n != 1 {
		t.Errorf("sweeper runs = %d", n)
	}
}

func TestSweeperBatchLimitIs100PerFirm(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	for i := 0; i < 103; i++ {
		id := env.SeedInvoice(t, env.FirmA, payload)
		age(t, env, env.FirmA, id, "fixed", time.Minute)
	}
	sw := &validation.Sweeper{Svc: &validation.Service{Pool: env.App, Validator: cleanValidator()}, Pool: env.App, Interval: time.Hour, MinAge: 30 * time.Second}
	if _, err := sw.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if left := countRows(t, env, env.FirmA, `SELECT count(*) FROM invoices WHERE status='fixed'`); left != 3 {
		t.Errorf("fixed left = %d, want 3 (100 swept)", left)
	}
	if _, err := sw.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if left := countRows(t, env, env.FirmA, `SELECT count(*) FROM invoices WHERE status='fixed'`); left != 0 {
		t.Errorf("fixed left = %d after the second pass", left)
	}
}

func TestSweeperKeepsGoingWhenOneInvoiceFails(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	bad := env.SeedInvoice(t, env.FirmA, `{"no_such_field":"x"}`) // permanent failure
	good := env.SeedInvoice(t, env.FirmA, payload)
	age(t, env, env.FirmA, bad, "fixed", time.Minute)
	age(t, env, env.FirmA, good, "fixed", time.Minute)
	sw := &validation.Sweeper{Svc: &validation.Service{Pool: env.App, Validator: cleanValidator()}, Pool: env.App, Interval: time.Hour, MinAge: time.Second}
	if _, err := sw.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got := getInvoice(t, env, env.FirmA, good).Status; got != "validated" {
		t.Errorf("good invoice: %s", got)
	}
	if got := getInvoice(t, env, env.FirmA, bad).Status; got != "fixed" {
		t.Errorf("bad invoice: %s", got)
	}
	setStatus(t, env, env.FirmA, bad, "uploaded") // keep later sweeps of this shared database quiet
}

func TestSweeperRunStopsWithContext(t *testing.T) {
	env := trackctest.Setup(t)
	id := env.SeedInvoice(t, env.FirmA, payload)
	age(t, env, env.FirmA, id, "fixed", time.Minute)
	sw := &validation.Sweeper{Svc: &validation.Service{Pool: env.App, Validator: cleanValidator()}, Pool: env.App, Interval: 20 * time.Millisecond, MinAge: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sw.Run(ctx); close(done) }()
	deadline := time.After(10 * time.Second)
	for getInvoice(t, env, env.FirmA, id).Status != "validated" {
		select {
		case <-deadline:
			t.Fatal("sweeper never re-validated the invoice")
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
