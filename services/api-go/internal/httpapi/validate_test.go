package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
)

type fakeValidator struct {
	run *compliancev1.ValidationRun
	err error
}

func (f fakeValidator) Validate(_ context.Context, _ *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&compliancev1.ValidateResponse{Run: f.run}), nil
}

type fakeValStore struct {
	err                    error
	firm, id               uuid.UUID
	status, rulesetVersion string
	issues                 []byte

	curStatus string // returned by Status
	statusErr error
}

func (f *fakeValStore) SetValidation(_ context.Context, firm, id uuid.UUID, status, rv string, issues []byte) error {
	f.firm, f.id, f.status, f.rulesetVersion, f.issues = firm, id, status, rv, issues
	return f.err
}

func (f *fakeValStore) Status(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return f.curStatus, f.statusErr
}

// countingValidator wraps fakeValidator to record how many times Validate
// was called, so tests can assert a skip never reached the validator.
type countingValidator struct {
	fakeValidator
	calls int
}

func (v *countingValidator) Validate(ctx context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	v.calls++
	return v.fakeValidator.Validate(ctx, req)
}

func extracted(firm, id uuid.UUID) *compliancev1.InvoiceExtracted {
	return &compliancev1.InvoiceExtracted{InvoiceId: id.String(), FirmId: firm.String(), Invoice: &compliancev1.Invoice{}}
}

func TestHandleExtracted(t *testing.T) {
	firm, id := uuid.New(), uuid.New()
	ctx := context.Background()

	withIssues := fakeValidator{run: &compliancev1.ValidationRun{RulesetVersion: "pint-ae@0.0-skeleton", Issues: []*compliancev1.ValidationIssue{
		{RuleId: "AE-TRN-001", Severity: compliancev1.Severity_SEVERITY_ERROR, Path: "seller_trn", Message: "bad"},
		{RuleId: "X", Severity: compliancev1.Severity_SEVERITY_WARNING, Path: "p", Message: "m"},
	}}}
	st := &fakeValStore{}
	if err := HandleExtracted(st, withIssues)(ctx, extracted(firm, id)); err != nil {
		t.Fatal(err)
	}
	var issues []IssueView
	_ = json.Unmarshal(st.issues, &issues)
	if st.firm != firm || st.id != id || st.status != db.StatusHasIssues || st.rulesetVersion != "pint-ae@0.0-skeleton" ||
		len(issues) != 2 || issues[0].Severity != "error" || issues[1].Severity != "warning" || issues[0].RuleID != "AE-TRN-001" {
		t.Errorf("stored %+v issues=%s", st, st.issues)
	}

	st = &fakeValStore{}
	clean := fakeValidator{run: &compliancev1.ValidationRun{RulesetVersion: "pint-ae@0.0-skeleton"}}
	if err := HandleExtracted(st, clean)(ctx, extracted(firm, id)); err != nil || st.status != db.StatusValidated || string(st.issues) != "[]" {
		t.Errorf("clean: status=%s issues=%s err=%v", st.status, st.issues, err)
	}

	// Hidden/missing row: permanent, so the consumer dead-letters it.
	err := HandleExtracted(&fakeValStore{err: db.ErrNotFound}, clean)(ctx, extracted(firm, id))
	if !errors.Is(err, events.ErrPermanent) || !errors.Is(err, db.ErrNotFound) {
		t.Errorf("not found: %v", err)
	}
	// Validator unavailable: transient.
	err = HandleExtracted(&fakeValStore{}, fakeValidator{err: connect.NewError(connect.CodeUnavailable, errors.New("down"))})(ctx, extracted(firm, id))
	if err == nil || errors.Is(err, events.ErrPermanent) {
		t.Errorf("unavailable should be transient: %v", err)
	}
	// Garbage ids: permanent.
	bad := &compliancev1.InvoiceExtracted{InvoiceId: "x", FirmId: "y"}
	if err := HandleExtracted(&fakeValStore{}, clean)(ctx, bad); !errors.Is(err, events.ErrPermanent) {
		t.Errorf("bad ids: %v", err)
	}
}

