//go:build integration

package exports_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/exports"
)

const payload = `{"invoice_number":"INV/2026 001","seller_trn":"100123456700003","currency":"AED"}`

var xmlDoc = []byte(`<?xml version="1.0" encoding="UTF-8"?><Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"/>`)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type fakeExporter struct {
	calls int
	req   *compliancev1.ExportRequest
	resp  *compliancev1.ExportResponse
	err   error
}

func (f *fakeExporter) Export(_ context.Context, r *connect.Request[compliancev1.ExportRequest]) (*connect.Response[compliancev1.ExportResponse], error) {
	f.calls++
	f.req = r.Msg
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(f.resp), nil
}

func okResponse() *compliancev1.ExportResponse {
	return &compliancev1.ExportResponse{Xml: xmlDoc, Sha256: sum(xmlDoc), Format: "pint-ae-billing-1.0.4/ubl-2.1",
		Exported: true, DocumentKind: "invoice"}
}

type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	putErr  error
	onPut   func() // runs after the object is stored (a concurrent edit)
	puts    int
}

func newMemStore() *memStore {
	return &memStore{objects: map[string][]byte{}, types: map[string]string{}}
}

func (m *memStore) Put(_ context.Context, key string, data []byte, ct string) error {
	m.mu.Lock()
	m.puts++
	if m.putErr != nil {
		m.mu.Unlock()
		return m.putErr
	}
	m.objects[key] = append([]byte(nil), data...)
	m.types[key] = ct
	hook := m.onPut
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (m *memStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	return append([]byte(nil), b...), nil
}

type fixture struct {
	env  trackctest.Env
	exp  *fakeExporter
	st   *memStore
	svc  *exports.Service
	inv  uuid.UUID
	run  uuid.UUID
	firm uuid.UUID
	ids  []uuid.UUID
}

// setup seeds a ready, clean, approved invoice (payload_version 1, run with zero errors).
func setup(t *testing.T) *fixture {
	t.Helper()
	env := trackctest.Setup(t)
	f := &fixture{env: env, exp: &fakeExporter{resp: okResponse()}, st: newMemStore(), firm: env.FirmA}
	f.inv = env.SeedInvoice(t, f.firm, payload)
	f.run = env.SeedRun(t, f.firm, f.inv, "pint-ae@1.0.4+r1", 0, 2)
	f.ownerExec(t, `UPDATE invoices SET latest_run_id=$2, status='ready', approved_payload_version=payload_version,
		approved_by='u1', approved_at=now() WHERE id=$1`, f.inv, f.run)
	f.svc = &exports.Service{Pool: env.App, Exporter: f.exp, Store: f.st, NewID: func() uuid.UUID {
		id := uuid.New()
		f.ids = append(f.ids, id)
		return id
	}}
	return f
}

func (f *fixture) ownerExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	err := f.env.OwnerTx(context.Background(), f.firm, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	err := f.env.OwnerTx(context.Background(), f.firm, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) nothingWritten(t *testing.T) {
	t.Helper()
	if n := f.count(t, `SELECT count(*) FROM exports`); n != 0 {
		t.Errorf("%d export rows", n)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_events WHERE action='invoice.exported'`); n != 0 {
		t.Errorf("%d audit events", n)
	}
}

func TestCreateHappyPath(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	v, err := f.svc.Create(ctx, f.firm, f.inv, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.ids) != 1 || v.ID != f.ids[0] || v.InvoiceID != f.inv || v.RunID != f.run || v.PayloadVersion != 1 {
		t.Fatalf("view %+v ids %v", v, f.ids)
	}
	if v.RulesetVersion != "pint-ae@1.0.4+r1" || v.Format != "pint-ae-billing-1.0.4/ubl-2.1" || v.DocumentKind != "invoice" ||
		v.SHA256 != sum(xmlDoc) || v.SizeBytes != int32(len(xmlDoc)) || v.CreatedBy != "user-1" || v.CreatedAt.IsZero() {
		t.Fatalf("view %+v", v)
	}
	if v.Filename != "INV_2026_001-invoice.xml" {
		t.Fatalf("filename %q", v.Filename)
	}
	// The exporter got the stored payload and the run's RuleSet.
	if f.exp.req.GetRulesetVersion() != "pint-ae@1.0.4+r1" || f.exp.req.GetInvoice().GetSellerTrn() != "100123456700003" {
		t.Fatalf("export request %v", f.exp.req)
	}
	key := fmt.Sprintf("firms/%s/exports/%s.xml", f.firm, v.ID)
	if string(f.st.objects[key]) != string(xmlDoc) || f.st.types[key] != "application/xml; charset=utf-8" {
		t.Fatalf("object %q type %q", f.st.objects[key], f.st.types[key])
	}
	if n := f.count(t, `SELECT count(*) FROM audit_events WHERE action='invoice.exported' AND entity_type='export'
		AND entity_id=$1 AND invoice_id=$2 AND actor_type='user' AND actor_id='user-1'`, v.ID, f.inv); n != 1 {
		t.Fatalf("audit events %d", n)
	}

	got, err := f.svc.Get(ctx, f.firm, v.ID)
	if err != nil || got.ID != v.ID || got.SHA256 != v.SHA256 || got.Filename != v.Filename {
		t.Fatalf("get %+v %v", got, err)
	}
	list, err := f.svc.List(ctx, f.firm, f.inv)
	if err != nil || len(list) != 1 || list[0].ID != v.ID {
		t.Fatalf("list %+v %v", list, err)
	}
	body, view, err := f.svc.Read(ctx, f.firm, v.ID)
	if err != nil || string(body) != string(xmlDoc) || view.ID != v.ID {
		t.Fatalf("read %q %v", body, err)
	}

	// Another Firm sees nothing (RLS) and the same invoice id is not exportable by it.
	if _, err := f.svc.Get(ctx, f.env.FirmB, v.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("other firm get: %v", err)
	}
	if _, _, err := f.svc.Read(ctx, f.env.FirmB, v.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("other firm read: %v", err)
	}
	if l, err := f.svc.List(ctx, f.env.FirmB, f.inv); err != nil || len(l) != 0 {
		t.Fatalf("other firm list %v %v", l, err)
	}
	if _, err := f.svc.Create(ctx, f.env.FirmB, f.inv, "x"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("other firm create: %v", err)
	}
}

func TestCreateRefusesWhatIsNotReady(t *testing.T) {
	cases := map[string]string{
		"validated, not approved": `UPDATE invoices SET status='validated', approved_payload_version=NULL, approved_by=NULL, approved_at=NULL WHERE id=$1`,
		"needs review":            `UPDATE invoices SET status='needs_review', approved_payload_version=NULL, approved_by=NULL, approved_at=NULL WHERE id=$1`,
		"no latest run":           `UPDATE invoices SET status='validated', latest_run_id=NULL, approved_payload_version=NULL, approved_by=NULL, approved_at=NULL WHERE id=$1`,
		// Approval and payload moved on together, but the latest run still describes version 1.
		"latest run is for another payload version": `UPDATE invoices SET payload_version=2, approved_payload_version=2 WHERE id=$1`,
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.ownerExec(t, sql, f.inv)
			_, err := f.svc.Create(context.Background(), f.firm, f.inv, "u")
			if !errors.Is(err, exports.ErrNotReady) {
				t.Fatalf("err = %v", err)
			}
			if f.exp.calls != 0 || f.st.puts != 0 {
				t.Fatalf("exporter calls %d, puts %d", f.exp.calls, f.st.puts)
			}
			f.nothingWritten(t)
		})
	}
}

func TestCreateRefusesDirtyLatestRun(t *testing.T) {
	f := setup(t)
	dirty := f.env.SeedRun(t, f.firm, f.inv, "pint-ae@1.0.4+r1", 3, 0)
	f.ownerExec(t, `UPDATE invoices SET latest_run_id=$2 WHERE id=$1`, f.inv, dirty)
	if _, err := f.svc.Create(context.Background(), f.firm, f.inv, "u"); !errors.Is(err, exports.ErrNotReady) {
		t.Fatalf("err = %v", err)
	}
	if f.exp.calls != 0 || f.st.puts != 0 {
		t.Fatalf("exporter calls %d, puts %d", f.exp.calls, f.st.puts)
	}
	f.nothingWritten(t)
}

func TestCreateExporterSaysNotExported(t *testing.T) {
	f := setup(t)
	f.exp.resp = &compliancev1.ExportResponse{Exported: false, Run: &compliancev1.ValidationRun{RulesetVersion: "pint-ae@1.0.4+r1"}}
	if _, err := f.svc.Create(context.Background(), f.firm, f.inv, "u"); !errors.Is(err, exports.ErrRevalidationRequired) {
		t.Fatalf("err = %v", err)
	}
	if f.st.puts != 0 {
		t.Fatal("nothing may be stored")
	}
	f.nothingWritten(t)
}

func TestCreateExporterFailure(t *testing.T) {
	f := setup(t)
	f.exp.err = connect.NewError(connect.CodeUnavailable, errors.New("down"))
	_, err := f.svc.Create(context.Background(), f.firm, f.inv, "u")
	if err == nil || errors.Is(err, exports.ErrRevalidationRequired) || errors.Is(err, exports.ErrNotReady) {
		t.Fatalf("err = %v", err)
	}
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("the transport error must stay visible, got %v", err)
	}
	f.nothingWritten(t)
}

func TestCreateRejectsInconsistentResponse(t *testing.T) {
	for name, mutate := range map[string]func(*compliancev1.ExportResponse){
		"sha256 mismatch": func(r *compliancev1.ExportResponse) { r.Sha256 = sum([]byte("other")) },
		"empty xml":       func(r *compliancev1.ExportResponse) { r.Xml = nil; r.Sha256 = sum(nil) },
		"bad kind":        func(r *compliancev1.ExportResponse) { r.DocumentKind = "receipt" },
		"empty format":    func(r *compliancev1.ExportResponse) { r.Format = "" },
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			mutate(f.exp.resp)
			_, err := f.svc.Create(context.Background(), f.firm, f.inv, "u")
			if err == nil || errors.Is(err, exports.ErrRevalidationRequired) || errors.Is(err, exports.ErrNotReady) {
				t.Fatalf("err = %v", err)
			}
			if f.st.puts != 0 {
				t.Fatal("nothing may be stored")
			}
			f.nothingWritten(t)
		})
	}
}

func TestCreateStoreFailure(t *testing.T) {
	f := setup(t)
	f.st.putErr = errors.New("s3 down")
	if _, err := f.svc.Create(context.Background(), f.firm, f.inv, "u"); err == nil {
		t.Fatal("expected an error")
	}
	f.nothingWritten(t)
}

// An edit that lands after the object is stored: the insert transaction re-checks and refuses. The
// object stays behind under a random key that no row references, which is harmless.
func TestCreateOrphanedObjectIsTolerated(t *testing.T) {
	f := setup(t)
	f.st.onPut = func() {
		f.ownerExec(t, `UPDATE invoices SET payload_version=2, status='fixed', approved_payload_version=NULL,
			approved_by=NULL, approved_at=NULL WHERE id=$1`, f.inv)
	}
	_, err := f.svc.Create(context.Background(), f.firm, f.inv, "u")
	if !errors.Is(err, exports.ErrNotReady) {
		t.Fatalf("err = %v", err)
	}
	f.nothingWritten(t)
	if len(f.st.objects) != 1 {
		t.Fatalf("expected the orphaned object to remain, have %d objects", len(f.st.objects))
	}
}

func TestCreateRaceOnLatestRun(t *testing.T) {
	f := setup(t)
	f.st.onPut = func() {
		other := f.env.SeedRun(t, f.firm, f.inv, "pint-ae@1.0.4+r1", 0, 0)
		f.ownerExec(t, `UPDATE invoices SET latest_run_id=$2 WHERE id=$1`, f.inv, other)
	}
	if _, err := f.svc.Create(context.Background(), f.firm, f.inv, "u"); !errors.Is(err, exports.ErrNotReady) {
		t.Fatalf("err = %v", err)
	}
	f.nothingWritten(t)
}

func TestReadDetectsCorruption(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	v, err := f.svc.Create(ctx, f.firm, f.inv, "u")
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("firms/%s/exports/%s.xml", f.firm, v.ID)
	f.st.objects[key] = append([]byte(nil), xmlDoc...)
	f.st.objects[key][10] ^= 0x01
	if _, _, err := f.svc.Read(ctx, f.firm, v.ID); !errors.Is(err, exports.ErrCorrupt) {
		t.Fatalf("flipped byte: %v", err)
	}
	f.st.objects[key] = xmlDoc[:len(xmlDoc)-1]
	if _, _, err := f.svc.Read(ctx, f.firm, v.ID); !errors.Is(err, exports.ErrCorrupt) {
		t.Fatalf("truncated: %v", err)
	}
	delete(f.st.objects, key)
	_, _, err = f.svc.Read(ctx, f.firm, v.ID)
	if err == nil || errors.Is(err, exports.ErrCorrupt) {
		t.Fatalf("a missing object is a storage error, not corruption: %v", err)
	}
}

func TestExportsAreAppendOnly(t *testing.T) {
	f := setup(t)
	v, err := f.svc.Create(context.Background(), f.firm, f.inv, "u")
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{`UPDATE exports SET created_by='x' WHERE id=$1`, `DELETE FROM exports WHERE id=$1`} {
		err := f.env.AppTx(context.Background(), f.firm, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), sql, v.ID)
			return err
		})
		if err == nil {
			t.Errorf("%q succeeded for compliance_app", sql)
		}
	}
}
