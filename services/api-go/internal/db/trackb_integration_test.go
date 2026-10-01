//go:build integration

package db_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func num(s string) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		panic(err)
	}
	return n
}

func sha(i int) string { return fmt.Sprintf("%064x", i) }

// seedFirm creates one ClientCompany, one Document, one run, one step and one proposal for firm.
type seeded struct{ cc, doc, run, step, prop uuid.UUID }

func seedFirm(t *testing.T, app *pgxpool.Pool, firm uuid.UUID) seeded {
	t.Helper()
	ctx := context.Background()
	var s seeded
	err := db.WithFirm(ctx, app, firm, func(q *sqlc.Queries) error {
		cc, err := q.CreateClientCompany(ctx, sqlc.CreateClientCompanyParams{FirmID: firm, Name: "Oasis Trading"})
		if err != nil {
			return err
		}
		s.cc = cc.ID
		docID := uuid.New()
		d, err := q.UpsertPendingDocument(ctx, sqlc.UpsertPendingDocumentParams{
			ID: docID, FirmID: firm, ClientCompanyID: cc.ID, Sha256: sha(1),
			ObjectKey: "firms/" + firm.String() + "/docs/" + docID.String(), Filename: "a.pdf",
			ContentType: "application/pdf", SizeBytes: 10, UploadedBy: "user"})
		if err != nil {
			return err
		}
		s.doc = d.ID
		s.run, s.step, s.prop = uuid.New(), uuid.New(), uuid.New()
		if err := q.UpsertRunStarted(ctx, sqlc.UpsertRunStartedParams{ID: s.run, FirmID: firm,
			Workflow: "document_ingestion@1", SubjectType: "document", SubjectID: d.ID.String(),
			DeliveryAttempt: 1, Plan: []byte(`[]`), Budget: []byte(`{}`), StartedAt: ts(time.Now())}); err != nil {
			return err
		}
		if err := q.UpsertStep(ctx, sqlc.UpsertStepParams{ID: s.step, FirmID: firm, RunID: s.run, Seq: 1,
			NodeID: "fetch", DependsOn: []string{}, Agent: "orchestrator", Action: "fetch", Kind: "deterministic",
			Status: "started", Attempt: 1, At: ts(time.Now()), MessageArgs: []byte(`{}`)}); err != nil {
			return err
		}
		_, err = q.InsertProposal(ctx, sqlc.InsertProposalParams{ID: s.prop, FirmID: firm,
			ClientCompanyID: uuid.NullUUID{UUID: cc.ID, Valid: true}, RunID: uuid.NullUUID{UUID: s.run, Valid: true},
			Agent: "intake", Kind: "document.attribution", TargetType: "document", TargetID: d.ID, SummaryKey: "k",
			SummaryArgs: []byte(`{}`), Confidence: num("0.9"), Changes: []byte(`[]`), Evidence: []byte(`[]`),
			CreatedAt: ts(time.Now())})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTrackBTablesAreFirmIsolated(t *testing.T) {
	ctx := context.Background()
	app, a, b := setup(t)
	sa := seedFirm(t, app, a)

	reads := map[string]func(q *sqlc.Queries) error{
		"client_companies": func(q *sqlc.Queries) error { _, err := q.GetClientCompany(ctx, sa.cc); return err },
		"documents":        func(q *sqlc.Queries) error { _, err := q.GetDocument(ctx, sa.doc); return err },
		"agent_runs":       func(q *sqlc.Queries) error { _, err := q.GetRun(ctx, sa.run); return err },
		"proposals":        func(q *sqlc.Queries) error { _, err := q.LockProposal(ctx, sa.prop); return err },
		"agent_steps": func(q *sqlc.Queries) error {
			steps, err := q.ListRunSteps(ctx, sa.run)
			if err == nil && len(steps) == 0 {
				return db.ErrNotFound
			}
			return err
		},
	}
	for table, read := range reads {
		if err := db.WithFirm(ctx, app, a, read); err != nil {
			t.Fatalf("%s: owner firm cannot read its row: %v", table, err)
		}
		if err := db.WithFirm(ctx, app, b, read); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("%s: firm B read firm A's row (err=%v)", table, err)
		}
	}

	// Firm B cannot insert rows claiming firm A (WITH CHECK).
	err := db.WithFirm(ctx, app, b, func(q *sqlc.Queries) error {
		_, err := q.CreateClientCompany(ctx, sqlc.CreateClientCompanyParams{FirmID: a, Name: "x"})
		return err
	})
	if err == nil {
		t.Fatal("cross-firm insert into client_companies succeeded")
	}
	// Firm B cannot point a Document at firm A's ClientCompany (composite FK + RLS).
	err = db.WithFirm(ctx, app, b, func(q *sqlc.Queries) error {
		id := uuid.New()
		_, err := q.UpsertPendingDocument(ctx, sqlc.UpsertPendingDocumentParams{ID: id, FirmID: b,
			ClientCompanyID: sa.cc, Sha256: sha(2), ObjectKey: "firms/" + b.String() + "/docs/" + id.String(),
			Filename: "b.pdf", ContentType: "application/pdf", SizeBytes: 1})
		return err
	})
	if err == nil {
		t.Fatal("document referencing another firm's client company was inserted")
	}
}

func TestFirmsUpdateOwnOnly(t *testing.T) {
	ctx := context.Background()
	app, a, b := setup(t)
	err := db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
		_, err := q.UpdateFirm(ctx, sqlc.UpdateFirmParams{ID: b, Name: "hijack"})
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("firm A updated firm B (err=%v)", err)
	}
	if err := db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
		f, err := q.UpdateFirm(ctx, sqlc.UpdateFirmParams{ID: a, Name: "Renamed",
			BrandColor: pgtype.Text{String: "#123ABC", Valid: true}})
		if err == nil && f.Name != "Renamed" {
			err = errors.New("not renamed")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Exec(ctx, `INSERT INTO firms(zitadel_org_id, name) VALUES ('x', 'x')`); err == nil {
		t.Fatal("app role inserted a firm")
	}
	if _, err := app.Exec(ctx, `UPDATE firms SET zitadel_org_id = 'x' WHERE id = $1`, a); err == nil {
		t.Fatal("app role updated a column outside the column grant")
	}
}

func TestDocumentUpsertDedupAndKeyShape(t *testing.T) {
	ctx := context.Background()
	app, a, _ := setup(t)
	sa := seedFirm(t, app, a)
	// Like the real caller, every request generates a fresh id and derives its key from it: CHECK
	// constraints apply to the proposed row before ON CONFLICT is resolved.
	key := func(id uuid.UUID) string { return "firms/" + a.String() + "/docs/" + id.String() }
	err := db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
		first, err := q.GetDocument(ctx, sa.doc)
		if err != nil {
			return err
		}
		// pending_upload: a second request resets the same row (same id and key) with a new nonce.
		id2 := uuid.New()
		again, err := q.UpsertPendingDocument(ctx, sqlc.UpsertPendingDocumentParams{ID: id2, FirmID: a,
			ClientCompanyID: sa.cc, Sha256: sha(1), ObjectKey: key(id2), Filename: "a2.pdf",
			ContentType: "application/pdf", SizeBytes: 10})
		if err != nil {
			return fmt.Errorf("reset: %w", err)
		}
		if again.ID != first.ID || again.ObjectKey != first.ObjectKey || again.ReprocessNonce == first.ReprocessNonce {
			return errors.New("reset did not keep the id and key and rotate the nonce")
		}
		if _, err := q.MarkDocumentUploaded(ctx, sa.doc); err != nil {
			return err
		}
		// uploaded: the upsert returns no row (pgx.ErrNoRows, which WithFirm maps to ErrNotFound).
		id3 := uuid.New()
		_, err = q.UpsertPendingDocument(ctx, sqlc.UpsertPendingDocumentParams{ID: id3, FirmID: a,
			ClientCompanyID: sa.cc, Sha256: sha(1), ObjectKey: key(id3), Filename: "a3.pdf",
			ContentType: "application/pdf", SizeBytes: 10})
		return err
	})
	if !errors.Is(err, db.ErrNotFound) { // the no-row upsert maps to ErrNotFound and rolls back
		t.Fatalf("want ErrNotFound from the dedup upsert, got %v", err)
	}
	err = db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
		id := uuid.New()
		_, err := q.UpsertPendingDocument(ctx, sqlc.UpsertPendingDocumentParams{ID: id, FirmID: a,
			ClientCompanyID: sa.cc, Sha256: sha(3), ObjectKey: "firms/" + a.String() + "/docs/../x",
			Filename: "c.pdf", ContentType: "application/pdf", SizeBytes: 1})
		return err
	})
	if err == nil {
		t.Fatal("object_key shape CHECK did not fire")
	}
}

