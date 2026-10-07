//go:build integration

package validation_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

const payload = `{"invoice_number":"INV-1","issue_date":"2026-09-27","seller_trn":"100000000000003","currency":"AED","total_amount":"1050.00","vat_amount":"50.00"}`

// fakeValidator answers Validate from a function and records every request.
type fakeValidator struct {
	mu    sync.Mutex
	calls []*compliancev1.ValidateRequest
	fn    func(n int, req *compliancev1.ValidateRequest) (*compliancev1.ValidationRun, error)
}

func (f *fakeValidator) Validate(_ context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	f.mu.Lock()
	f.calls = append(f.calls, req.Msg)
	n := len(f.calls)
	f.mu.Unlock()
	run, err := f.fn(n, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&compliancev1.ValidateResponse{Run: run}), nil
}

func (f *fakeValidator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func clean(string) *compliancev1.ValidationRun {
	return &compliancev1.ValidationRun{RulesetVersion: "pint-ae@1.0.4+r1", RulesEvaluated: 300, DurationUs: 120}
}

func cleanValidator() *fakeValidator {
	return &fakeValidator{fn: func(int, *compliancev1.ValidateRequest) (*compliancev1.ValidationRun, error) { return clean(""), nil }}
}

func issue(rule string, sev compliancev1.Severity, path string, fixable bool) *compliancev1.ValidationIssue {
	return &compliancev1.ValidationIssue{RuleId: rule, Severity: sev, Path: path, Message: "m " + rule, BusinessTerm: "BT-1",
		MessageArgs: map[string]string{"value": "x"}, Fixable: fixable, MessageAr: "ر", SuggestedValue: "S"}
}

func withIssues(is ...*compliancev1.ValidationIssue) *fakeValidator {
	return &fakeValidator{fn: func(int, *compliancev1.ValidateRequest) (*compliancev1.ValidationRun, error) {
		return &compliancev1.ValidationRun{RulesetVersion: "pint-ae@1.0.4+r1", Issues: is, RulesEvaluated: 300, DurationUs: 90}, nil
	}}
}

const (
	sevErr  = compliancev1.Severity_SEVERITY_ERROR
	sevWarn = compliancev1.Severity_SEVERITY_WARNING
)

func getInvoice(t *testing.T, env trackctest.Env, firm, id uuid.UUID) sqlc.TrackCGetInvoiceRow {
	t.Helper()
	var row sqlc.TrackCGetInvoiceRow
	if err := db.WithFirm(context.Background(), env.App, firm, func(q *sqlc.Queries) error {
		var err error
		row, err = q.TrackCGetInvoice(context.Background(), id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return row
}

func countRows(t *testing.T, env trackctest.Env, firm uuid.UUID, query string, args ...any) int {
	t.Helper()
	var n int
	if err := env.AppTx(context.Background(), firm, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), query, args...).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func setStatus(t *testing.T, env trackctest.Env, firm, id uuid.UUID, status string) {
	t.Helper()
	if err := env.AppTx(context.Background(), firm, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE invoices SET status=$2 WHERE id=$1`, id, status)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func approve(t *testing.T, env trackctest.Env, firm, id uuid.UUID, version int32) {
	t.Helper()
	if err := db.WithFirm(context.Background(), env.App, firm, func(q *sqlc.Queries) error {
		_, err := q.TrackCApproveInvoice(context.Background(), sqlc.TrackCApproveInvoiceParams{ID: id, ApprovedBy: "u1", PayloadVersion: version})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunCleanInvoiceBecomesValidated(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	id := env.SeedInvoice(t, env.FirmA, payload)
	v := cleanValidator()
	svc := &validation.Service{Pool: env.App, Validator: v}

	res, err := svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerExtracted})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Status != "validated" || res.Errors != 0 || res.Warnings != 0 || res.PayloadVersion != 1 ||
		res.RulesetVersion != "pint-ae@1.0.4+r1" || res.RunID == uuid.Nil {
		t.Fatalf("result = %+v", res)
	}
	inv := getInvoice(t, env, env.FirmA, id)
	if inv.Status != "validated" || inv.LatestRunID != res.RunID || inv.RulesetVersion.String != "pint-ae@1.0.4+r1" {
		t.Fatalf("invoice = %+v", inv)
	}
	// The validator saw the STORED payload.
	if got := v.calls[0].GetInvoice().GetInvoiceNumber(); got != "INV-1" || v.calls[0].GetRulesetVersion() != "" {
		t.Errorf("request = %v", v.calls[0])
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1 AND trigger='extracted' AND rules_evaluated=300`, id); n != 1 {
		t.Errorf("runs = %d", n)
	}
	// Phase 0 compatibility: the legacy issues column is an empty JSON array.
	var legacy string
	_ = env.AppTx(ctx, env.FirmA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT issues::text FROM invoices WHERE id=$1`, id).Scan(&legacy)
	})
	if legacy != "[]" {
		t.Errorf("legacy issues = %q", legacy)
	}
}

func TestRunWithIssues(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	id := env.SeedInvoice(t, env.FirmA, payload)
	v := withIssues(
		issue("ibr-132-ae", sevErr, "seller.tax_registration_number", true),
		issue("AE-FMT-001", sevErr, "buyer.vat_id", false),
		issue("ibr-cl-04", sevWarn, "currency", true), // a fixable warning is not a fixable error
	)
	svc := &validation.Service{Pool: env.App, Validator: v}
	res, err := svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual, Actor: "user-1", RulesetVersion: "pint-ae@1.0.4+r1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "has_issues" || res.Errors != 2 || res.Warnings != 1 || res.FixableErrors != 1 || len(res.Issues) != 3 {
		t.Fatalf("result = %+v", res)
	}
	if v.calls[0].GetRulesetVersion() != "pint-ae@1.0.4+r1" {
		t.Errorf("ruleset_version not forwarded: %q", v.calls[0].GetRulesetVersion())
	}
	var rules []string
	_ = env.AppTx(ctx, env.FirmA, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT rule_id FROM validation_issues WHERE run_id=$1 ORDER BY seq`, res.RunID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r string
			if err := rows.Scan(&r); err != nil {
				return err
			}
			rules = append(rules, r)
		}
		return rows.Err()
	})
	if len(rules) != 3 || rules[0] != "ibr-132-ae" || rules[1] != "AE-FMT-001" || rules[2] != "ibr-cl-04" {
		t.Errorf("issue order = %v", rules)
	}
	var requestedBy, args, ar string
	var fixable bool
	_ = env.AppTx(ctx, env.FirmA, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT requested_by FROM validation_runs WHERE id=$1`, res.RunID).Scan(&requestedBy); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT message_args::text, message_ar, fixable FROM validation_issues WHERE run_id=$1 AND seq=0`, res.RunID).Scan(&args, &ar, &fixable)
	})
	if requestedBy != "user-1" || args != `{"value": "x"}` || ar != "ر" || !fixable {
		t.Errorf("stored: requested_by=%q args=%q ar=%q fixable=%v", requestedBy, args, ar, fixable)
	}
	// Legacy issues column keeps the Phase 0 IssueView shape.
	var legacy []map[string]string
	_ = env.AppTx(ctx, env.FirmA, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT issues FROM invoices WHERE id=$1`, id).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &legacy)
	})
	if len(legacy) != 3 || legacy[0]["rule_id"] != "ibr-132-ae" || legacy[0]["severity"] != "error" ||
		legacy[0]["path"] != "seller.tax_registration_number" || legacy[0]["message"] != "m ibr-132-ae" || legacy[2]["severity"] != "warning" {
		t.Errorf("legacy issues = %v", legacy)
	}
	if got := getInvoice(t, env, env.FirmA, id).Status; got != "has_issues" {
		t.Errorf("status = %s", got)
	}
}

func TestWarningsOnlyIsValidated(t *testing.T) {
	env := trackctest.Setup(t)
	id := env.SeedInvoice(t, env.FirmA, payload)
	svc := &validation.Service{Pool: env.App, Validator: withIssues(issue("ibr-cl-04", sevWarn, "currency", false))}
	res, err := svc.Run(context.Background(), env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual})
	if err != nil || res.Status != "validated" || res.Warnings != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// AC-11: a redelivered invoice.extracted never re-validates.
func TestExtractedRedeliverySkipped(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	id := env.SeedInvoice(t, env.FirmA, payload)
	v := withIssues(issue("ibr-132-ae", sevErr, "seller.tax_registration_number", true))
	var hooks atomic.Int32
	svc := &validation.Service{Pool: env.App, Validator: v, OnRun: func(context.Context, uuid.UUID, uuid.UUID, validation.RunResult) { hooks.Add(1) }}

	first, err := svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerExtracted})
	if err != nil || first.Skipped {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerExtracted})
	if err != nil || !second.Skipped {
		t.Fatalf("second: %+v %v", second, err)
	}
	if v.count() != 1 || hooks.Load() != 1 {
		t.Errorf("validate calls=%d hooks=%d, want 1 and 1", v.count(), hooks.Load())
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1`, id); n != 1 {
		t.Errorf("runs = %d", n)
	}
	// A manual run is never skipped.
	if r, err := svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual}); err != nil || r.Skipped {
		t.Errorf("manual: %+v %v", r, err)
	}
}

