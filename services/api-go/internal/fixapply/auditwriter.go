package fixapply

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

// protoUnmarshal keeps unknown fields out of the way of a newer agent writing a newer detail.
var protoUnmarshal = proto.UnmarshalOptions{DiscardUnknown: true}

// AuditWriter implements proposals.AuditWriter for every proposal kind: one proposal.accepted or
// proposal.rejected event per decision, entity "proposal", in the Decide transaction.
type AuditWriter struct{}

// Record writes the decision's audit event through q (the Decide transaction). actor is the deciding
// user's subject. The event carries the proposal's changes and, for an invoice target, the invoice id,
// so it shows in the invoice's audit trail.
func (AuditWriter) Record(ctx context.Context, q *sqlc.Queries, p proposals.Proposal, d proposals.Decision, actor, reason string) error {
	var action string
	switch d {
	case proposals.Accept:
		action = "proposal.accepted"
	case proposals.Reject:
		action = "proposal.rejected"
	default:
		return fmt.Errorf("fixapply: unknown decision %q: %w", d, proposals.ErrInvalidDecision)
	}
	proposalID := p.ID
	changes := make([]fieldpath.FieldChange, 0, len(p.Changes))
	for _, c := range p.Changes {
		changes = append(changes, fieldpath.FieldChange{Path: c.Path, OldValue: c.OldValue, NewValue: c.NewValue})
	}
	e := audit.Event{
		Actor:      audit.Actor{Type: "user", ID: actor, Agent: p.Agent, ProposalID: &proposalID},
		Action:     action,
		EntityType: "proposal",
		EntityID:   p.ID,
		Changes:    changes,
		Before:     map[string]any{"state": proposals.StateProposed},
		After:      map[string]any{"state": p.State, "kind": p.Kind},
		Reason:     reason,
	}
	if inv := TargetInvoice(p); inv != uuid.Nil {
		e.InvoiceID = &inv
	}
	if _, err := audit.Record(ctx, q, p.FirmID, e); err != nil {
		return fmt.Errorf("fixapply: audit %s: %w", action, err)
	}
	return nil
}
