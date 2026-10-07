// Package validation runs an invoice through validator-rs, stores the outcome as an append-only run
// with its issues, and owns the invoice status state machine (spec §5.6.1). It validates the STORED
// payload only: a redelivered invoice.extracted never overwrites a corrected invoice.
package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/protojson"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// Trigger says why a run happened. The values equal the validation_runs.trigger CHECK list.
type Trigger string

const (
	TriggerExtracted      Trigger = "extracted"
	TriggerManual         Trigger = "manual"
	TriggerFixAccepted    Trigger = "fix_accepted"
	TriggerCorrection     Trigger = "correction"
	TriggerSweeper        Trigger = "sweeper"
	TriggerRulesetUpgrade Trigger = "ruleset_upgrade"
)

// Triggers returns every valid trigger.
func Triggers() []Trigger {
	return []Trigger{TriggerExtracted, TriggerManual, TriggerFixAccepted, TriggerCorrection, TriggerSweeper, TriggerRulesetUpgrade}
}

func (t Trigger) valid() bool {
	for _, x := range Triggers() {
		if x == t {
			return true
		}
	}
	return false
}

// system reports whether the trigger is an automatic one: it may not take an invoice out of
// needs_review, only a human may.
func (t Trigger) system() bool {
	return t == TriggerExtracted || t == TriggerSweeper || t == TriggerRulesetUpgrade
}

// RunOpts configures one Run.
type RunOpts struct {
	Trigger        Trigger
	Actor          string // auth.Principal.Subject for manual/correction/fix_accepted; "" for system triggers
	RulesetVersion string // "" = validator default
}

// RunResult is the outcome of one Run.
type RunResult struct {
	Skipped        bool // extracted trigger and the invoice already has a run
	RunID          uuid.UUID
	PayloadVersion int32
	RulesetVersion string
	Status         string
	Errors         int
	Warnings       int
	FixableErrors  int
	Issues         []*compliancev1.ValidationIssue
}

var (
	// ErrPermanent: redelivery cannot help (payload undecodable, INVALID_ARGUMENT, invoice missing).
	ErrPermanent = errors.New("validation: permanent failure")
	// ErrStale: the payload kept changing during validation.
	ErrStale = errors.New("validation: payload changed during validation")
)

// maxAttempts bounds the optimistic-lock restarts of one Run.
const maxAttempts = 3

// Validator is the subset of compliancev1connect.ValidatorServiceClient used here.
type Validator interface {
	Validate(ctx context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error)
}

// Service validates invoices.
type Service struct {
	Pool      *pgxpool.Pool
	Validator Validator
	// OnRun, when set, is called after each committed (not skipped) run, e.g. to trigger the Fix
	// agent. Its failures are its own business.
	OnRun func(ctx context.Context, firmID, invoiceID uuid.UUID, r RunResult)
}

// NextStatus is the whole status state machine (spec §5.6.1).
//
//	system trigger, needs_review          -> needs_review (only a human leaves it)
//	errors > 0                            -> has_issues
//	ready, no errors, approval matches    -> ready
//	otherwise (warnings never block)      -> validated
func NextStatus(current string, t Trigger, errors int, approvalMatches bool) string {
	switch {
	case current == "needs_review" && t.system():
		return "needs_review"
	case errors > 0:
		return "has_issues"
	case current == "ready" && approvalMatches:
		return "ready"
	default:
		return "validated"
	}
}

