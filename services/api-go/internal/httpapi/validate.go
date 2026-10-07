package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// Runner validates the stored payload of an invoice; *validation.Service implements it.
type Runner interface {
	Run(ctx context.Context, firmID, invoiceID uuid.UUID, o validation.RunOpts) (validation.RunResult, error)
}

// HandleExtracted turns invoice.extracted into a validation run with trigger "extracted". It
// validates the STORED payload and never reads the event's own invoice copy, so a redelivery cannot
// overwrite a corrected invoice (the service skips an invoice that already has a run, and a skip
// acks). Failures redelivery cannot fix wrap events.ErrPermanent.
func HandleExtracted(r Runner) events.ExtractedHandler {
	return func(ctx context.Context, ev *compliancev1.InvoiceExtracted) error {
		firmID, err := uuid.Parse(ev.GetFirmId())
		if err != nil {
			return fmt.Errorf("%w: firm_id %q: %w", events.ErrPermanent, ev.GetFirmId(), err)
		}
		id, err := uuid.Parse(ev.GetInvoiceId())
		if err != nil {
			return fmt.Errorf("%w: invoice_id %q: %w", events.ErrPermanent, ev.GetInvoiceId(), err)
		}
		if _, err := r.Run(ctx, firmID, id, validation.RunOpts{Trigger: validation.TriggerExtracted}); err != nil {
			switch {
			case errors.Is(err, events.ErrPermanent):
				return err
			case errors.Is(err, validation.ErrPermanent), errors.Is(err, db.ErrNotFound):
				return fmt.Errorf("%w: validate %s: %w", events.ErrPermanent, id, err)
			default:
				return fmt.Errorf("validate %s: %w", id, err)
			}
		}
		return nil
	}
}
