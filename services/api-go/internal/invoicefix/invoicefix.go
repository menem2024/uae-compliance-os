// Package invoicefix applies field changes to a stored invoice payload (spec §5.6.3): the single
// write path for human corrections and accepted agent proposals.
package invoicefix

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
)

var (
	// ErrStalePayload: the invoice's payload_version is not the version the caller saw.
	ErrStalePayload = errors.New("invoicefix: stale payload version")
	// ErrForbiddenPath: an agent-origin change touches a fieldpath.AgentForbidden path.
	ErrForbiddenPath = errors.New("invoicefix: path is forbidden for agents")
)

// Apply changes the invoice's payload. q must belong to a db.WithFirm transaction for firmID, so
// the row lock lives until the caller commits and RLS applies. Steps: reject agent-forbidden paths,
// lock the invoice row, check expectedVersion (0 = any), decode the payload, fieldpath.ApplyChanges,
// re-encode with proto names, store payload_version+1 with status "fixed" and the approval cleared,
// and audit.Record(action) for actor. It returns the new payload_version.
//
// Errors from fieldpath (ErrBadPath, ErrOldValueMismatch, ErrNoChanges) and pgx.ErrNoRows (invoice
// not found, hidden by RLS) are returned wrapped.
func Apply(ctx context.Context, q *sqlc.Queries, firmID, invoiceID uuid.UUID, expectedVersion int32,
	changes []fieldpath.FieldChange, actor audit.Actor, agentOrigin bool, action, reason string) (int32, error) {
	if !audit.ValidAction(action) {
		return 0, fmt.Errorf("%w: unknown action %q", audit.ErrInvalidEvent, action)
	}
	if agentOrigin {
		for _, c := range changes {
			if fieldpath.AgentForbidden(c.Path) {
				return 0, fmt.Errorf("%w: %s", ErrForbiddenPath, c.Path)
			}
		}
	}
	row, err := q.TrackCLockInvoice(ctx, invoiceID)
	if err != nil {
		return 0, fmt.Errorf("invoicefix: lock invoice: %w", err)
	}
	if expectedVersion != 0 && expectedVersion != row.PayloadVersion {
		return 0, fmt.Errorf("%w: invoice is at %d, caller saw %d", ErrStalePayload, row.PayloadVersion, expectedVersion)
	}
	var inv compliancev1.Invoice
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(row.Payload, &inv); err != nil {
		return 0, fmt.Errorf("invoicefix: decode payload: %w", err)
	}
	if err := fieldpath.ApplyChanges(&inv, changes); err != nil {
		return 0, err
	}
	payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(&inv)
	if err != nil {
		return 0, fmt.Errorf("invoicefix: encode payload: %w", err)
	}
	newVersion, err := q.TrackCApplyPayload(ctx, sqlc.TrackCApplyPayloadParams{
		Payload: payload, ID: invoiceID, ExpectedPayloadVersion: row.PayloadVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%w: payload changed under the lock", ErrStalePayload)
	}
	if err != nil {
		return 0, fmt.Errorf("invoicefix: store payload: %w", err)
	}
	_, err = audit.Record(ctx, q, firmID, audit.Event{
		Actor:      actor,
		Action:     action,
		EntityType: "invoice",
		EntityID:   invoiceID,
		InvoiceID:  &invoiceID,
		Changes:    changes,
		Before:     map[string]any{"payload_version": row.PayloadVersion, "status": row.Status},
		After:      map[string]any{"payload_version": newVersion, "status": "fixed"},
		Reason:     reason,
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}
