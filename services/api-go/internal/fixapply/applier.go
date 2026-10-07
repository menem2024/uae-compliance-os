// Package fixapply turns a person's decision on an agent's field-fix proposal into data: the Applier
// writes the accepted changes to the invoice (through invoicefix, the single write path), and the
// AuditWriter records the decision itself. Both run inside proposals.Decide's one transaction, so an
// accept is all or nothing: the invoice change, its audit event and the proposal state commit together.
//
// An agent only proposes. Nothing here runs without a signed-in user: the Applier refuses a context
// that does not carry one, and agent-forbidden paths (identifiers a human must type) fail even when a
// proposal asks for them.
package fixapply

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/anypb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
	"github.com/menem2024/uae-platform/services/api-go/internal/invoicefix"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

// KindFieldFix is the proposal kind of the Fix agent (agent-runtime contract section 5).
const KindFieldFix = "invoice.field_fix"

// ErrNoHumanActor: Apply ran without a signed-in user in its context (audit.WithActor). It is a wiring
// bug, never a client error, so it is not one of the proposals sentinel errors.
var ErrNoHumanActor = errors.New("fixapply: no human actor in context")

// Applier implements proposals.Applier for KindFieldFix.
type Applier struct{}

// Apply writes p.Changes to the invoice p.TargetID with expectedVersion 0 (any payload version): each
// change's old_value is the concurrency guard, so a proposal made against an older payload still
// applies when the fields it touches are unchanged. q must belong to the Decide transaction.
//
// Errors, all of which make Decide roll back and leave the proposal open:
//   - proposals.ErrBadProposal: not an invoice proposal, no (or a no-op) change, undecodable detail,
//     a path that breaks the grammar, or an agent-forbidden path (invoicefix.ErrForbiddenPath);
//   - proposals.ErrStale: an old_value no longer matches, or the invoice is gone (fieldpath.ErrOldValueMismatch);
//   - ErrNoHumanActor.
func (Applier) Apply(ctx context.Context, q *sqlc.Queries, p proposals.Proposal) error {
	if p.TargetType != "invoice" || len(p.Changes) == 0 {
		return fmt.Errorf("%w: %s proposal on %q with %d changes", proposals.ErrBadProposal, KindFieldFix, p.TargetType, len(p.Changes))
	}
	if _, err := DecodeDetail(p); err != nil {
		return err
	}
	changes := make([]fieldpath.FieldChange, 0, len(p.Changes))
	for _, c := range p.Changes {
		if c.OldValue == c.NewValue {
			return fmt.Errorf("%w: change at %q does not change anything", proposals.ErrBadProposal, c.Path)
		}
		changes = append(changes, fieldpath.FieldChange{Path: c.Path, OldValue: c.OldValue, NewValue: c.NewValue})
	}
	user, ok := audit.ActorFrom(ctx)
	if !ok || user.Type != "user" || user.ID == "" {
		return ErrNoHumanActor
	}
	proposalID := p.ID
	actor := audit.Actor{Type: "user", ID: user.ID, Agent: p.Agent, ProposalID: &proposalID}
	_, err := invoicefix.Apply(ctx, q, p.FirmID, p.TargetID, 0, changes, actor, true, "invoice.fields_changed", p.Rationale)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fieldpath.ErrOldValueMismatch), errors.Is(err, invoicefix.ErrStalePayload), errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: %w", proposals.ErrStale, err)
	case errors.Is(err, fieldpath.ErrBadPath), errors.Is(err, fieldpath.ErrNoChanges), errors.Is(err, invoicefix.ErrForbiddenPath):
		return fmt.Errorf("%w: %w", proposals.ErrBadProposal, err)
	default:
		return err
	}
}

// DecodeDetail unpacks the proposal's FixProposalDetail. It returns nil, nil when the proposal has no
// detail, and proposals.ErrBadProposal when the detail is not a decodable FixProposalDetail.
func DecodeDetail(p proposals.Proposal) (*compliancev1.FixProposalDetail, error) {
	if p.DetailType == "" && len(p.Detail) == 0 {
		return nil, nil
	}
	d := &compliancev1.FixProposalDetail{}
	if err := anypb.UnmarshalTo(&anypb.Any{TypeUrl: p.DetailType, Value: p.Detail}, d, protoUnmarshal); err != nil {
		return nil, fmt.Errorf("%w: detail: %w", proposals.ErrBadProposal, err)
	}
	return d, nil
}

// TargetInvoice returns the invoice a proposal is about (uuid.Nil for other targets).
func TargetInvoice(p proposals.Proposal) uuid.UUID {
	if p.TargetType != "invoice" {
		return uuid.Nil
	}
	return p.TargetID
}
