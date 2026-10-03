//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

type seenValidator struct {
	seen []*compliancev1.Invoice
	run  *compliancev1.ValidationRun
}

func (v *seenValidator) Validate(_ context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	v.seen = append(v.seen, req.Msg.GetInvoice())
	return connect.NewResponse(&compliancev1.ValidateResponse{Run: v.run}), nil
}

// TestHandleExtractedAgainstPostgres runs the adapter over the real validation.Service and database:
// the stored payload (not the event's copy) is validated, the Phase 0 GET shape still works, and a
// redelivery of the event is a no-op (AC-11).
func TestHandleExtractedAgainstPostgres(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	store := PGStore{Pool: env.App}
	id, err := store.Create(ctx, env.FirmA, []byte(`{"invoice_number":"STORED-1","issue_date":"2026-09-27","seller_trn":"100000000000003","currency":"AED","total_amount":"10.00","vat_amount":"0.50"}`))
	if err != nil {
		t.Fatal(err)
	}
	v := &seenValidator{run: &compliancev1.ValidationRun{RulesetVersion: "pint-ae@1.0.4+r1", Issues: []*compliancev1.ValidationIssue{
		{RuleId: "ibr-132-ae", Severity: compliancev1.Severity_SEVERITY_ERROR, Path: "seller_trn", Message: "bad"},
	}}}
	h := HandleExtracted(&validation.Service{Pool: env.App, Validator: v})
	ev := &compliancev1.InvoiceExtracted{InvoiceId: id.String(), FirmId: env.FirmA.String(),
		Invoice: &compliancev1.Invoice{InvoiceNumber: "EVENT-COPY"}}

	if err := h(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if len(v.seen) != 1 || v.seen[0].GetInvoiceNumber() != "STORED-1" {
		t.Fatalf("validator saw %v, want the stored payload", v.seen)
	}
	got, err := store.Get(ctx, env.FirmA, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "has_issues" || got.RulesetVersion == nil || *got.RulesetVersion != "pint-ae@1.0.4+r1" ||
		len(got.Issues) != 1 || got.Issues[0].RuleID != "ibr-132-ae" || got.Issues[0].Severity != "error" {
		t.Errorf("GET view = %+v", got)
	}
	if err := h(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if len(v.seen) != 1 {
		t.Errorf("redelivery re-validated (%d calls)", len(v.seen))
	}

	// An unknown invoice and a foreign Firm are permanent.
	for name, e := range map[string]*compliancev1.InvoiceExtracted{
		"missing":      {InvoiceId: uuid.NewString(), FirmId: env.FirmA.String()},
		"cross-tenant": {InvoiceId: id.String(), FirmId: env.FirmB.String()},
	} {
		if err := h(ctx, e); !errors.Is(err, events.ErrPermanent) {
			t.Errorf("%s: err = %v, want ErrPermanent", name, err)
		}
	}
}

// TestCreatedFullInvoiceIsStoredWithProtoNames posts a full invoice through the router into Postgres.
func TestCreatedFullInvoiceIsStoredWithProtoNames(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	st := okStoreFor(env.FirmA, PGStore{Pool: env.App})
	pub := &fakePub{}
	h := NewRouter(fakeVerifier{}, st, pub, fakeLimiter{ok: true}, func(context.Context) error { return nil })
	rec := do(h, "POST", "/v1/invoices", "tok-a", fullBody)
	if rec.Code != 202 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	var raw []byte
	if err := env.AppTx(ctx, env.FirmA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload FROM invoices WHERE id=$1`, out.ID).Scan(&raw)
	}); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["invoice_type_code"] != "380" || m["seller"].(map[string]any)["name"] != "Seller LLC" || len(m["lines"].([]any)) != 1 {
		t.Errorf("stored payload = %s", raw)
	}
}

// firmStore resolves org-A to a real firm and delegates the rest to PGStore.
type firmStore struct {
	PGStore
	firm uuid.UUID
}

func okStoreFor(firm uuid.UUID, pg PGStore) firmStore { return firmStore{PGStore: pg, firm: firm} }

func (f firmStore) FirmIDForOrg(_ context.Context, org string) (uuid.UUID, error) {
	return fakeStore{firm: f.firm}.FirmIDForOrg(context.Background(), org)
}
