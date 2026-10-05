package httpapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// fakeRunner is the validation.Service stand-in HandleExtracted talks to.
type fakeRunner struct {
	res   validation.RunResult
	err   error
	calls int
	firm  uuid.UUID
	id    uuid.UUID
	opts  validation.RunOpts
}

func (f *fakeRunner) Run(_ context.Context, firm, id uuid.UUID, o validation.RunOpts) (validation.RunResult, error) {
	f.calls++
	f.firm, f.id, f.opts = firm, id, o
	return f.res, f.err
}

func extracted(firm, id uuid.UUID) *compliancev1.InvoiceExtracted {
	return &compliancev1.InvoiceExtracted{InvoiceId: id.String(), FirmId: firm.String()}
}

func TestHandleExtractedRunsTheExtractedTrigger(t *testing.T) {
	firm, id := uuid.New(), uuid.New()
	r := &fakeRunner{res: validation.RunResult{Status: "validated"}}
	// The event carries no invoice copy at all: the adapter must not need it (AC-11, F4).
	if err := HandleExtracted(r)(context.Background(), extracted(firm, id)); err != nil {
		t.Fatalf("err = %v", err)
	}
	if r.calls != 1 || r.firm != firm || r.id != id || r.opts.Trigger != validation.TriggerExtracted || r.opts.Actor != "" || r.opts.RulesetVersion != "" {
		t.Errorf("runner call = %+v", r)
	}
}

func TestHandleExtractedIgnoresTheEventsInvoiceCopy(t *testing.T) {
	firm, id := uuid.New(), uuid.New()
	r := &fakeRunner{}
	ev := extracted(firm, id)
	ev.Invoice = &compliancev1.Invoice{InvoiceNumber: "STALE-COPY", SellerTrn: "bad"}
	if err := HandleExtracted(r)(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	// Run has no way to receive the copy: its signature takes only ids and options. A redelivery that
	// was skipped acks without error.
	r.res = validation.RunResult{Skipped: true}
	if err := HandleExtracted(r)(context.Background(), ev); err != nil {
		t.Errorf("skip must ack, got %v", err)
	}
}

func TestHandleExtractedBadIDsArePermanent(t *testing.T) {
	r := &fakeRunner{}
	good := uuid.NewString()
	for name, ev := range map[string]*compliancev1.InvoiceExtracted{
		"bad firm":    {FirmId: "nope", InvoiceId: good},
		"bad invoice": {FirmId: good, InvoiceId: "nope"},
		"empty":       {},
	} {
		if err := HandleExtracted(r)(context.Background(), ev); !errors.Is(err, events.ErrPermanent) {
			t.Errorf("%s: err = %v, want ErrPermanent", name, err)
		}
	}
	if r.calls != 0 {
		t.Errorf("runner called %d times for undecodable ids", r.calls)
	}
}

func TestHandleExtractedErrorClassification(t *testing.T) {
	firm, id := uuid.New(), uuid.New()
	run := func(err error) error {
		return HandleExtracted(&fakeRunner{err: err})(context.Background(), extracted(firm, id))
	}
	for name, e := range map[string]error{
		"permanent":      fmt.Errorf("%w: undecodable", validation.ErrPermanent),
		"not found":      fmt.Errorf("get: %w", db.ErrNotFound),
		"permanent+nf":   fmt.Errorf("%w: gone: %w", validation.ErrPermanent, db.ErrNotFound),
		"already wraped": fmt.Errorf("%w: x", events.ErrPermanent),
	} {
		got := run(e)
		if !errors.Is(got, events.ErrPermanent) {
			t.Errorf("%s: err = %v, want events.ErrPermanent", name, got)
		}
		if !errors.Is(got, e) {
			t.Errorf("%s: the cause is lost: %v", name, got)
		}
	}
	for name, e := range map[string]error{
		"validator down": errors.New("validate: unavailable"),
		"stale":          validation.ErrStale,
	} {
		got := run(e)
		if got == nil || errors.Is(got, events.ErrPermanent) {
			t.Errorf("%s: err = %v, want a retryable error", name, got)
		}
		if !errors.Is(got, e) {
			t.Errorf("%s: the cause is lost: %v", name, got)
		}
	}
}
