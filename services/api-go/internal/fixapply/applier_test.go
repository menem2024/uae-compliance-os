package fixapply_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/fixapply"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

func fieldFix(changes ...proposals.Change) proposals.Proposal {
	return proposals.Proposal{ID: uuid.New(), FirmID: uuid.New(), Agent: "fix", Kind: fixapply.KindFieldFix,
		TargetType: "invoice", TargetID: uuid.New(), Changes: changes}
}

func userCtx() context.Context {
	return audit.WithActor(context.Background(), audit.Actor{Type: "user", ID: "u-1"})
}

// A proposal the Applier cannot make sense of fails before it touches the database (q is nil here, so
// reaching the database would panic).
func TestApplyRejectsMalformedProposals(t *testing.T) {
	good := proposals.Change{Path: "currency", OldValue: "aed", NewValue: "AED"}
	wrongTarget := fieldFix(good)
	wrongTarget.TargetType = "document"
	badDetail := fieldFix(good)
	badDetail.DetailType, badDetail.Detail = "type.googleapis.com/compliance.v1.FixProposalDetail", []byte{0xff, 0xff, 0xff}
	foreignDetail := fieldFix(good)
	foreignDetail.DetailType, foreignDetail.Detail = "type.googleapis.com/compliance.v1.Invoice", nil
	forbidden := fieldFix(proposals.Change{Path: "invoice_number", OldValue: "A", NewValue: "B"})

	for name, tc := range map[string]struct {
		p    proposals.Proposal
		want error
	}{
		"no changes":      {fieldFix(), proposals.ErrBadProposal},
		"not an invoice":  {wrongTarget, proposals.ErrBadProposal},
		"garbled detail":  {badDetail, proposals.ErrBadProposal},
		"foreign detail":  {foreignDetail, proposals.ErrBadProposal},
		"forbidden path":  {forbidden, proposals.ErrBadProposal},
		"unchanged value": {fieldFix(proposals.Change{Path: "currency", OldValue: "AED", NewValue: "AED"}), proposals.ErrBadProposal},
	} {
		t.Run(name, func(t *testing.T) {
			err := fixapply.Applier{}.Apply(userCtx(), nil, tc.p)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// Human decides: without a signed-in user nothing is applied, whatever the proposal says.
func TestApplyNeedsAHumanActor(t *testing.T) {
	p := fieldFix(proposals.Change{Path: "currency", OldValue: "aed", NewValue: "AED"})
	for name, ctx := range map[string]context.Context{
		"no actor":     context.Background(),
		"agent actor":  audit.WithActor(context.Background(), audit.Actor{Type: "agent", ID: "fix"}),
		"system actor": audit.WithActor(context.Background(), audit.Actor{Type: "system", ID: "api-go"}),
		"empty user":   audit.WithActor(context.Background(), audit.Actor{Type: "user"}),
	} {
		t.Run(name, func(t *testing.T) {
			err := fixapply.Applier{}.Apply(ctx, nil, p)
			if !errors.Is(err, fixapply.ErrNoHumanActor) {
				t.Fatalf("err = %v, want ErrNoHumanActor", err)
			}
		})
	}
}

func TestDecodeDetail(t *testing.T) {
	want := &compliancev1.FixProposalDetail{ValidationRunId: uuid.NewString(), PayloadVersion: 3, RulesetVersion: "pint-ae@1.0.4+r1",
		ErrorsBefore: 4, ErrorsAfter: 1, ResolvedRuleIds: []string{"ibr-132-ae"},
		Notes: []*compliancev1.FixChangeNote{{Path: "currency", RuleIds: []string{"ibr-132-ae"}, Source: "deterministic", Rationale: "upper case"}}}
	a, err := anypb.New(want)
	if err != nil {
		t.Fatal(err)
	}
	p := fieldFix()
	p.DetailType, p.Detail = a.GetTypeUrl(), a.GetValue()
	got, err := fixapply.DecodeDetail(p)
	if err != nil || !proto.Equal(got, want) {
		t.Fatalf("got %v, %v", got, err)
	}
	if none, err := fixapply.DecodeDetail(fieldFix()); err != nil || none != nil {
		t.Fatalf("no detail: %v, %v", none, err)
	}
	p.DetailType = "type.googleapis.com/compliance.v1.Nope"
	if _, err := fixapply.DecodeDetail(p); !errors.Is(err, proposals.ErrBadProposal) {
		t.Fatalf("unknown type: %v", err)
	}
}

func TestAuditWriterRejectsAnUnknownDecision(t *testing.T) {
	err := fixapply.AuditWriter{}.Record(userCtx(), nil, fieldFix(), proposals.Decision("maybe"), "u-1", "")
	if err == nil || !strings.Contains(err.Error(), "decision") {
		t.Fatalf("err = %v", err)
	}
}