// legacyIssue is the Phase 0 IssueView shape kept in the invoices.issues column so that
// GET /v1/invoices/{id} and the demo keep working.
type legacyIssue struct {
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

func severityName(s compliancev1.Severity) (string, bool) {
	switch s {
	case compliancev1.Severity_SEVERITY_ERROR:
		return "error", true
	case compliancev1.Severity_SEVERITY_WARNING:
		return "warning", true
	default:
		return "", false
	}
}

// Run validates the stored payload of an invoice and records the outcome. See the package doc.
func (s *Service) Run(ctx context.Context, firmID, invoiceID uuid.UUID, o RunOpts) (RunResult, error) {
	if !o.Trigger.valid() {
		return RunResult{}, fmt.Errorf("%w: unknown trigger %q", ErrPermanent, o.Trigger)
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, drifted, err := s.attempt(ctx, firmID, invoiceID, o)
		if err != nil {
			return RunResult{}, err
		}
		if drifted {
			slog.InfoContext(ctx, "payload changed during validation, restarting",
				"invoice_id", invoiceID, "firm_id", firmID, "attempt", attempt)
			continue
		}
		if !res.Skipped && s.OnRun != nil {
			s.OnRun(ctx, firmID, invoiceID, res)
		}
		return res, nil
	}
	return RunResult{}, fmt.Errorf("%w: invoice %s after %d attempts", ErrStale, invoiceID, maxAttempts)
}

// attempt is one pass over steps 1-4. drifted reports that the payload version changed between the
// read and the lock; the result was abandoned and nothing was stored.
func (s *Service) attempt(ctx context.Context, firmID, invoiceID uuid.UUID, o RunOpts) (res RunResult, drifted bool, err error) {
	// Step 1: read.
	var inv sqlc.TrackCGetInvoiceRow
	err = db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		var qerr error
		inv, qerr = q.TrackCGetInvoice(ctx, invoiceID)
		return qerr
	})
	if errors.Is(err, db.ErrNotFound) {
		return RunResult{}, false, fmt.Errorf("%w: invoice %s not visible to firm %s: %w", ErrPermanent, invoiceID, firmID, err)
	}
	if err != nil {
		return RunResult{}, false, fmt.Errorf("read invoice %s: %w", invoiceID, err)
	}
	if o.Trigger == TriggerExtracted && inv.LatestRunID != uuid.Nil {
		slog.InfoContext(ctx, "invoice already validated, skipping redelivered invoice.extracted",
			"invoice_id", invoiceID, "firm_id", firmID, "status", inv.Status)
		return RunResult{Skipped: true, RunID: inv.LatestRunID, PayloadVersion: inv.PayloadVersion, Status: inv.Status,
			RulesetVersion: inv.RulesetVersion.String}, false, nil
	}

	// Step 2: decode. Unknown fields are an error (CI §11).
	var invoice compliancev1.Invoice
	if err := (protojson.UnmarshalOptions{}).Unmarshal(inv.Payload, &invoice); err != nil {
		return RunResult{}, false, fmt.Errorf("%w: decode payload of %s: %w", ErrPermanent, invoiceID, err)
	}

	// Step 3: validate, outside any transaction.
	resp, err := s.Validator.Validate(ctx, connect.NewRequest(&compliancev1.ValidateRequest{
		Invoice: &invoice, RulesetVersion: o.RulesetVersion,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeInvalidArgument {
			return RunResult{}, false, fmt.Errorf("%w: validate %s: %w", ErrPermanent, invoiceID, err)
		}
		return RunResult{}, false, fmt.Errorf("validate %s: %w", invoiceID, err)
	}
	run := resp.Msg.GetRun()
	if run == nil {
		return RunResult{}, false, fmt.Errorf("validate %s: empty validation run", invoiceID)
	}
	st, err := summarise(run)
	if err != nil {
		return RunResult{}, false, fmt.Errorf("%w: validate %s: %w", ErrPermanent, invoiceID, err)
	}

	// Step 4: store, under the row lock.
	err = db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		locked, err := q.TrackCLockInvoice(ctx, invoiceID)
		if err != nil {
			return err
		}
		if locked.PayloadVersion != inv.PayloadVersion {
			drifted = true
			return nil
		}
		if o.Trigger == TriggerExtracted && locked.LatestRunID != uuid.Nil {
			// A concurrent delivery validated it first.
			res = RunResult{Skipped: true, RunID: locked.LatestRunID, PayloadVersion: locked.PayloadVersion,
				Status: locked.Status, RulesetVersion: locked.RulesetVersion.String}
			return nil
		}
		approvalMatches := locked.ApprovedPayloadVersion.Valid && locked.ApprovedPayloadVersion.Int32 == locked.PayloadVersion
		next := NextStatus(locked.Status, o.Trigger, st.errors, approvalMatches)

		var traceID string
		if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
			traceID = sc.TraceID().String()
		}
		row, err := q.TrackCInsertRun(ctx, sqlc.TrackCInsertRunParams{
			FirmID: firmID, InvoiceID: invoiceID, PayloadVersion: locked.PayloadVersion,
			RulesetVersion: run.GetRulesetVersion(), Trigger: string(o.Trigger),
			ErrorCount: int32(st.errors), WarningCount: int32(st.warnings), //nolint:gosec // bounded by the issue count
			RulesEvaluated: run.GetRulesEvaluated(), DurationUs: run.GetDurationUs(),
			RequestedBy: o.Actor, TraceID: traceID,
		})
		if err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		if len(st.params.Seqs) > 0 {
			st.params.FirmID, st.params.RunID, st.params.InvoiceID = firmID, row.ID, invoiceID
			if err := q.TrackCInsertIssues(ctx, st.params); err != nil {
				return fmt.Errorf("insert issues: %w", err)
			}
		}
		n, err := q.TrackCSetValidationResult(ctx, sqlc.TrackCSetValidationResultParams{
			Status: next, RunID: row.ID, RulesetVersion: run.GetRulesetVersion(), Issues: st.legacy,
			ID: invoiceID, PayloadVersion: locked.PayloadVersion,
		})
		if err != nil {
			return fmt.Errorf("set validation result: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("set validation result: updated %d rows for a locked invoice", n)
		}
		if locked.Status == "ready" && next != "ready" {
			pv := locked.PayloadVersion
			if _, err := audit.Record(ctx, q, firmID, audit.Event{
				Actor:      audit.Actor{Type: "system", ID: "api-go"},
				Action:     "invoice.approval_revoked",
				EntityType: "invoice", EntityID: invoiceID, InvoiceID: &invoiceID,
				Before: map[string]any{"status": "ready", "payload_version": pv, "approved_payload_version": locked.ApprovedPayloadVersion.Int32},
				After:  map[string]any{"status": next, "payload_version": pv, "run_id": row.ID.String()},
				Reason: fmt.Sprintf("validation run (%s) left ready: %d errors", o.Trigger, st.errors),
			}); err != nil {
				return err
			}
		}
		res = RunResult{
			RunID: row.ID, PayloadVersion: locked.PayloadVersion, RulesetVersion: run.GetRulesetVersion(), Status: next,
			Errors: st.errors, Warnings: st.warnings, FixableErrors: st.fixable, Issues: run.GetIssues(),
		}
		return nil
	})
	if errors.Is(err, db.ErrNotFound) {
		return RunResult{}, false, fmt.Errorf("%w: invoice %s vanished: %w", ErrPermanent, invoiceID, err)
	}
	if err != nil {
		return RunResult{}, false, fmt.Errorf("store validation of %s: %w", invoiceID, err)
	}
	if !drifted && !res.Skipped {
		slog.InfoContext(ctx, "invoice validated", "invoice_id", invoiceID, "firm_id", firmID, "status", res.Status,
			"trigger", string(o.Trigger), "ruleset_version", res.RulesetVersion, "errors", res.Errors, "warnings", res.Warnings)
	}
	return res, drifted, nil
}

