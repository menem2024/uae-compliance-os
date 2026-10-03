package invoicefix_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
	"github.com/menem2024/uae-platform/services/api-go/internal/invoicefix"
)

type fakeRow struct {
	scan func(dest ...any) error
}

func (r fakeRow) Scan(dest ...any) error { return r.scan(dest...) }

// fakeDB answers the three statements Apply issues and records their order.
type fakeDB struct {
	calls      []string
	payload    []byte
	version    int32
	status     string
	lockErr    error
	applyErr   error
	newVersion int32
	applyArgs  []any
	auditArgs  []any
}

func (f *fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}
func (f *fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (f *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "FOR UPDATE"):
		f.calls = append(f.calls, "lock")
		return fakeRow{func(d ...any) error {
			if f.lockErr != nil {
				return f.lockErr
			}
			*d[0].(*uuid.UUID) = args[0].(uuid.UUID)
			*d[1].(*string) = f.status
			*d[2].(*[]byte) = f.payload
			*d[3].(*int32) = f.version
			*d[4].(*uuid.UUID) = uuid.Nil
			*d[5].(*pgtype.Int4) = pgtype.Int4{Int32: f.version, Valid: true}
			*d[6].(*pgtype.Text) = pgtype.Text{String: "u0", Valid: true}
			*d[7].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			*d[8].(*pgtype.Text) = pgtype.Text{}
			*d[9].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			*d[10].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			return nil
		}}
	case strings.Contains(sql, "UPDATE invoices"):
		f.calls = append(f.calls, "apply")
		f.applyArgs = args
		return fakeRow{func(d ...any) error {
			if f.applyErr != nil {
				return f.applyErr
			}
			*d[0].(*int32) = f.newVersion
			return nil
		}}
	case strings.Contains(sql, "INSERT INTO audit_events"):
		f.calls = append(f.calls, "audit")
		f.auditArgs = args
		return fakeRow{func(d ...any) error {
			*d[0].(*uuid.UUID) = uuid.New()
			*d[1].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			return nil
		}}
	}
	f.calls = append(f.calls, "unexpected: "+sql)
	return fakeRow{func(...any) error { return errors.New("unexpected query") }}
}

func newFake(t *testing.T, version int32) *fakeDB {
	t.Helper()
	inv := &compliancev1.Invoice{
		InvoiceNumber: "INV-1", Currency: "USD",
		Seller: &compliancev1.Party{PostalAddress: &compliancev1.PostalAddress{CountrySubdivision: "DXB"}},
	}
	b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeDB{payload: b, version: version, status: "has_issues", newVersion: version + 1}
}

var user = audit.Actor{Type: "user", ID: "user-1"}

func currencyFix() []fieldpath.FieldChange {
	return []fieldpath.FieldChange{{Path: "currency", OldValue: "USD", NewValue: "AED"}}
}

func TestApplyHappyPath(t *testing.T) {
	f := newFake(t, 3)
	firm, inv := uuid.New(), uuid.New()
	v, err := invoicefix.Apply(context.Background(), sqlc.New(f), firm, inv, 3, currencyFix(), user, false,
		"invoice.fields_changed", "fix currency")
	if err != nil {
		t.Fatal(err)
	}
	if v != 4 {
		t.Fatalf("version = %d, want 4", v)
	}
	if got := strings.Join(f.calls, ","); got != "lock,apply,audit" {
		t.Fatalf("call order = %s; the row must be locked first", got)
	}
	// TrackCApplyPayload args: payload, id, expected version.
	if f.applyArgs[1] != inv || f.applyArgs[2] != int32(3) {
		t.Fatalf("apply args = %v", f.applyArgs)
	}
	var m map[string]any
	if err := json.Unmarshal(f.applyArgs[0].([]byte), &m); err != nil {
		t.Fatal(err)
	}
	if m["currency"] != "AED" || m["invoice_number"] != "INV-1" {
		t.Fatalf("stored payload = %v (want proto field names and the change applied)", m)
	}
	if _, camel := m["invoiceNumber"]; camel {
		t.Fatal("payload must use proto names")
	}
	// audit args: firm, actor_type, actor_id, agent, proposal, action, entity, entity_id, invoice, changes, before, after, reason
	a := f.auditArgs
	if a[0] != firm || a[1] != "user" || a[5] != "invoice.fields_changed" || a[6] != "invoice" || a[7] != inv ||
		a[8] != inv || a[12] != "fix currency" {
		t.Fatalf("audit args = %v", a)
	}
	var changes []fieldpath.FieldChange
	if err := json.Unmarshal(a[9].([]byte), &changes); err != nil || len(changes) != 1 || changes[0].NewValue != "AED" {
		t.Fatalf("audit changes = %s (%v)", a[9], err)
	}
	var before, after map[string]any
	_ = json.Unmarshal(a[10].([]byte), &before)
	_ = json.Unmarshal(a[11].([]byte), &after)
	if fmt.Sprint(before["payload_version"]) != "3" || fmt.Sprint(after["payload_version"]) != "4" || after["status"] != "fixed" {
		t.Fatalf("snapshots before=%v after=%v", before, after)
	}
}

