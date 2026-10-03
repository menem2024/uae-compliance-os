//go:build integration

package proposals_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

type failing struct{}

func (failing) Apply(context.Context, *sqlc.Queries, proposals.Proposal) error {
	return errors.New("boom")
}

// seedDoc inserts an uploaded Document of cc with one invoice, as the owner role.
func seedDoc(t *testing.T, env dbtest.Env, firm, cc uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := dbtest.ExecFirm(ctx, env, env.FirmA, `INSERT INTO documents (id, firm_id, client_company_id, sha256, object_key, filename,
		content_type, size_bytes, status, direction) VALUES ($1::uuid, $2::uuid, $3, $4, 'firms/' || $2::text || '/docs/' || $1::text,
		'a.pdf', 'application/pdf', 10, 'extracted', 'received')`, id, firm, cc, uuid.NewString()[:8]+"00000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dbtest.ExecFirm(ctx, env, env.FirmA, `INSERT INTO invoices (firm_id, status, payload, client_company_id, document_id, source_ordinal)
		VALUES ($1, 'extracted', '{}', $2, $3, 0)`, firm, cc, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insert(t *testing.T, env dbtest.Env, firm uuid.UUID, target uuid.UUID, changes []*compliancev1.FieldChange, expires time.Time) uuid.UUID {
	t.Helper()
	now := time.Now()
	pb := &compliancev1.Proposal{ProposalId: uuid.NewString(), FirmId: firm.String(), Agent: "intake",
		Kind: proposals.KindDocumentAttribution, TargetType: "document", TargetId: target.String(),
		SummaryKey: "P1Agents.proposal.attribution", Confidence: 0.9, Changes: changes,
		CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(expires)}
	p, err := proposals.InsertParams(pb, now)
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithFirm(context.Background(), env.App, firm, func(q *sqlc.Queries) error {
		_, err := proposals.Insert(context.Background(), q, p)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestAttributionAcceptMovesDocumentAndInvoices(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	oldCC := dbtest.ClientCompany(t, env, env.FirmA, "Old", "100000000000021")
	newCC := dbtest.ClientCompany(t, env, env.FirmA, "New", "100000000000022")
	doc := seedDoc(t, env, env.FirmA, oldCC)
	change := []*compliancev1.FieldChange{{Path: "client_company_id", OldValue: oldCC.String(), NewValue: newCC.String()},
		{Path: "direction", OldValue: "received", NewValue: "issued"}}
	first := insert(t, env, env.FirmA, doc, change, time.Now().Add(time.Hour))
	second := insert(t, env, env.FirmA, doc, change[:1], time.Now().Add(time.Hour)) // supersedes the first
	reg := proposals.NewRegistry()
	reg.Register(proposals.KindDocumentAttribution, proposals.AttributionApplier{})

	if _, err := proposals.Decide(ctx, env.App, env.FirmA, first, proposals.Accept, "user-1", "", reg, nil); !errors.Is(err, proposals.ErrNotOpen) {
		t.Fatalf("superseded proposal decided: %v", err)
	}
	if _, err := proposals.Decide(ctx, env.App, env.FirmB, second, proposals.Accept, "user-1", "", reg, nil); !errors.Is(err, proposals.ErrNotFound) {
		t.Fatalf("cross-firm decide: %v", err)
	}
	p, err := proposals.Decide(ctx, env.App, env.FirmA, second, proposals.Accept, "user-1", "looks right", reg, nil)
	if err != nil || p.State != proposals.StateAccepted || p.AppliedAt == nil || p.DecidedBy != "user-1" {
		t.Fatalf("%+v %v", p, err)
	}
	var cc uuid.UUID
	var dir string
	var invoices int
	if err := dbtest.QueryRowFirm(ctx, env, env.FirmA, `SELECT client_company_id, direction FROM documents WHERE id = $1`, doc).Scan(&cc, &dir); err != nil || cc != newCC || dir != "unknown" {
		t.Fatalf("document %v %s %v", cc, dir, err)
	}
	if err := dbtest.QueryRowFirm(ctx, env, env.FirmA, `SELECT count(*) FROM invoices WHERE document_id = $1 AND client_company_id = $2`, doc, newCC).Scan(&invoices); err != nil || invoices != 1 {
		t.Fatalf("invoices %d %v", invoices, err)
	}
	if _, err := proposals.Decide(ctx, env.App, env.FirmA, second, proposals.Reject, "user-1", "", reg, nil); !errors.Is(err, proposals.ErrNotOpen) {
		t.Fatalf("decided twice: %v", err)
	}
	// The same change proposed again is stale now: the Document no longer belongs to oldCC.
	third := insert(t, env, env.FirmA, doc, change, time.Now().Add(time.Hour))
	if _, err := proposals.Decide(ctx, env.App, env.FirmA, third, proposals.Accept, "user-1", "", reg, nil); !errors.Is(err, proposals.ErrStale) {
		t.Fatalf("stale: %v", err)
	}
	list, err := proposals.List(ctx, env.App, env.FirmA, proposals.ListFilter{State: proposals.StateProposed, Limit: 10})
	if err != nil || len(list) != 1 || list[0].ID != third {
		t.Fatalf("stale proposal stays open: %+v %v", list, err)
	}
}

func TestDecideRollbackRejectAndExpiry(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	cc := dbtest.ClientCompany(t, env, env.FirmA, "A", "")
	other := dbtest.ClientCompany(t, env, env.FirmA, "B", "")
	doc := seedDoc(t, env, env.FirmA, cc)
	change := []*compliancev1.FieldChange{{Path: "client_company_id", OldValue: cc.String(), NewValue: other.String()}}
	id := insert(t, env, env.FirmA, doc, change, time.Now().Add(time.Hour))

	broken := proposals.NewRegistry()
	broken.Register(proposals.KindDocumentAttribution, failing{})
	if _, err := proposals.Decide(ctx, env.App, env.FirmA, id, proposals.Accept, "u", "", broken, nil); err == nil {
		t.Fatal("applier error swallowed")
	}
	if _, err := proposals.Decide(ctx, env.App, env.FirmA, id, proposals.Accept, "u", "", proposals.NewRegistry(), nil); !errors.Is(err, proposals.ErrNoApplier) {
		t.Fatalf("%v", err)
	}
	p, err := proposals.Decide(ctx, env.App, env.FirmA, id, proposals.Reject, "u", "wrong client", proposals.NewRegistry(), nil)
	if err != nil || p.State != proposals.StateRejected || p.AppliedAt != nil || p.DecisionReason != "wrong client" {
		t.Fatalf("%+v %v", p, err)
	}
	expiredID := insert(t, env, env.FirmA, uuid.New(), change, time.Now().Add(-time.Minute))
	p, err = proposals.Decide(ctx, env.App, env.FirmA, expiredID, proposals.Accept, "u", "", broken, nil)
	if !errors.Is(err, proposals.ErrExpired) || p.State != proposals.StateExpired {
		t.Fatalf("%+v %v", p, err)
	}
	var state string
	if err := dbtest.QueryRowFirm(ctx, env, env.FirmA, `SELECT state FROM proposals WHERE id = $1`, expiredID).Scan(&state); err != nil || state != "expired" {
		t.Fatalf("expiry not committed: %s %v", state, err)
	}
	// A redelivered proposal is a no-op and supersedes nothing.
	again := insert(t, env, env.FirmA, uuid.New(), change, time.Now().Add(time.Hour))
	err = db.WithFirm(ctx, env.App, env.FirmA, func(q *sqlc.Queries) error {
		rows, err := proposals.ForRun(ctx, q, uuid.New())
		if err != nil || len(rows) != 0 {
			t.Errorf("for run: %v %v", rows, err)
		}
		return nil
	})
	if err != nil || again == uuid.Nil {
		t.Fatal(err)
	}
}