// AC-11 regression (F4): a human changed the stored payload; a stale invoice.extracted event must not
// overwrite the outcome and the validator must see the stored payload, not the event's copy.
func TestStoredPayloadIsValidatedNotTheEventCopy(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	id := env.SeedInvoice(t, env.FirmA, payload)
	if err := env.AppTx(ctx, env.FirmA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE invoices SET payload = jsonb_set(payload, '{invoice_number}', '"FIXED-9"') WHERE id=$1`, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	v := cleanValidator()
	svc := &validation.Service{Pool: env.App, Validator: v}
	if _, err := svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerExtracted}); err != nil {
		t.Fatal(err)
	}
	if got := v.calls[0].GetInvoice().GetInvoiceNumber(); got != "FIXED-9" {
		t.Errorf("validator saw %q, want the stored payload", got)
	}
}

func TestRunMissingOrForeignInvoiceIsPermanent(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	idA := env.SeedInvoice(t, env.FirmA, payload)
	v := cleanValidator()
	svc := &validation.Service{Pool: env.App, Validator: v}
	for name, c := range map[string]struct{ firm, id uuid.UUID }{
		"missing":      {env.FirmA, uuid.New()},
		"cross-tenant": {env.FirmB, idA},
	} {
		_, err := svc.Run(ctx, c.firm, c.id, validation.RunOpts{Trigger: validation.TriggerExtracted})
		if !errors.Is(err, validation.ErrPermanent) || !errors.Is(err, db.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrPermanent wrapping db.ErrNotFound", name, err)
		}
	}
	if v.count() != 0 {
		t.Errorf("validator called %d times", v.count())
	}
}

func TestRunUndecodablePayloadIsPermanent(t *testing.T) {
	env := trackctest.Setup(t)
	id := env.SeedInvoice(t, env.FirmA, `{"invoice_number":"X","no_such_field":"y"}`)
	v := cleanValidator()
	svc := &validation.Service{Pool: env.App, Validator: v}
	_, err := svc.Run(context.Background(), env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual})
	if !errors.Is(err, validation.ErrPermanent) {
		t.Fatalf("err = %v, want ErrPermanent", err)
	}
	if v.count() != 0 {
		t.Error("validator called for an undecodable payload")
	}
}

func TestValidatorErrorClassification(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	id := env.SeedInvoice(t, env.FirmA, payload)
	mk := func(code connect.Code) *validation.Service {
		return &validation.Service{Pool: env.App, Validator: &fakeValidator{fn: func(int, *compliancev1.ValidateRequest) (*compliancev1.ValidationRun, error) {
			return nil, connect.NewError(code, errors.New("boom"))
		}}}
	}
	if _, err := mk(connect.CodeInvalidArgument).Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual}); !errors.Is(err, validation.ErrPermanent) {
		t.Errorf("INVALID_ARGUMENT: %v, want ErrPermanent", err)
	}
	_, err := mk(connect.CodeUnavailable).Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual})
	if err == nil || errors.Is(err, validation.ErrPermanent) || errors.Is(err, validation.ErrStale) {
		t.Errorf("UNAVAILABLE: %v, want a transient error", err)
	}
	// An empty response is transient too.
	empty := &validation.Service{Pool: env.App, Validator: &fakeValidator{fn: func(int, *compliancev1.ValidateRequest) (*compliancev1.ValidationRun, error) { return nil, nil }}}
	if _, err := empty.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual}); err == nil || errors.Is(err, validation.ErrPermanent) {
		t.Errorf("empty run: %v", err)
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1`, id); n != 0 {
		t.Errorf("failed validations wrote %d runs", n)
	}
	if got := getInvoice(t, env, env.FirmA, id).Status; got != "uploaded" {
		t.Errorf("status = %s", got)
	}
}