type summary struct {
	errors, warnings, fixable int
	params                    sqlc.TrackCInsertIssuesParams
	legacy                    []byte
}

// summarise counts a run's issues and prepares the parallel arrays of TrackCInsertIssues and the
// legacy issues JSON.
func summarise(run *compliancev1.ValidationRun) (summary, error) {
	var s summary
	legacy := make([]legacyIssue, 0, len(run.GetIssues()))
	for i, is := range run.GetIssues() {
		sev, ok := severityName(is.GetSeverity())
		if !ok {
			return summary{}, fmt.Errorf("issue %d (%s) has no severity", i, is.GetRuleId())
		}
		if sev == "error" {
			s.errors++
			if is.GetFixable() {
				s.fixable++
			}
		} else {
			s.warnings++
		}
		args := is.GetMessageArgs()
		if args == nil {
			args = map[string]string{}
		}
		rawArgs, err := json.Marshal(args)
		if err != nil {
			return summary{}, fmt.Errorf("issue %d: encode message_args: %w", i, err)
		}
		p := &s.params
		p.Seqs = append(p.Seqs, int32(i)) //nolint:gosec // issue counts are far below int32
		p.RuleIds = append(p.RuleIds, is.GetRuleId())
		p.Severities = append(p.Severities, sev)
		p.Paths = append(p.Paths, is.GetPath())
		p.BusinessTerms = append(p.BusinessTerms, is.GetBusinessTerm())
		p.Messages = append(p.Messages, is.GetMessage())
		p.MessagesAr = append(p.MessagesAr, is.GetMessageAr())
		p.MessageArgs = append(p.MessageArgs, string(rawArgs))
		p.Fixables = append(p.Fixables, is.GetFixable())
		p.SuggestedValues = append(p.SuggestedValues, is.GetSuggestedValue())
		legacy = append(legacy, legacyIssue{RuleID: is.GetRuleId(), Severity: sev, Path: is.GetPath(), Message: is.GetMessage()})
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		return summary{}, fmt.Errorf("encode legacy issues: %w", err)
	}
	s.legacy = raw
	return s, nil
}
