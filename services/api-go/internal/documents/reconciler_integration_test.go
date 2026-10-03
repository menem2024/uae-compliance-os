//go:build integration

package documents_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/documents"
)

// ageDocument backdates a Document's updated_at so it clears ListUnpublishedDocuments' 1-minute
// floor. asOwner is declared in documents_integration_test.go (same package, same build tag).
func ageDocument(t *testing.T, env dbtest.Env, firm, id uuid.UUID, age time.Duration) {
	t.Helper()
	asOwner(t, env.Owner, firm, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE documents SET updated_at = now() - ($2 * interval '1 second') WHERE id = $1`,
			id, age.Seconds())
		return err
	})
}

func TestReconcilerIntegrationRepublishesUnpublished(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	store := documents.PGStore{Pool: env.App}
	bus := &fakeBus{}
	svc := &documents.Service{Store: store, Bus: bus}
	rec := &documents.Reconciler{Firms: store, Store: store, Service: svc, Bus: bus}

	cc := dbtest.ClientCompany(t, env, env.FirmA, "Oasis", "")
	docID := uploadedDocument(t, store, env, env.FirmA, cc)
	ageDocument(t, env, env.FirmA, docID, 2*time.Minute) // past the 1-minute floor, published_at NULL

	if err := rec.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	n := 0
	for _, m := range bus.published {
		if du, ok := m.msg.(*compliancev1.DocumentUploaded); ok && du.GetDocumentId() == docID.String() {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one republish for %s, got %d (bus=%+v)", docID, n, bus.published)
	}

	// A successful republish counts twice: BumpPublishAttempt (enforces the retry cap before publishing)
	// and MarkPublished (+1 on success). A published Document is never republished again.
	doc, err := store.Get(ctx, env.FirmA, docID)
	if err != nil || doc.PublishAttempts != 2 || !doc.PublishedAt.Valid {
		t.Fatalf("%+v %v", doc, err)
	}
}
