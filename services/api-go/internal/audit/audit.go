// Package audit writes the append-only audit_events log (spec §5.6.3). Who changed, approved or
// exported what is an AuditEvent; validation runs are their own log and are not audit events.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
)

// Actor is who did it.
type Actor struct {
	Type       string     // "user" | "agent" | "system"
	ID         string     // user Subject, agent name, or "api-go"
	Agent      string     // originating agent for agent-proposed changes ("" otherwise)
	ProposalID *uuid.UUID // the Approval this event belongs to
}

// Event is one audit entry.
type Event struct {
	Actor         Actor
	Action        string // closed list, see Actions
	EntityType    string // "invoice" | "proposal" | "export"
	EntityID      uuid.UUID
	InvoiceID     *uuid.UUID
	Changes       []fieldpath.FieldChange // stored as a JSON array; may be empty
	Before, After map[string]any          // small state snapshots, never payload copies
	Reason        string
}

// ErrInvalidEvent: the event breaks the closed lists or misses a required field; nothing was written.
var ErrInvalidEvent = errors.New("audit: invalid event")

var actions = []string{
	"invoice.fields_changed",
	"invoice.validation_requested",
	"invoice.approved",
	"invoice.approval_revoked",
	"invoice.exported",
	"invoice.fix_requested",
	"proposal.accepted",
	"proposal.rejected",
}

// Actions returns the closed list of audit actions (a copy).
func Actions() []string { return append([]string(nil), actions...) }

// ValidAction reports whether a is one of Actions.
func ValidAction(a string) bool {
	for _, x := range actions {
		if x == a {
			return true
		}
	}
	return false
}

func validEntity(t string) bool { return t == "invoice" || t == "proposal" || t == "export" }

func validActorType(t string) bool { return t == "user" || t == "agent" || t == "system" }

// Record inserts e for firmID through q, which must run inside a db.WithFirm transaction for that
// Firm (RLS), and returns the new event's id. It stores the active span's trace id.
func Record(ctx context.Context, q *sqlc.Queries, firmID uuid.UUID, e Event) (uuid.UUID, error) {
	switch {
	case firmID == uuid.Nil:
		return uuid.Nil, fmt.Errorf("%w: nil firm id", ErrInvalidEvent)
	case !ValidAction(e.Action):
		return uuid.Nil, fmt.Errorf("%w: unknown action %q", ErrInvalidEvent, e.Action)
	case !validEntity(e.EntityType):
		return uuid.Nil, fmt.Errorf("%w: unknown entity type %q", ErrInvalidEvent, e.EntityType)
	case !validActorType(e.Actor.Type):
		return uuid.Nil, fmt.Errorf("%w: unknown actor type %q", ErrInvalidEvent, e.Actor.Type)
	case e.Actor.ID == "":
		return uuid.Nil, fmt.Errorf("%w: empty actor id", ErrInvalidEvent)
	case e.EntityID == uuid.Nil:
		return uuid.Nil, fmt.Errorf("%w: nil entity id", ErrInvalidEvent)
	}
	changes := e.Changes
	if changes == nil {
		changes = []fieldpath.FieldChange{}
	}
	changesJSON, err := json.Marshal(changes)
	if err != nil {
		return uuid.Nil, fmt.Errorf("audit: encode changes: %w", err)
	}
	before, err := snapshot(e.Before)
	if err != nil {
		return uuid.Nil, err
	}
	after, err := snapshot(e.After)
	if err != nil {
		return uuid.Nil, err
	}
	arg := sqlc.TrackCInsertAuditEventParams{
		FirmID:     firmID,
		ActorType:  e.Actor.Type,
		ActorID:    e.Actor.ID,
		Agent:      e.Actor.Agent,
		Action:     e.Action,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
		Changes:    changesJSON,
		Before:     before,
		After:      after,
		Reason:     e.Reason,
	}
	if e.Actor.ProposalID != nil {
		arg.ProposalID = *e.Actor.ProposalID
	}
	if e.InvoiceID != nil {
		arg.InvoiceID = *e.InvoiceID
	}
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		arg.TraceID = sc.TraceID().String()
	}
	row, err := q.TrackCInsertAuditEvent(ctx, arg)
	if err != nil {
		return uuid.Nil, fmt.Errorf("audit: insert %s: %w", e.Action, err)
	}
	return row.ID, nil
}

// snapshot encodes a state snapshot; nil stays SQL NULL.
func snapshot(m map[string]any) ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("audit: encode snapshot: %w", err)
	}
	return b, nil
}

type actorKey struct{}

// WithActor returns ctx carrying the acting user (set by the HTTP layer, read by appliers).
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor stored by WithActor.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}