// TestHandleExtractedSkipsAlreadyTerminalInvoice covers redelivery after the
// api-validation durable is recreated: the retained INVOICES stream replays,
// including invoices already validated. Re-validating them is wasted work
// (and, for a dead-lettered permanent failure, would re-publish to the DLQ),
// so an invoice already in a terminal status is acked without calling the
// validator again.
func TestHandleExtractedSkipsAlreadyTerminalInvoice(t *testing.T) {
	firm, id := uuid.New(), uuid.New()
	ctx := context.Background()
	for _, status := range []string{db.StatusValidated, db.StatusHasIssues} {
		st := &fakeValStore{curStatus: status}
		v := &countingValidator{}
		if err := HandleExtracted(st, v)(ctx, extracted(firm, id)); err != nil {
			t.Errorf("status=%s: %v", status, err)
		}
		if v.calls != 0 {
			t.Errorf("status=%s: validator called %d times, want 0", status, v.calls)
		}
		if st.firm != uuid.Nil || st.id != uuid.Nil {
			t.Errorf("status=%s: SetValidation called unexpectedly", status)
		}
	}
}

// TestHandleExtractedValidatesWhenNotTerminal is the control: a non-terminal
// (or unknown) status still runs validation normally.
func TestHandleExtractedValidatesWhenNotTerminal(t *testing.T) {
	firm, id := uuid.New(), uuid.New()
	ctx := context.Background()
	for _, status := range []string{db.StatusUploaded, db.StatusExtracted, ""} {
		st := &fakeValStore{curStatus: status}
		v := &countingValidator{fakeValidator: fakeValidator{run: &compliancev1.ValidationRun{RulesetVersion: "pint-ae@0.0-skeleton"}}}
		if err := HandleExtracted(st, v)(ctx, extracted(firm, id)); err != nil {
			t.Errorf("status=%q: %v", status, err)
		}
		if v.calls != 1 {
			t.Errorf("status=%q: validator called %d times, want 1", status, v.calls)
		}
		if st.status != db.StatusValidated {
			t.Errorf("status=%q: SetValidation not called with the outcome", status)
		}
	}
}

// TestHandleExtractedProceedsWhenStatusLookupNotFound preserves existing
// behaviour: if the status check itself finds nothing (e.g. RLS hides the
// row), fall through to the normal flow, which fails permanently via
// SetValidation exactly as before this change.
func TestHandleExtractedProceedsWhenStatusLookupNotFound(t *testing.T) {
	firm, id := uuid.New(), uuid.New()
	st := &fakeValStore{statusErr: db.ErrNotFound, err: db.ErrNotFound}
	v := &countingValidator{fakeValidator: fakeValidator{run: &compliancev1.ValidationRun{RulesetVersion: "x"}}}
	err := HandleExtracted(st, v)(context.Background(), extracted(firm, id))
	if !errors.Is(err, events.ErrPermanent) || !errors.Is(err, db.ErrNotFound) {
		t.Errorf("got %v", err)
	}
	if v.calls != 1 {
		t.Errorf("validator called %d times, want 1", v.calls)
	}
}

// TestHandleExtractedStatusLookupTransientErrorRetries: a transient failure
// checking status (e.g. the DB is briefly unavailable) must not skip or
// dead-letter the message; it should be retried like any other transient
// failure, without wasting a validator call first.
func TestHandleExtractedStatusLookupTransientErrorRetries(t *testing.T) {
	firm, id := uuid.New(), uuid.New()
	st := &fakeValStore{statusErr: errors.New("db down")}
	v := &countingValidator{}
	err := HandleExtracted(st, v)(context.Background(), extracted(firm, id))
	if err == nil || errors.Is(err, events.ErrPermanent) {
		t.Errorf("status lookup error should be transient: %v", err)
	}
	if v.calls != 0 {
		t.Errorf("validator called despite status lookup failure: %d", v.calls)
	}
}
