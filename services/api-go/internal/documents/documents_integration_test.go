//go:build integration

package documents_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/documents"
)

// asOwner runs fn as compliance_owner with app.firm_id set transaction-locally: documents and
// client_companies are FORCE ROW LEVEL SECURITY, so even the table owner must carry it to see or
// touch firm-scoped rows (same pattern as dbtest.ClientCompany).
func asOwner(t testing.TB, pool *pgxpool.Pool, firm uuid.UUID, fn func(tx pgx.Tx) error) {
	t.Helper()
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, firm.String()); err != nil {
			return err
		}
		return fn(tx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPGStoreUploadLifecycle(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	s := documents.PGStore{Pool: env.App}
	cc := dbtest.ClientCompany(t, env, env.FirmA, "Oasis", "")
	f := documents.FileIn{Filename: "a.pdf", ContentType: "application/pdf", SizeBytes: 29, SHA256: strings.Repeat("ab", 32)}

	p1, err := s.PreparePending(ctx, env.FirmA, cc, "user-1", []documents.FileIn{f})
	if err != nil || len(p1) != 1 || p1[0].Deduplicated || p1[0].Doc.Status != "pending_upload" {
		t.Fatalf("%+v %v", p1, err)
	}
	doc := p1[0].Doc
	if doc.ObjectKey != documents.ObjectKey(env.FirmA, doc.ID) {
		t.Fatal(doc.ObjectKey)
	}
	// A second request while pending resets the same row with a new nonce.
	p2, err := s.PreparePending(ctx, env.FirmA, cc, "user-1", []documents.FileIn{f})
	if err != nil || p2[0].Doc.ID != doc.ID || p2[0].Deduplicated || p2[0].Doc.ReprocessNonce == doc.ReprocessNonce {
		t.Fatalf("reset: %+v %v", p2, err)
	}
	up, moved, err := s.FinishUpload(ctx, env.FirmA, doc.ID, "")
	if err != nil || !moved || up.Status != "uploaded" {
		t.Fatalf("%+v %v %v", up, moved, err)
	}
	if _, moved, err := s.FinishUpload(ctx, env.FirmA, doc.ID, ""); err != nil || moved {
		t.Fatalf("second finish moved=%v err=%v", moved, err)
	}
	// Uploaded: the same bytes for the same ClientCompany are deduplicated, no new row (AC-E3a).
	p3, err := s.PreparePending(ctx, env.FirmA, cc, "user-2", []documents.FileIn{f})
	if err != nil || !p3[0].Deduplicated || p3[0].Doc.ID != doc.ID || p3[0].Doc.Status != "uploaded" {
		t.Fatalf("dedup: %+v %v", p3, err)
	}
	var n int
	asOwner(t, env.Owner, env.FirmA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM documents WHERE firm_id = $1`, env.FirmA).Scan(&n)
	})
	if n != 1 {
		t.Fatalf("rows=%d", n)
	}
	// Publish ack only for the current nonce.
	if err := s.MarkPublished(ctx, env.FirmA, doc.ID, "stale"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, env.FirmA, doc.ID)
	if got.PublishedAt.Valid {
		t.Fatal("stale nonce marked published")
	}
	if err := s.MarkPublished(ctx, env.FirmA, doc.ID, up.ReprocessNonce); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(ctx, env.FirmA, doc.ID); !got.PublishedAt.Valid {
		t.Fatal("not marked published")
	}
}

func TestPGStoreRejectAndTenancy(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	s := documents.PGStore{Pool: env.App}
	ccA := dbtest.ClientCompany(t, env, env.FirmA, "Alpha", "100000000000011")
	ccB := dbtest.ClientCompany(t, env, env.FirmB, "Beta", "100000000000012")
	f := documents.FileIn{Filename: "x.csv", ContentType: "text/csv", SizeBytes: 5, SHA256: strings.Repeat("cd", 32)}

	p, err := s.PreparePending(ctx, env.FirmA, ccA, "u", []documents.FileIn{f})
	if err != nil {
		t.Fatal(err)
	}
	rej, moved, err := s.FinishUpload(ctx, env.FirmA, p[0].Doc.ID, documents.RejectSHA256Mismatch)
	if err != nil || !moved || rej.Status != "rejected" || rej.StatusReason != documents.RejectSHA256Mismatch {
		t.Fatalf("%+v %v", rej, err)
	}
	again, err := s.PreparePending(ctx, env.FirmA, ccA, "u", []documents.FileIn{f})
	if err != nil || again[0].Doc.ID != p[0].Doc.ID || again[0].Doc.Status != "pending_upload" {
		t.Fatalf("rejected rows reset: %+v %v", again, err)
	}
	// Firm A cannot upload to Firm B's ClientCompany, nor see Firm B's rows.
	if _, err := s.PreparePending(ctx, env.FirmA, ccB, "u", []documents.FileIn{f}); !errors.Is(err, documents.ErrClientNotFound) {
		t.Fatalf("cross-firm client: %v", err)
	}
	if _, err := s.Get(ctx, env.FirmB, p[0].Doc.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("cross-firm get: %v", err)
	}
	if rows, err := s.List(ctx, env.FirmB, documents.ListFilter{Limit: 10}); err != nil || len(rows) != 0 {
		t.Fatalf("cross-firm list: %d %v", len(rows), err)
	}
	asOwner(t, env.Owner, env.FirmA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE client_companies SET status = 'archived' WHERE id = $1`, ccA)
		return err
	})
	if _, err := s.PreparePending(ctx, env.FirmA, ccA, "u", []documents.FileIn{f}); !errors.Is(err, documents.ErrClientArchived) {
		t.Fatalf("archived: %v", err)
	}
	cands, err := s.Candidates(ctx, env.FirmA)
	if err != nil || len(cands) != 0 {
		t.Fatalf("archived client must not be a candidate: %v %v", cands, err)
	}
	cands, err = s.Candidates(ctx, env.FirmB)
	if err != nil || len(cands) != 1 || cands[0].GetTrn() != "100000000000012" || cands[0].GetClientCompanyId() != ccB.String() {
		t.Fatalf("%v %v", cands, err)
	}
	if rows, err := s.List(ctx, env.FirmA, documents.ListFilter{Status: "pending_upload", ClientCompanyID: ccA, Limit: 10}); err != nil || len(rows) != 1 {
		t.Fatalf("list: %d %v", len(rows), err)
	}
	if inv, err := s.Invoices(ctx, env.FirmA, p[0].Doc.ID); err != nil || len(inv) != 0 {
		t.Fatalf("invoices: %v %v", inv, err)
	}
	if _, _, err := s.FinishUpload(ctx, env.FirmA, uuid.New(), ""); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}
