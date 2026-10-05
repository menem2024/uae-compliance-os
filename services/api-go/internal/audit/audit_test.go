package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
)

type fakeRow struct {
	id  uuid.UUID
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*uuid.UUID) = r.id
	*dest[1].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	return nil
}

type fakeDB struct {
	calls []string
	args  []any
	id    uuid.UUID
	err   error
}

func (f *fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}
func (f *fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (f *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.calls = append(f.calls, sql)
	f.args = args
	return fakeRow{id: f.id, err: f.err}
}

func newEvent(inv uuid.UUID) audit.Event {
	return audit.Event{
		Actor:      audit.Actor{Type: "user", ID: "user-1"},
		Action:     "invoice.fields_changed",
		EntityType: "invoice",
		EntityID:   inv,
		InvoiceID:  &inv,
		Changes:    []fieldpath.FieldChange{{Path: "currency", OldValue: "USD", NewValue: "AED"}},
		Before:     map[string]any{"payload_version": 1, "status": "has_issues"},
		After:      map[string]any{"payload_version": 2, "status": "fixed"},
		Reason:     "typo",
	}
}

func TestRecordMapsEvent(t *testing.T) {
	firm, inv, want := uuid.New(), uuid.New(), uuid.New()
	f := &fakeDB{id: want}
	prop := uuid.New()
	ev := newEvent(inv)
	ev.Actor = audit.Actor{Type: "agent", ID: "fix", Agent: "fix", ProposalID: &prop}

	traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	got, err := audit.Record(ctx, sqlc.New(f), firm, ev)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("id = %s, want %s", got, want)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d", len(f.calls))
	}
	// Arguments follow TrackCInsertAuditEvent: firm, actor_type, actor_id, agent, proposal, action,
	// entity_type, entity_id, invoice, changes, before, after, reason, trace_id.
	a := f.args
	if a[0] != firm || a[1] != "agent" || a[2] != "fix" || a[3] != "fix" || a[4] != prop ||
		a[5] != "invoice.fields_changed" || a[6] != "invoice" || a[7] != inv || a[8] != inv || a[12] != "typo" ||
		a[13] != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("args = %v", a)
	}
	var changes []map[string]string
	if err := json.Unmarshal(a[9].([]byte), &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0]["path"] != "currency" || changes[0]["old_value"] != "USD" || changes[0]["new_value"] != "AED" {
		t.Fatalf("changes = %v", changes)
	}
	var before, after map[string]any
	if err := json.Unmarshal(a[10].([]byte), &before); err != nil || before["status"] != "has_issues" {
		t.Fatalf("before = %s (%v)", a[10], err)
	}
	if err := json.Unmarshal(a[11].([]byte), &after); err != nil || after["status"] != "fixed" {
		t.Fatalf("after = %s (%v)", a[11], err)
	}
}

func TestRecordNilsAndDefaults(t *testing.T) {
	firm, inv := uuid.New(), uuid.New()
	f := &fakeDB{id: uuid.New()}
	ev := audit.Event{
		Actor: audit.Actor{Type: "system", ID: "api-go"}, Action: "invoice.approval_revoked",
		EntityType: "invoice", EntityID: inv,
	}
	if _, err := audit.Record(context.Background(), sqlc.New(f), firm, ev); err != nil {
		t.Fatal(err)
	}
	a := f.args
	if a[4] != uuid.Nil || a[8] != uuid.Nil {
		t.Fatalf("nil proposal and invoice must be uuid.Nil, got %v / %v", a[4], a[8])
	}
	if string(a[9].([]byte)) != "[]" {
		t.Fatalf("changes = %s, want []", a[9])
	}
	if b, ok := a[10].([]byte); !ok || b != nil {
		t.Fatalf("before must be a nil []byte (NULL), got %#v", a[10])
	}
	if b, ok := a[11].([]byte); !ok || b != nil {
		t.Fatalf("after must be a nil []byte (NULL), got %#v", a[11])
	}
	if a[13] != "" {
		t.Fatalf("trace id = %v, want empty without a span", a[13])
	}
}

func TestRecordRejectsInvalid(t *testing.T) {
	firm, inv := uuid.New(), uuid.New()
	bad := map[string]func(*audit.Event){
		"unknown action":      func(e *audit.Event) { e.Action = "invoice.exploded" },
		"unknown entity":      func(e *audit.Event) { e.EntityType = "user" },
		"unknown actor type":  func(e *audit.Event) { e.Actor.Type = "robot" },
		"empty actor id":      func(e *audit.Event) { e.Actor.ID = "" },
		"nil entity id":       func(e *audit.Event) { e.EntityID = uuid.Nil },
		"nil firm is checked": nil,
	}
	for name, mut := range bad {
		if mut == nil {
			if _, err := audit.Record(context.Background(), sqlc.New(&fakeDB{}), uuid.Nil, newEvent(inv)); !errors.Is(err, audit.ErrInvalidEvent) {
				t.Errorf("%s: err = %v", name, err)
			}
			continue
		}
		ev := newEvent(inv)
		mut(&ev)
		f := &fakeDB{id: uuid.New()}
		if _, err := audit.Record(context.Background(), sqlc.New(f), firm, ev); !errors.Is(err, audit.ErrInvalidEvent) {
			t.Errorf("%s: err = %v, want ErrInvalidEvent", name, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("%s: wrote to the database", name)
		}
	}
}

func TestRecordPropagatesDBError(t *testing.T) {
	boom := errors.New("boom")
	_, err := audit.Record(context.Background(), sqlc.New(&fakeDB{err: boom}), uuid.New(), newEvent(uuid.New()))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestActionsAreAClosedList(t *testing.T) {
	want := []string{
		"invoice.fields_changed", "invoice.validation_requested", "invoice.approved", "invoice.approval_revoked",
		"invoice.exported", "invoice.fix_requested", "proposal.accepted", "proposal.rejected",
	}
	got := audit.Actions()
	if len(got) != len(want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for i, a := range want {
		if got[i] != a {
			t.Fatalf("actions[%d] = %q, want %q", i, got[i], a)
		}
		if !audit.ValidAction(a) {
			t.Errorf("%q must be valid", a)
		}
	}
	for _, a := range []string{"", "invoice.deleted", "Invoice.approved", "invoice.approved "} {
		if audit.ValidAction(a) {
			t.Errorf("%q must be invalid", a)
		}
	}
	got[0] = "tampered"
	if audit.Actions()[0] != "invoice.fields_changed" {
		t.Fatal("Actions returns the internal slice")
	}
}

func TestActorContext(t *testing.T) {
	if _, ok := audit.ActorFrom(context.Background()); ok {
		t.Fatal("empty context has an actor")
	}
	p := uuid.New()
	a := audit.Actor{Type: "user", ID: "u1", Agent: "fix", ProposalID: &p}
	got, ok := audit.ActorFrom(audit.WithActor(context.Background(), a))
	if !ok || got.ID != "u1" || got.Type != "user" || got.Agent != "fix" || got.ProposalID == nil || *got.ProposalID != p {
		t.Fatalf("round trip = %+v, %v", got, ok)
	}
}
