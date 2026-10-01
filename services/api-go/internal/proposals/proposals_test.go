package proposals_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

type noop struct{}

func (noop) Apply(context.Context, *sqlc.Queries, proposals.Proposal) error { return nil }

func TestRegistry(t *testing.T) {
	r := proposals.NewRegistry()
	r.Register(proposals.KindDocumentAttribution, proposals.AttributionApplier{})
	if _, ok := r.Get(proposals.KindDocumentAttribution); !ok {
		t.Fatal("missing")
	}
	if _, ok := r.Get("invoice.field_fix"); ok {
		t.Fatal("unexpected")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate kind must panic")
		}
	}()
	r.Register(proposals.KindDocumentAttribution, noop{})
}

func proto(now time.Time) *compliancev1.Proposal {
	return &compliancev1.Proposal{
		ProposalId: uuid.NewString(), FirmId: uuid.NewString(), ClientCompanyId: uuid.NewString(),
		RunId: uuid.NewString(), Agent: "intake", Kind: proposals.KindDocumentAttribution, TargetType: "document",
		TargetId: uuid.NewString(), SummaryKey: "P1Agents.proposal.attribution", SummaryArgs: map[string]string{"trn": "100000000000001"},
		Rationale: "seller TRN matches another client", Confidence: 0.9876,
		Changes:   []*compliancev1.FieldChange{{Path: "client_company_id", OldValue: "a", NewValue: "b"}},
		Detail:    &anypb.Any{TypeUrl: "type.googleapis.com/x", Value: []byte{1, 2}},
		Evidence:  []*compliancev1.Evidence{{Kind: "field", Ref: "seller_trn", Excerpt: "100000000000001"}},
		CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(14 * 24 * time.Hour)),
	}
}

func TestInsertParamsAndFromRowRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	in := proto(now)
	p, err := proposals.InsertParams(in, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if p.Confidence.Int.Int64() != 988 || p.Confidence.Exp != -3 || p.DetailType != "type.googleapis.com/x" ||
		!p.CreatedAt.Time.Equal(now) || !p.ExpiresAt.Valid || !p.RunID.Valid || !p.ClientCompanyID.Valid {
		t.Fatalf("%+v", p)
	}
	row := sqlc.Proposal{ID: p.ID, FirmID: p.FirmID, ClientCompanyID: p.ClientCompanyID, RunID: p.RunID,
		Agent: p.Agent, Kind: p.Kind, TargetType: p.TargetType, TargetID: p.TargetID, SummaryKey: p.SummaryKey,
		SummaryArgs: p.SummaryArgs, Rationale: p.Rationale, Confidence: p.Confidence, Changes: p.Changes,
		DetailType: p.DetailType, Detail: p.Detail, Evidence: p.Evidence, State: "proposed", CreatedAt: p.CreatedAt,
		ExpiresAt: p.ExpiresAt}
	got, err := proposals.FromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := got.Change("client_company_id")
	if !ok || c.NewValue != "b" || got.SummaryArgs["trn"] != "100000000000001" || got.Confidence != 0.988 ||
		len(got.Evidence) != 1 || got.DecidedAt != nil || got.ExpiresAt == nil {
		t.Fatalf("%+v", got)
	}
	// Optional ids may be empty; required ones may not.
	in.RunId, in.ClientCompanyId, in.ExpiresAt, in.CreatedAt, in.Detail = "", "", nil, nil, nil
	p, err = proposals.InsertParams(in, now)
	if err != nil || p.RunID.Valid || p.ClientCompanyID.Valid || p.ExpiresAt.Valid || !p.CreatedAt.Time.Equal(now) || p.Detail != nil {
		t.Fatalf("%+v %v", p, err)
	}
	for _, mut := range []func(*compliancev1.Proposal){
		func(x *compliancev1.Proposal) { x.ProposalId = "x" },
		func(x *compliancev1.Proposal) { x.FirmId = "" },
		func(x *compliancev1.Proposal) { x.TargetId = "doc-1" },
		func(x *compliancev1.Proposal) { x.RunId = "run" },
	} {
		bad := proto(now)
		mut(bad)
		if _, err := proposals.InsertParams(bad, now); err == nil {
			t.Error("malformed proposal accepted")
		}
	}
}

func TestDecideRejectsBadInputBeforeTouchingTheDatabase(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		d     proposals.Decision
		actor string
	}{{"maybe", "user"}, {proposals.Accept, ""}} {
		if _, err := proposals.Decide(ctx, nil, uuid.New(), uuid.New(), c.d, c.actor, "", proposals.NewRegistry(), nil); !errors.Is(err, proposals.ErrInvalidDecision) {
			t.Fatalf("%v", err)
		}
	}
}

func TestIsPermanentPG(t *testing.T) {
	if !proposals.IsPermanentPG(&pgconn.PgError{Code: "23503"}) || !proposals.IsPermanentPG(&pgconn.PgError{Code: "22P02"}) {
		t.Fatal("integrity and data errors are permanent")
	}
	if proposals.IsPermanentPG(&pgconn.PgError{Code: "40001"}) || proposals.IsPermanentPG(errors.New("x")) {
		t.Fatal("serialization failures and plain errors are retryable")
	}
}
