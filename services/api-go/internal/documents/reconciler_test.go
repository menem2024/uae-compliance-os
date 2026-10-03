package documents_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/documents"
)

// publishedFor filters bus.published down to the messages for one document id (recon tests run over
// every seeded Firm, so assertions key on the document under test rather than on the bus's total size).
func publishedFor(bus *fakeBus, docID uuid.UUID) []publishedMsg {
	var out []publishedMsg
	for _, m := range bus.published {
		switch v := m.msg.(type) {
		case *compliancev1.DocumentUploaded:
			if v.GetDocumentId() == docID.String() {
				out = append(out, m)
			}
		case *compliancev1.DocumentFailed:
			if v.GetDocumentId() == docID.String() {
				out = append(out, m)
			}
		}
	}
	return out
}

func TestReconcilerTickResetsStuckAndRepublishes(t *testing.T) {
	r := newRig()
	store, bus := r.store, r.bus
	firmA, firmB := uuid.New(), uuid.New()
	store.firms = []uuid.UUID{firmA, firmB}

	stuckID := uuid.New()
	store.docs[stuckID] = sqlcDoc(firmA, stuckID, "processing")

	readyID := uuid.New()
	ready := sqlcDoc(firmA, readyID, "uploaded")
	ready.PublishAttempts = 1
	store.docs[readyID] = ready

	exhaustedID := uuid.New()
	exhausted := sqlcDoc(firmA, exhaustedID, "uploaded")
	exhausted.PublishAttempts = 4
	store.docs[exhaustedID] = exhausted

	rec := &documents.Reconciler{Firms: store, Store: store, Service: r.svc, Bus: bus,
		Now: func() time.Time { return time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC) }}

	if err := rec.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := store.docs[stuckID]; got.Status != "uploaded" {
		t.Fatalf("stuck document not reset: %+v", got)
	}

	if got := store.docs[readyID]; got.Status != "uploaded" || got.PublishAttempts != 2 {
		t.Fatalf("ready document: %+v", got)
	}
	if msgs := publishedFor(bus, readyID); len(msgs) != 1 || msgs[0].subject != "document.uploaded" {
		t.Fatalf("ready republish: %+v", msgs)
	}

	if got := store.docs[exhaustedID]; got.Status != "failed" || got.StatusReason != "publish_exhausted" {
		t.Fatalf("exhausted document: %+v", got)
	}
	msgs := publishedFor(bus, exhaustedID)
	if len(msgs) != 1 || msgs[0].subject != "document.failed" {
		t.Fatalf("exhausted publish: %+v", msgs)
	}
	fail, ok := msgs[0].msg.(*compliancev1.DocumentFailed)
	if !ok || fail.GetReasonCode() != "publish_exhausted" {
		t.Fatalf("%+v", msgs[0].msg)
	}
}

// staleStore wraps fakeStore so Unpublished returns a snapshot of a Document that has already moved
// on by the time BumpPublishAttempt runs (a concurrent Complete or an earlier reconciler pass), so
// BumpPublishAttempt must report pgx.ErrNoRows and the reconciler must skip it without error.
type staleStore struct {
	*fakeStore
	stale sqlc.Document
}

func (s staleStore) Unpublished(context.Context, uuid.UUID) ([]sqlc.Document, error) {
	return []sqlc.Document{s.stale}, nil
}

func TestReconcilerSkipsDocumentThatMovedOn(t *testing.T) {
	r := newRig()
	store, bus := r.store, r.bus
	firm := uuid.New()
	store.firms = []uuid.UUID{firm}

	id := uuid.New()
	stale := sqlcDoc(firm, id, "uploaded") // the snapshot Unpublished would have listed
	moved := stale
	moved.Status, moved.StatusReason = "failed", "some_other_terminal_reason"
	store.docs[id] = moved // but the real row already moved on

	ss := staleStore{fakeStore: store, stale: stale}
	rec := &documents.Reconciler{Firms: ss, Store: ss, Service: r.svc, Bus: bus}

	if err := rec.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.docs[id]; got.Status != "failed" || got.StatusReason != "some_other_terminal_reason" {
		t.Fatalf("moved-on document must be left untouched: %+v", got)
	}
	if msgs := publishedFor(bus, id); len(msgs) != 0 {
		t.Fatalf("no publish expected: %+v", msgs)
	}
}

func TestReconcilerContinuesPastPublishErrors(t *testing.T) {
	r := newRig()
	store, bus := r.store, r.bus
	firmA, firmB := uuid.New(), uuid.New()
	store.firms = []uuid.UUID{firmA, firmB}
	bus.err = errors.New("nats down")

	idA1, idA2, idB1 := uuid.New(), uuid.New(), uuid.New()
	store.docs[idA1] = sqlcDoc(firmA, idA1, "uploaded")
	store.docs[idA2] = sqlcDoc(firmA, idA2, "uploaded")
	store.docs[idB1] = sqlcDoc(firmB, idB1, "uploaded")

	rec := &documents.Reconciler{Firms: store, Store: store, Service: r.svc, Bus: bus}
	if err := rec.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Every row got its attempt bumped despite the Firm A and Firm B publish failures, proving one
	// failing Document (or Firm) never stops the rest of the pass.
	for _, id := range []uuid.UUID{idA1, idA2, idB1} {
		if got := store.docs[id]; got.PublishAttempts != 1 || got.Status != "uploaded" {
			t.Fatalf("%s: %+v", id, got)
		}
	}
}
