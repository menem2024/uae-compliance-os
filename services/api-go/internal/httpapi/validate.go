package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
)

// ValidationStore persists a validation outcome. It returns db.ErrNotFound
// when the invoice is missing or hidden by RLS.
type ValidationStore interface {
	SetValidation(ctx context.Context, firmID, id uuid.UUID, status, rulesetVersion string, issues []byte) error
}

// ValidatorClient is the subset of compliancev1connect.ValidatorServiceClient used here.
type ValidatorClient interface {
	Validate(ctx context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error)
}

// HandleExtracted validates an extracted invoice via validator-rs and stores
// the outcome. Failures redelivery cannot fix wrap events.ErrPermanent.
func HandleExtracted(store ValidationStore, vc ValidatorClient) events.ExtractedHandler {
	return func(ctx context.Context, ev *compliancev1.InvoiceExtracted) error {
		firmID, err := uuid.Parse(ev.GetFirmId())
		if err != nil {
			return fmt.Errorf("%w: firm_id %q: %w", events.ErrPermanent, ev.GetFirmId(), err)
		}
		id, err := uuid.Parse(ev.GetInvoiceId())
		if err != nil {
			return fmt.Errorf("%w: invoice_id %q: %w", events.ErrPermanent, ev.GetInvoiceId(), err)
		}
		resp, err := vc.Validate(ctx, connect.NewRequest(&compliancev1.ValidateRequest{Invoice: ev.GetInvoice()}))
		if err != nil {
			if connect.CodeOf(err) == connect.CodeInvalidArgument {
				return fmt.Errorf("%w: validate %s: %w", events.ErrPermanent, id, err)
			}
			return fmt.Errorf("validate %s: %w", id, err)
		}
		run := resp.Msg.GetRun()
		if run == nil {
			return fmt.Errorf("validate %s: empty validation run", id)
		}
		issues := make([]IssueView, 0, len(run.GetIssues()))
		for _, is := range run.GetIssues() {
			issues = append(issues, IssueView{RuleID: is.GetRuleId(), Severity: severityName(is.GetSeverity()), Path: is.GetPath(), Message: is.GetMessage()})
		}
		raw, err := json.Marshal(issues)
		if err != nil {
			return fmt.Errorf("marshal issues: %w", err)
		}
		status := db.StatusValidated
		if len(issues) > 0 {
			status = db.StatusHasIssues
		}
		if err := store.SetValidation(ctx, firmID, id, status, run.GetRulesetVersion(), raw); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return fmt.Errorf("%w: invoice %s not visible to firm %s: %w", events.ErrPermanent, id, firmID, err)
			}
			return fmt.Errorf("set validation %s: %w", id, err)
		}
		slog.InfoContext(ctx, "invoice validated", "invoice_id", id, "firm_id", firmID, "status", status,
			"ruleset_version", run.GetRulesetVersion(), "issues", len(issues))
		return nil
	}
}

func severityName(s compliancev1.Severity) string {
	switch s {
	case compliancev1.Severity_SEVERITY_ERROR:
		return "error"
	case compliancev1.Severity_SEVERITY_WARNING:
		return "warning"
	default:
		return "unspecified"
	}
}

// SetValidation stores the outcome through db.WithFirm (RLS applies).
func (s PGStore) SetValidation(ctx context.Context, firmID, id uuid.UUID, status, rulesetVersion string, issues []byte) error {
	return db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		if _, err := q.SetValidation(ctx, sqlc.SetValidationParams{
			ID:             id,
			Status:         status,
			RulesetVersion: pgtype.Text{String: rulesetVersion, Valid: rulesetVersion != ""},
			Issues:         issues,
		}); err != nil {
			return fmt.Errorf("set validation: %w", err)
		}
		return nil
	})
}