// bump changes the stored payload version behind the service's back, as a concurrent correction would.
func bump(t *testing.T, env trackctest.Env, firm, id uuid.UUID) {
	t.Helper()
	if err := env.AppTx(context.Background(), firm, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE invoices SET payload_version = payload_version + 1 WHERE id=$1`, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPayloadDriftRestartsOnce(t *testing.T) {
	env := trackctest.Setup(t)
	id := env.SeedInvoice(t, env.FirmA, payload)
	v := &fakeValidator{fn: func(n int, _ *compliancev1.ValidateRequest) (*compliancev1.ValidationRun, error) {
		if n == 1 {
			bump(t, env, env.FirmA, id)
		}
		return clean(""), nil
	}}
	svc := &validation.Service{Pool: env.App, Validator: v}
	res, err := svc.Run(context.Background(), env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	if v.count() != 2 || res.PayloadVersion != 2 {
		t.Errorf("validate calls=%d payload_version=%d, want 2 and 2", v.count(), res.PayloadVersion)
	}
	// The abandoned result was never stored: one run, for version 2.
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1 AND payload_version=2`, id); n != 1 {
		t.Errorf("runs at v2 = %d", n)
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1`, id); n != 1 {
		t.Errorf("runs = %d, want 1", n)
	}
}

func TestPayloadDriftThreeTimesIsStale(t *testing.T) {
	env := trackctest.Setup(t)
	id := env.SeedInvoice(t, env.FirmA, payload)
	v := &fakeValidator{fn: func(int, *compliancev1.ValidateRequest) (*compliancev1.ValidationRun, error) {
		bump(t, env, env.FirmA, id)
		return clean(""), nil
	}}
	svc := &validation.Service{Pool: env.App, Validator: v}
	_, err := svc.Run(context.Background(), env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual})
	if !errors.Is(err, validation.ErrStale) {
		t.Fatalf("err = %v, want ErrStale", err)
	}
	if v.count() != 3 {
		t.Errorf("validate calls = %d, want exactly 3 attempts", v.count())
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1`, id); n != 0 {
		t.Errorf("runs = %d, want 0", n)
	}
}

