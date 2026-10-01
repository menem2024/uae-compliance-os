//go:build integration

package clients_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/menem2024/uae-platform/services/api-go/internal/clients"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

func sp2(s string) *string { return &s }

// TestPGStoreCRUDRoundTrip covers create/get/update/archive/restore against a
// real Postgres, plus the TRN-uniqueness, cross-Firm isolation and search
// behaviours the task's "Done when" integration step calls for.
func TestPGStoreCRUDRoundTrip(t *testing.T) {
	ctx := context.Background()
	env := dbtest.Setup(t)
	store := clients.PGStore{Pool: env.App}

	c, err := store.Create(ctx, env.FirmA, clients.Fields{Name: "Oasis Trading", TRN: "100234567800003"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Name != "Oasis Trading" || *c.TRN != "100234567800003" || c.Status != "active" {
		t.Fatalf("create: %+v", c)
	}

	// A duplicate TRN in the same Firm is rejected; the same TRN in Firm B is fine.
	if _, err := store.Create(ctx, env.FirmA, clients.Fields{Name: "Dup", TRN: "100234567800003"}); !errors.Is(err, clients.ErrTRNTaken) {
		t.Fatalf("want ErrTRNTaken, got %v", err)
	}
	if _, err := store.Create(ctx, env.FirmB, clients.Fields{Name: "Other Firm Same TRN", TRN: "100234567800003"}); err != nil {
		t.Fatalf("firm B same TRN: %v", err)
	}

	// Get: Firm A sees its row; Firm B does not (RLS, not a leak through 403/404 confusion).
	got, err := store.Get(ctx, env.FirmA, c.ID)
	if err != nil || got.ID != c.ID {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := store.Get(ctx, env.FirmB, c.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("firm B read firm A's client company: %v", err)
	}

	// Update merges over the current row.
	updated, err := store.Update(ctx, env.FirmA, c.ID, clients.Input{NameAr: sp2("الواحة")})
	if err != nil || updated.Name != "Oasis Trading" || updated.NameAr != "الواحة" {
		t.Fatalf("update: %v %+v", err, updated)
	}

	// Archive / restore round trip.
	archived, err := store.SetStatus(ctx, env.FirmA, c.ID, "archived")
	if err != nil || archived.Status != "archived" {
		t.Fatalf("archive: %v %+v", err, archived)
	}
	restored, err := store.SetStatus(ctx, env.FirmA, c.ID, "active")
	if err != nil || restored.Status != "active" {
		t.Fatalf("restore: %v %+v", err, restored)
	}
}

// TestListSearchEscapesLikeAndMatchesNameAr covers the ILIKE-escaping and
// name_ar matching behaviour of List's search.
func TestListSearchEscapesLikeAndMatchesNameAr(t *testing.T) {
	ctx := context.Background()
	env := dbtest.Setup(t)
	store := clients.PGStore{Pool: env.App}

	withUnderscoreTRN, err := store.Create(ctx, env.FirmA, clients.Fields{Name: "Underscore Co", TRN: "100000000000003"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	arabicNamed, err := store.Create(ctx, env.FirmA, clients.Fields{Name: "Named In Arabic", NameAr: "الواحة للتجارة"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// "100_" with the "_" escaped must not match a TRN starting "1000...": the
	// literal underscore, not the SQL wildcard, is what's being searched for.
	rows, err := store.List(ctx, env.FirmA, clients.ListFilter{Status: "all", Query: httpx.ContainsPattern("100_"), Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.ID == withUnderscoreTRN.ID {
			t.Fatalf("escaped '_' matched a TRN that has no literal underscore: %+v", r)
		}
	}

	// A name_ar search matches too.
	rows, err = store.List(ctx, env.FirmA, clients.ListFilter{Status: "all", Query: httpx.ContainsPattern("الواحة"), Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.ID == arabicNamed.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("name_ar search did not find %+v in %+v", arabicNamed, rows)
	}
}

// insertDocument inserts a minimal Document row directly (owner role, firm_id
// set transaction-locally to satisfy FORCE ROW LEVEL SECURITY), the way
// dbtest.ClientCompany does, so List's documents_total/documents_needs_review
// counters can be exercised without the documents package (wave 3).
func insertDocument(t *testing.T, env dbtest.Env, firm, clientCompany uuid.UUID, seed int, status string) {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	objectKey := "firms/" + firm.String() + "/docs/" + id.String()
	err := pgx.BeginFunc(ctx, env.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, firm.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO documents(id, firm_id, client_company_id, sha256, object_key, filename,
			content_type, size_bytes, status) VALUES ($1,$2,$3,$4,$5,'a.pdf','application/pdf',10,$6)`,
			id, firm, clientCompany, sha(seed), objectKey, status)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func sha(i int) string {
	const hexDigits = "0123456789abcdef"
	b := make([]byte, 64)
	for j := range b {
		b[j] = hexDigits[(i+j)%16]
	}
	return string(b)
}

// TestListCountsDocuments covers documents_total and documents_needs_review.
func TestListCountsDocuments(t *testing.T) {
	ctx := context.Background()
	env := dbtest.Setup(t)
	store := clients.PGStore{Pool: env.App}

	c, err := store.Create(ctx, env.FirmA, clients.Fields{Name: "Has Docs"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	insertDocument(t, env, env.FirmA, c.ID, 1, "uploaded")
	insertDocument(t, env, env.FirmA, c.ID, 2, "needs_review")

	rows, err := store.List(ctx, env.FirmA, clients.ListFilter{Status: "all", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.ID != c.ID {
			continue
		}
		found = true
		if r.DocumentsTotal == nil || *r.DocumentsTotal != 2 {
			t.Errorf("documents_total: %v", r.DocumentsTotal)
		}
		if r.DocumentsNeedsReview == nil || *r.DocumentsNeedsReview != 1 {
			t.Errorf("documents_needs_review: %v", r.DocumentsNeedsReview)
		}
	}
	if !found {
		t.Fatalf("client company not found in list: %+v", rows)
	}
}

// TestUpdatePreservesTIN covers the Task 3 correction: client_companies.tin
// (pgtype.Text, set only by direct SQL today since Input/Fields never carry
// it) must survive a PATCH that does not mention it. UpdateClientCompany
// replaces the whole row like trn, so PGStore.Update must re-send the row's
// current tin -- the bug this guards against is a PATCH silently NULLing it.
func TestUpdatePreservesTIN(t *testing.T) {
	ctx := context.Background()
	env := dbtest.Setup(t)
	store := clients.PGStore{Pool: env.App}

	c, err := store.Create(ctx, env.FirmA, clients.Fields{Name: "Tin Co", TRN: "100234567800003"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const tin = "1000000001"
	err = pgx.BeginFunc(ctx, env.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, env.FirmA.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE client_companies SET tin = $1 WHERE id = $2`, tin, c.ID)
		return err
	})
	if err != nil {
		t.Fatalf("seed tin: %v", err)
	}

	// A PATCH that touches an unrelated field (name_ar) must not clear tin.
	if _, err := store.Update(ctx, env.FirmA, c.ID, clients.Input{NameAr: sp2("شركة الضريبة")}); err != nil {
		t.Fatalf("update: %v", err)
	}

	var gotTin string
	err = pgx.BeginFunc(ctx, env.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, env.FirmA.String()); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT tin FROM client_companies WHERE id = $1`, c.ID).Scan(&gotTin)
	})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if gotTin != tin {
		t.Fatalf("PATCH cleared tin: got %q want %q", gotTin, tin)
	}

	// A second PATCH that touches trn (also a full-replace column) must also
	// leave tin untouched: the preservation is independent of which column triggered it.
	if _, err := store.Update(ctx, env.FirmA, c.ID, clients.Input{TRN: sp2("100000000000003")}); err != nil {
		t.Fatalf("update trn: %v", err)
	}
	err = pgx.BeginFunc(ctx, env.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, env.FirmA.String()); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT tin FROM client_companies WHERE id = $1`, c.ID).Scan(&gotTin)
	})
	if err != nil {
		t.Fatalf("read back 2: %v", err)
	}
	if gotTin != tin {
		t.Fatalf("second PATCH cleared tin: got %q want %q", gotTin, tin)
	}
}