func TestRunUpsertsAreOrderIndependent(t *testing.T) {
	ctx := context.Background()
	app, a, _ := setup(t)
	subject := uuid.NewString()
	older, newer := uuid.New(), uuid.New()
	t0 := time.Now().Add(-time.Minute)
	err := db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
		// finished arrives before started: the later started must not revert it to running.
		if err := q.UpsertRunFinished(ctx, sqlc.UpsertRunFinishedParams{ID: newer, FirmID: a, SubjectType: "document",
			SubjectID: subject, Status: "succeeded", FinishedAt: ts(time.Now())}); err != nil {
			return err
		}
		for _, r := range []struct {
			id uuid.UUID
			at time.Time
		}{{newer, t0.Add(30 * time.Second)}, {older, t0}} {
			if err := q.UpsertRunStarted(ctx, sqlc.UpsertRunStartedParams{ID: r.id, FirmID: a, Workflow: "w@1",
				SubjectType: "document", SubjectID: subject, DeliveryAttempt: 1, Plan: []byte(`[]`),
				Budget: []byte(`{}`), StartedAt: ts(r.at)}); err != nil {
				return err
			}
			if _, err := q.AbandonSupersededRuns(ctx, sqlc.AbandonSupersededRunsParams{SubjectType: "document",
				SubjectID: subject}); err != nil {
				return err
			}
		}
		n, err := q.GetRun(ctx, newer)
		if err != nil {
			return err
		}
		o, err := q.GetRun(ctx, older)
		if err != nil {
			return err
		}
		if n.Status != "succeeded" || o.Status != "abandoned" {
			return fmt.Errorf("newer=%s older=%s", n.Status, o.Status)
		}
		// Highest seq wins for one step id.
		step := uuid.New()
		for _, seq := range []int64{3, 2} {
			status := map[int64]string{3: "succeeded", 2: "started"}[seq]
			if err := q.UpsertStep(ctx, sqlc.UpsertStepParams{ID: step, FirmID: a, RunID: newer, Seq: seq,
				NodeID: "n", DependsOn: []string{}, Agent: "a", Action: "x", Kind: "llm", Status: status,
				Attempt: 1, At: ts(time.Now()), MessageArgs: []byte(`{}`)}); err != nil {
				return err
			}
		}
		steps, err := q.ListRunSteps(ctx, newer)
		if err != nil {
			return err
		}
		if len(steps) != 1 || steps[0].Seq != 3 || steps[0].Status != "succeeded" {
			return fmt.Errorf("step not highest-seq-wins: %+v", steps)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProposalsGuardTrigger(t *testing.T) {
	ctx := context.Background()
	app, a, _ := setup(t)
	sa := seedFirm(t, app, a)
	run := func(sql string, args ...any) error {
		conn, err := app.Acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, a.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	cases := []struct {
		name string
		sql  string
		ok   bool
	}{
		{"content is immutable", `UPDATE proposals SET changes = '[{"path":"x"}]' WHERE id = $1`, false},
		{"decided needs decider", `UPDATE proposals SET state = 'accepted' WHERE id = $1`, false},
		{"accept", `UPDATE proposals SET state = 'accepted', decided_by = 'u', decided_at = now(), applied_at = now() WHERE id = $1`, true},
		{"accepted cannot be rejected", `UPDATE proposals SET state = 'rejected' WHERE id = $1`, false},
		{"applied_at cannot change", `UPDATE proposals SET applied_at = now() + interval '1 hour' WHERE id = $1`, false},
		{"no delete grant", `DELETE FROM proposals WHERE id = $1`, false},
	}
	for _, c := range cases {
		err := run(c.sql, sa.prop)
		if (err == nil) != c.ok {
			t.Errorf("%s: ok=%v err=%v", c.name, c.ok, err)
		}
	}
}

// TestClientCompanyTIN covers the reuse-audit correction (docs/REUSE-MANIFEST.md section F): a
// ClientCompany carries its TIN (PINT-AE IBT-032, and the 0235 electronic address IBT-034) in its own
// column, independent of the VAT TRN (IBT-031), which may be a VAT-group TRN.
func TestClientCompanyTIN(t *testing.T) {
	ctx := context.Background()
	app, a, _ := setup(t)
	txt := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
	trn := txt("100000000000003")
	err := db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
		c, err := q.CreateClientCompany(ctx, sqlc.CreateClientCompanyParams{FirmID: a, Name: "Tin Co",
			Trn: trn, Tin: txt("1000000001")})
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}
		if c.Tin != txt("1000000001") || c.Trn != trn {
			return fmt.Errorf("create: tin=%v trn=%v", c.Tin, c.Trn)
		}
		u, err := q.UpdateClientCompany(ctx, sqlc.UpdateClientCompanyParams{ID: c.ID, Name: "Tin Co",
			Trn: trn, Tin: txt("1999999999")})
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		if u.Tin != txt("1999999999") || u.Trn != trn {
			return fmt.Errorf("update: tin=%v trn=%v", u.Tin, u.Trn)
		}
		g, err := q.GetClientCompany(ctx, c.ID)
		if err != nil {
			return err
		}
		if g.Tin != u.Tin {
			return fmt.Errorf("get: tin=%v", g.Tin)
		}
		rows, err := q.ListClientCompanies(ctx, sqlc.ListClientCompaniesParams{Status: "all", PageLimit: 10})
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != c.ID || rows[0].Tin != u.Tin || rows[0].Trn != trn {
			return fmt.Errorf("list: %+v", rows)
		}
		// UpdateClientCompany replaces the whole row like trn: an absent tin clears it.
		cleared, err := q.UpdateClientCompany(ctx, sqlc.UpdateClientCompanyParams{ID: c.ID, Name: "Tin Co", Trn: trn})
		if err != nil {
			return err
		}
		if cleared.Tin.Valid || cleared.Trn != trn {
			return fmt.Errorf("clear: tin=%v trn=%v", cleared.Tin, cleared.Trn)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// TIN shape: 10 digits starting with 1 (ibr-148-ae). A 15-digit TRN is not a TIN.
	for _, bad := range []string{"0123456789", "123456789", "12345678901", "100000000000003", "1abcdefghi",
		" 1000000001", "1000000001\n", ""} {
		err := db.WithFirm(ctx, app, a, func(q *sqlc.Queries) error {
			_, err := q.CreateClientCompany(ctx, sqlc.CreateClientCompanyParams{FirmID: a, Name: "Bad", Tin: txt(bad)})
			return err
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != "client_companies_tin_check" {
			t.Errorf("tin %q: want a client_companies_tin_check violation, got %v", bad, err)
		}
	}
}