func TestLeavingReadyRevokesApprovalAndAudits(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	id := env.SeedInvoice(t, env.FirmA, payload)
	clean := &validation.Service{Pool: env.App, Validator: cleanValidator()}
	if _, err := clean.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual}); err != nil {
		t.Fatal(err)
	}
	approve(t, env, env.FirmA, id, 1)
	if inv := getInvoice(t, env, env.FirmA, id); inv.Status != "ready" || !inv.ApprovedPayloadVersion.Valid {
		t.Fatalf("not ready: %+v", inv)
	}

	// A clean re-run keeps ready, keeps the approval and writes no audit event.
	res, err := clean.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual})
	if err != nil || res.Status != "ready" {
		t.Fatalf("clean rerun: %+v %v", res, err)
	}
	inv := getInvoice(t, env, env.FirmA, id)
	if inv.Status != "ready" || !inv.ApprovedBy.Valid || inv.ApprovedBy.String != "u1" {
		t.Fatalf("approval lost on a clean run: %+v", inv)
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM audit_events WHERE invoice_id=$1`, id); n != 0 {
		t.Fatalf("audit events = %d before any revocation", n)
	}

	// A run with errors revokes: status, approval columns and the audit event change together.
	bad := &validation.Service{Pool: env.App, Validator: withIssues(issue("ibr-132-ae", sevErr, "seller.tax_registration_number", false))}
	res, err = bad.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerSweeper})
	if err != nil || res.Status != "has_issues" {
		t.Fatalf("bad run: %+v %v", res, err)
	}
	inv = getInvoice(t, env, env.FirmA, id)
	if inv.Status != "has_issues" || inv.ApprovedPayloadVersion.Valid || inv.ApprovedBy.Valid || inv.ApprovedAt.Valid {
		t.Fatalf("approval not cleared: %+v", inv)
	}
	var actorType, actorID, entityType, reason string
	var entityID uuid.UUID
	var before, after []byte
	n := 0
	if err := env.AppTx(ctx, env.FirmA, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT actor_type, actor_id, entity_type, entity_id, reason, before, after FROM audit_events WHERE invoice_id=$1 AND action='invoice.approval_revoked'`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			n++
			if err := rows.Scan(&actorType, &actorID, &entityType, &entityID, &reason, &before, &after); err != nil {
				return err
			}
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 || actorType != "system" || actorID != "api-go" || entityType != "invoice" || entityID != id || reason == "" {
		t.Errorf("audit: n=%d actor=%s/%s entity=%s/%s reason=%q", n, actorType, actorID, entityType, entityID, reason)
	}
	var b, a map[string]any
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	if b["status"] != "ready" || a["status"] != "has_issues" {
		t.Errorf("before=%v after=%v", b, a)
	}
	// A second error run does not revoke again.
	if _, err := bad.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM audit_events WHERE invoice_id=$1 AND action='invoice.approval_revoked'`, id); n != 1 {
		t.Errorf("revocation events = %d, want 1", n)
	}
}

// The revocation audit event is in the same transaction as the status change: if the audit insert
// fails nothing is stored.
func TestRunRollsBackWhenAnIssueIsRejectedByTheDatabase(t *testing.T) {
	env := trackctest.Setup(t)
	id := env.SeedInvoice(t, env.FirmA, payload)
	// "BAD RULE" breaks validation_issues.rule_id CHECK.
	svc := &validation.Service{Pool: env.App, Validator: withIssues(issue("BAD RULE", sevErr, "p", false))}
	if _, err := svc.Run(context.Background(), env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual}); err == nil {
		t.Fatal("want an error")
	}
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1`, id); n != 0 {
		t.Errorf("run row survived the rollback: %d", n)
	}
	if inv := getInvoice(t, env, env.FirmA, id); inv.Status != "uploaded" || inv.LatestRunID != uuid.Nil {
		t.Errorf("invoice changed: %+v", inv)
	}
}