func TestApplyExpectedVersionZeroMeansAny(t *testing.T) {
	f := newFake(t, 9)
	if _, err := invoicefix.Apply(context.Background(), sqlc.New(f), uuid.New(), uuid.New(), 0, currencyFix(), user, false,
		"invoice.fields_changed", ""); err != nil {
		t.Fatal(err)
	}
	if f.applyArgs[2] != int32(9) {
		t.Fatalf("the update must guard on the locked row's version, got %v", f.applyArgs[2])
	}
}

func TestApplyStalePayload(t *testing.T) {
	f := newFake(t, 5)
	_, err := invoicefix.Apply(context.Background(), sqlc.New(f), uuid.New(), uuid.New(), 4, currencyFix(), user, false,
		"invoice.fields_changed", "")
	if !errors.Is(err, invoicefix.ErrStalePayload) {
		t.Fatalf("err = %v, want ErrStalePayload", err)
	}
	if got := strings.Join(f.calls, ","); got != "lock" {
		t.Fatalf("calls = %s; a stale version must stop right after the lock", got)
	}
}

func TestApplyLostUpdateIsStale(t *testing.T) {
	f := newFake(t, 5)
	f.applyErr = pgx.ErrNoRows
	_, err := invoicefix.Apply(context.Background(), sqlc.New(f), uuid.New(), uuid.New(), 5, currencyFix(), user, false,
		"invoice.fields_changed", "")
	if !errors.Is(err, invoicefix.ErrStalePayload) {
		t.Fatalf("err = %v, want ErrStalePayload", err)
	}
}

func TestApplyAgentForbiddenPath(t *testing.T) {
	changes := []fieldpath.FieldChange{{Path: "invoice_number", OldValue: "INV-1", NewValue: "INV-9"}}
	f := newFake(t, 1)
	agent := audit.Actor{Type: "agent", ID: "fix", Agent: "fix"}
	_, err := invoicefix.Apply(context.Background(), sqlc.New(f), uuid.New(), uuid.New(), 0, changes, agent, true,
		"invoice.fields_changed", "")
	if !errors.Is(err, invoicefix.ErrForbiddenPath) {
		t.Fatalf("err = %v, want ErrForbiddenPath", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("calls = %v; nothing may be touched", f.calls)
	}
	// The same change by a human is allowed.
	f = newFake(t, 1)
	if _, err := invoicefix.Apply(context.Background(), sqlc.New(f), uuid.New(), uuid.New(), 0, changes, user, false,
		"invoice.fields_changed", ""); err != nil {
		t.Fatalf("human correction: %v", err)
	}
}

func TestApplyPropagatesFieldpathErrors(t *testing.T) {
	cases := map[string]struct {
		changes []fieldpath.FieldChange
		want    error
	}{
		"old value": {[]fieldpath.FieldChange{{Path: "currency", OldValue: "EUR", NewValue: "AED"}}, fieldpath.ErrOldValueMismatch},
		"bad path":  {[]fieldpath.FieldChange{{Path: "seller", NewValue: "x"}}, fieldpath.ErrBadPath},
		"none":      {nil, fieldpath.ErrNoChanges},
	}
	for name, c := range cases {
		f := newFake(t, 1)
		_, err := invoicefix.Apply(context.Background(), sqlc.New(f), uuid.New(), uuid.New(), 0, c.changes, user, false,
			"invoice.fields_changed", "")
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
		for _, call := range f.calls {
			if call == "apply" || call == "audit" {
				t.Errorf("%s: wrote after failure (%v)", name, f.calls)
			}
		}
	}
}

func TestApplyInvoiceNotFound(t *testing.T) {
	f := newFake(t, 1)
	f.lockErr = pgx.ErrNoRows
	_, err := invoicefix.Apply(context.Background(), sqlc.New(f), uuid.New(), uuid.New(), 0, currencyFix(), user, false,
		"invoice.fields_changed", "")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("err = %v; pgx.ErrNoRows must stay visible so db.WithFirm maps it to ErrNotFound", err)
	}
}

func TestApplyRejectsUnknownAuditAction(t *testing.T) {
	f := newFake(t, 1)
	_, err := invoicefix.Apply(context.Background(), sqlc.New(f), uuid.New(), uuid.New(), 0, currencyFix(), user, false,
		"invoice.exploded", "")
	if !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("err = %v, want audit.ErrInvalidEvent", err)
	}
}
