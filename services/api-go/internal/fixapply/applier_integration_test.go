//go:build integration

// AC-7 (E3): accepting an invoice.field_fix proposal applies its changes, bumps payload_version and
// writes invoice.fields_changed and proposal.accepted in the same transaction; a rejection writes
// proposal.rejected and changes nothing else. Real Postgres 17 with forced RLS (see trackctest).
package fixapply_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
	"github.com/menem2024/uae-platform/services/api-go/internal/fixapply"
	"github.com/menem2024/uae-platform/services/api-go/internal/invoicefix"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

const payload = `{"invoice_number":"INV-1","issue_date":"2026-09-27","seller_trn":"100000000000003","currency":"aed","total_amount":"1050.00"}`

func registry() *proposals.Registry {
	reg := proposals.NewRegistry()
	reg.Register(fixapply.KindFieldFix, fixapply.Applier{})
	reg.Register(proposals.KindDocumentAttribution, proposals.AttributionApplier{})
	return reg
}

func asUser(sub string) context.Context {
	return audit.WithActor(context.Background(), audit.Actor{Type: "user", ID: sub})
}

// propose stores an open invoice.field_fix proposal the way the api-agent-events consumer does.
func propose(t *testing.T, env trackctest.Env, firm, invoice uuid.UUID, changes ...*compliancev1.FieldChange) uuid.UUID {
	t.Helper()
	detail, err := anypb.New(&compliancev1.FixProposalDetail{ValidationRunId: uuid.NewString(), PayloadVersion: 1,
		RulesetVersion: "pint-ae@1.0.4+r1", ErrorsBefore: 2, ErrorsAfter: 0, ResolvedRuleIds: []string{"ibr-cl-04"},
		Notes: []*compliancev1.FixChangeNote{{Path: "currency", Source: "deterministic", Rationale: "upper case"}}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	pb := &compliancev1.Proposal{ProposalId: uuid.NewString(), FirmId: firm.String(), Agent: "fix", Kind: fixapply.KindFieldFix,
		TargetType: "invoice", TargetId: invoice.String(), SummaryKey: "P2Review.proposal.summary",
		SummaryArgs: map[string]string{"changes": "1", "resolved": "1"}, Rationale: "currency codes are upper case",
		Confidence: 1, Changes: changes, Detail: detail, CreatedAt: timestamppb.New(now)}
	arg, err := proposals.InsertParams(pb, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithFirm(context.Background(), env.App, firm, func(q *sqlc.Queries) error {
		_, err := proposals.Insert(context.Background(), q, arg)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return arg.ID
}

func currencyFix() *compliancev1.FieldChange {
	return &compliancev1.FieldChange{Path: "currency", OldValue: "aed", NewValue: "AED"}
}

type invoiceState struct {
	Status  string
	Version int32
	Payload map[string]any
}

func readInvoice(t *testing.T, env trackctest.Env, firm, id uuid.UUID) invoiceState {
	t.Helper()
	var row sqlc.TrackCGetInvoiceRow
	if err := db.WithFirm(context.Background(), env.App, firm, func(q *sqlc.Queries) error {
		var err error
		row, err = q.TrackCGetInvoice(context.Background(), id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s := invoiceState{Status: row.Status, Version: row.PayloadVersion}
	if err := json.Unmarshal(row.Payload, &s.Payload); err != nil {
		t.Fatal(err)
	}
	return s
}

type auditRow struct {
	Action, ActorType, ActorID, Agent, EntityType string
	EntityID                                      uuid.UUID
	InvoiceID                                     *uuid.UUID
	ProposalID                                    *uuid.UUID
	Changes                                       []fieldpath.FieldChange
	Reason                                        string
}

func auditRows(t *testing.T, env trackctest.Env, firm uuid.UUID) []auditRow {
	t.Helper()
	var out []auditRow
	err := env.AppTx(context.Background(), firm, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT action, actor_type, actor_id, agent, entity_type, entity_id, invoice_id,
			proposal_id, changes, reason FROM audit_events ORDER BY occurred_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r auditRow
			var changes []byte
			if err := rows.Scan(&r.Action, &r.ActorType, &r.ActorID, &r.Agent, &r.EntityType, &r.EntityID, &r.InvoiceID,
				&r.ProposalID, &changes, &r.Reason); err != nil {
				return err
			}
			if err := json.Unmarshal(changes, &r.Changes); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func proposalState(t *testing.T, env trackctest.Env, firm, id uuid.UUID) (state, by string) {
	t.Helper()
	err := env.AppTx(context.Background(), firm, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT state, COALESCE(decided_by, '') FROM proposals WHERE id = $1`, id).Scan(&state, &by)
	})
	if err != nil {
		t.Fatal(err)
	}
	return state, by
}

func TestAcceptAppliesChangesAndWritesBothAuditEventsInOneTransaction(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := asUser("user-1")
	inv := env.SeedInvoice(t, env.FirmA, payload)
	id := propose(t, env, env.FirmA, inv, currencyFix())

	got, err := proposals.Decide(ctx, env.App, env.FirmA, id, proposals.Accept, "user-1", "looks right", registry(), fixapply.AuditWriter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != proposals.StateAccepted || got.DecidedBy != "user-1" || got.AppliedAt == nil {
		t.Fatalf("proposal = %+v", got)
	}
	s := readInvoice(t, env, env.FirmA, inv)
	if s.Version != 2 || s.Status != "fixed" || s.Payload["currency"] != "AED" || s.Payload["invoice_number"] != "INV-1" {
		t.Fatalf("invoice = %+v", s)
	}
	rows := auditRows(t, env, env.FirmA)
	if len(rows) != 2 {
		t.Fatalf("audit rows = %+v", rows)
	}
	changed, accepted := rows[0], rows[1]
	if changed.Action != "invoice.fields_changed" || changed.EntityType != "invoice" || changed.EntityID != inv ||
		changed.ActorType != "user" || changed.ActorID != "user-1" || changed.Agent != "fix" ||
		changed.ProposalID == nil || *changed.ProposalID != id || len(changed.Changes) != 1 || changed.Changes[0].NewValue != "AED" {
		t.Errorf("fields_changed = %+v", changed)
	}
	if accepted.Action != "proposal.accepted" || accepted.EntityType != "proposal" || accepted.EntityID != id ||
		accepted.InvoiceID == nil || *accepted.InvoiceID != inv || accepted.ActorID != "user-1" || accepted.Agent != "fix" ||
		accepted.Reason != "looks right" || len(accepted.Changes) != 1 || accepted.Changes[0].Path != "currency" {
		t.Errorf("proposal.accepted = %+v", accepted)
	}
}

func TestRejectWritesOnlyProposalRejected(t *testing.T) {
	env := trackctest.Setup(t)
	inv := env.SeedInvoice(t, env.FirmA, payload)
	id := propose(t, env, env.FirmA, inv, currencyFix())

	got, err := proposals.Decide(asUser("user-2"), env.App, env.FirmA, id, proposals.Reject, "user-2", "not now", registry(), fixapply.AuditWriter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != proposals.StateRejected || got.AppliedAt != nil || got.DecisionReason != "not now" {
		t.Fatalf("proposal = %+v", got)
	}
	s := readInvoice(t, env, env.FirmA, inv)
	if s.Version != 1 || s.Status != "uploaded" || s.Payload["currency"] != "aed" {
		t.Fatalf("a rejection changed the invoice: %+v", s)
	}
	rows := auditRows(t, env, env.FirmA)
	if len(rows) != 1 || rows[0].Action != "proposal.rejected" || rows[0].ActorID != "user-2" || rows[0].Reason != "not now" {
		t.Fatalf("audit rows = %+v", rows)
	}
}

// A proposal made against an older payload still applies when the fields it touches are unchanged, and
// fails closed, without a single write, when one of them moved.
func TestStaleOldValueRollsEverythingBack(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	inv := env.SeedInvoice(t, env.FirmA, payload)

	// A person edits an unrelated field after the agent looked: the proposal is still good.
	if err := db.WithFirm(ctx, env.App, env.FirmA, func(q *sqlc.Queries) error {
		_, err := invoicefix.Apply(ctx, q, env.FirmA, inv, 0,
			[]fieldpath.FieldChange{{Path: "total_amount", OldValue: "1050.00", NewValue: "1050.50"}},
			audit.Actor{Type: "user", ID: "user-9"}, false, "invoice.fields_changed", "typo")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fresh := propose(t, env, env.FirmA, inv, currencyFix())
	if _, err := proposals.Decide(asUser("user-1"), env.App, env.FirmA, fresh, proposals.Accept, "user-1", "", registry(), fixapply.AuditWriter{}); err != nil {
		t.Fatalf("a proposal on unchanged fields must apply across payload versions: %v", err)
	}
	if s := readInvoice(t, env, env.FirmA, inv); s.Version != 3 || s.Payload["currency"] != "AED" || s.Payload["total_amount"] != "1050.50" {
		t.Fatalf("invoice = %+v", s)
	}

	// Now the field the proposal touches moves: a person already fixed the currency differently.
	other := env.SeedInvoice(t, env.FirmA, payload)
	stale := propose(t, env, env.FirmA, other, currencyFix())
	if err := db.WithFirm(ctx, env.App, env.FirmA, func(q *sqlc.Queries) error {
		_, err := invoicefix.Apply(ctx, q, env.FirmA, other, 0,
			[]fieldpath.FieldChange{{Path: "currency", OldValue: "aed", NewValue: "USD"}},
			audit.Actor{Type: "user", ID: "user-9"}, false, "invoice.fields_changed", "customer pays in USD")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	beforeStale := len(auditRows(t, env, env.FirmA))
	_, err := proposals.Decide(asUser("user-1"), env.App, env.FirmA, stale, proposals.Accept, "user-1", "", registry(), fixapply.AuditWriter{})
	if !errors.Is(err, proposals.ErrStale) || !errors.Is(err, fieldpath.ErrOldValueMismatch) {
		t.Fatalf("err = %v, want ErrStale wrapping ErrOldValueMismatch", err)
	}
	if st, by := proposalState(t, env, env.FirmA, stale); st != proposals.StateProposed || by != "" {
		t.Errorf("a failed accept left the proposal %s decided by %q", st, by)
	}
	if s := readInvoice(t, env, env.FirmA, other); s.Payload["currency"] != "USD" || s.Version != 2 {
		t.Errorf("the human's value was overwritten: %+v", s)
	}
	if n := len(auditRows(t, env, env.FirmA)); n != beforeStale {
		t.Errorf("a rolled-back accept left %d audit rows behind", n-beforeStale)
	}
}

// The Fix agent cannot change an identifier a person must type, even if its proposal says so.
func TestAgentForbiddenPathFailsEvenFromTheFixAgent(t *testing.T) {
	env := trackctest.Setup(t)
	inv := env.SeedInvoice(t, env.FirmA, payload)
	id := propose(t, env, env.FirmA, inv, &compliancev1.FieldChange{Path: "invoice_number", OldValue: "INV-1", NewValue: "INV-2"})

	_, err := proposals.Decide(asUser("user-1"), env.App, env.FirmA, id, proposals.Accept, "user-1", "", registry(), fixapply.AuditWriter{})
	if !errors.Is(err, invoicefix.ErrForbiddenPath) || !errors.Is(err, proposals.ErrBadProposal) {
		t.Fatalf("err = %v", err)
	}
	if st, _ := proposalState(t, env, env.FirmA, id); st != proposals.StateProposed {
		t.Errorf("state = %s", st)
	}
	if s := readInvoice(t, env, env.FirmA, inv); s.Version != 1 || s.Payload["invoice_number"] != "INV-1" {
		t.Errorf("invoice = %+v", s)
	}
	if rows := auditRows(t, env, env.FirmA); len(rows) != 0 {
		t.Errorf("audit rows = %+v", rows)
	}
}

// An accept without a signed-in user in the context writes nothing: the Applier never trusts the
// proposal's own agent as the actor.
func TestAcceptWithoutHumanActorWritesNothing(t *testing.T) {
	env := trackctest.Setup(t)
	inv := env.SeedInvoice(t, env.FirmA, payload)
	id := propose(t, env, env.FirmA, inv, currencyFix())

	_, err := proposals.Decide(context.Background(), env.App, env.FirmA, id, proposals.Accept, "user-1", "", registry(), fixapply.AuditWriter{})
	if !errors.Is(err, fixapply.ErrNoHumanActor) {
		t.Fatalf("err = %v", err)
	}
	if s := readInvoice(t, env, env.FirmA, inv); s.Version != 1 {
		t.Errorf("invoice = %+v", s)
	}
}

func TestAnotherFirmCannotDecide(t *testing.T) {
	env := trackctest.Setup(t)
	inv := env.SeedInvoice(t, env.FirmA, payload)
	id := propose(t, env, env.FirmA, inv, currencyFix())

	_, err := proposals.Decide(asUser("user-b"), env.App, env.FirmB, id, proposals.Accept, "user-b", "", registry(), fixapply.AuditWriter{})
	if !errors.Is(err, proposals.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if s := readInvoice(t, env, env.FirmA, inv); s.Version != 1 {
		t.Errorf("invoice = %+v", s)
	}
}

// proposal.accepted/rejected is written for every kind, not only invoice field fixes.
func TestAuditWriterCoversEveryKind(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	p := proposals.Proposal{ID: uuid.New(), FirmID: env.FirmA, Agent: "intake", Kind: proposals.KindDocumentAttribution,
		TargetType: "document", TargetID: uuid.New(), State: proposals.StateRejected,
		Changes: []proposals.Change{{Path: "client_company_id", OldValue: "a", NewValue: "b"}}}
	if err := db.WithFirm(ctx, env.App, env.FirmA, func(q *sqlc.Queries) error {
		return fixapply.AuditWriter{}.Record(ctx, q, p, proposals.Reject, "user-3", "wrong company")
	}); err != nil {
		t.Fatal(err)
	}
	rows := auditRows(t, env, env.FirmA)
	if len(rows) != 1 || rows[0].Action != "proposal.rejected" || rows[0].EntityType != "proposal" || rows[0].InvoiceID != nil ||
		rows[0].Agent != "intake" || rows[0].Reason != "wrong company" {
		t.Fatalf("rows = %+v", rows)
	}
}