func TestNeedsReviewIsStickyForSystemTriggers(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	id := env.SeedInvoice(t, env.FirmA, payload)
	setStatus(t, env, env.FirmA, id, "needs_review")
	svc := &validation.Service{Pool: env.App, Validator: cleanValidator()}

	res, err := svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerExtracted})
	if err != nil || res.Status != "needs_review" {
		t.Fatalf("extracted: %+v %v", res, err)
	}
	// Even a sweeper run (extracted would skip now) with errors keeps it.
	bad := &validation.Service{Pool: env.App, Validator: withIssues(issue("ibr-132-ae", sevErr, "p", false))}
	res, err = bad.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerSweeper})
	if err != nil || res.Status != "needs_review" || res.Errors != 1 {
		t.Fatalf("sweeper: %+v %v", res, err)
	}
	// The run is recorded even though the status is unchanged.
	if n := countRows(t, env, env.FirmA, `SELECT count(*) FROM validation_runs WHERE invoice_id=$1`, id); n != 2 {
		t.Errorf("runs = %d", n)
	}
	// A human leaves it.
	res, err = svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual, Actor: "u"})
	if err != nil || res.Status != "validated" {
		t.Fatalf("manual: %+v %v", res, err)
	}
}

func TestOnRunFiresAfterCommit(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	id := env.SeedInvoice(t, env.FirmA, payload)
	var seen validation.RunResult
	var seenFirm, seenInv uuid.UUID
	var committed string
	svc := &validation.Service{Pool: env.App, Validator: withIssues(issue("ibr-132-ae", sevErr, "p", true))}
	svc.OnRun = func(c context.Context, firm, inv uuid.UUID, r validation.RunResult) {
		seen, seenFirm, seenInv = r, firm, inv
		// Another connection already sees the commit.
		committed = getInvoice(t, env, firm, inv).Status
	}
	res, err := svc.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	if seenFirm != env.FirmA || seenInv != id || seen.RunID != res.RunID || seen.FixableErrors != 1 || committed != "has_issues" {
		t.Errorf("hook: firm=%s inv=%s run=%s committed=%q", seenFirm, seenInv, seen.RunID, committed)
	}
	// Failed runs do not call the hook.
	called := false
	failing := &validation.Service{Pool: env.App, OnRun: func(context.Context, uuid.UUID, uuid.UUID, validation.RunResult) { called = true },
		Validator: &fakeValidator{fn: func(int, *compliancev1.ValidateRequest) (*compliancev1.ValidationRun, error) {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("down"))
		}}}
	if _, err := failing.Run(ctx, env.FirmA, id, validation.RunOpts{Trigger: validation.TriggerManual}); err == nil || called {
		t.Errorf("err=%v called=%v", err, called)
	}
}
