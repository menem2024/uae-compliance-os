//go:build integration

package trackc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackc"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

const (
	oldRS = "pint-ae@0.0-skeleton"
	newRS = "pint-ae@1.0.4+r1"
)

func iss(rule string, sev compliancev1.Severity, path string) *compliancev1.ValidationIssue {
	return &compliancev1.ValidationIssue{RuleId: rule, Severity: sev, Path: path, Message: "m"}
}

const (
	sevE = compliancev1.Severity_SEVERITY_ERROR
	sevW = compliancev1.Severity_SEVERITY_WARNING
)

// rsValidator answers per (requested RuleSet, invoice number): the empty request is the old RuleSet.
type rsValidator struct {
	oldIssues, newIssues map[string][]*compliancev1.ValidationIssue
	failNew              string
	calls                int
}

func (v *rsValidator) Validate(_ context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	v.calls++
	num := req.Msg.GetInvoice().GetInvoiceNumber()
	if req.Msg.GetRulesetVersion() == "" {
		return connect.NewResponse(&compliancev1.ValidateResponse{Run: &compliancev1.ValidationRun{RulesetVersion: oldRS, Issues: v.oldIssues[num]}}), nil
	}
	if num == v.failNew {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("validator down"))
	}
	return connect.NewResponse(&compliancev1.ValidateResponse{Run: &compliancev1.ValidationRun{RulesetVersion: req.Msg.GetRulesetVersion(), Issues: v.newIssues[num]}}), nil
}

func seed(t *testing.T, env trackctest.Env, firm uuid.UUID, svc *validation.Service, num string) uuid.UUID {
	t.Helper()
	id := env.SeedInvoice(t, firm, `{"invoice_number":"`+num+`"}`)
	if _, err := svc.Run(context.Background(), firm, id, validation.RunOpts{Trigger: validation.TriggerManual, Actor: "u"}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRevalidate(t *testing.T) {
	env := trackctest.Setup(t)
	ctx := context.Background()
	v := &rsValidator{
		oldIssues: map[string][]*compliancev1.ValidationIssue{
			"A": {iss("ibr-001", sevE, "invoice_number")},
			"C": {iss("ibr-001", sevE, "invoice_number")},
			"F": {iss("ibr-001", sevE, "invoice_number")},
		},
		newIssues: map[string][]*compliancev1.ValidationIssue{
			"B": {iss("ibr-002", sevE, "issue_date"), iss("AE-FMT-001", sevW, "note")},
			"C": {iss("ibr-001", sevW, "invoice_number")},
			"X": {iss("ibr-003", sevE, "currency")},
		},
		failNew: "F",
	}
	svc := &validation.Service{Pool: env.App, Validator: v}
	a := seed(t, env, env.FirmA, svc, "A")
	b := seed(t, env, env.FirmA, svc, "B")
	c := seed(t, env, env.FirmA, svc, "C")
	f := seed(t, env, env.FirmA, svc, "F")
	x := seed(t, env, env.FirmB, svc, "X")
	_ = env.SeedInvoice(t, env.FirmA, `{"invoice_number":"never-validated"}`) // no run: out of scope

	var report bytes.Buffer
	tot, err := trackc.Revalidate(ctx, trackc.RevalidateOpts{Pool: env.App, Svc: svc, Ruleset: newRS, Firm: env.FirmA, Rate: 1000, Report: &report})
	if err != nil {
		t.Fatal(err)
	}
	if tot.Invoices != 3 || tot.Failed != 1 || tot.IssuesAdded != 3 || tot.IssuesRemoved != 2 {
		t.Errorf("totals = %+v", tot)
	}
	if tot.StatusChanges["has_issues->validated"] != 2 || tot.StatusChanges["validated->has_issues"] != 1 || len(tot.StatusChanges) != 2 {
		t.Errorf("status changes = %v", tot.StatusChanges)
	}
	var printed bytes.Buffer
	tot.Print(&printed)
	for _, want := range []string{"invoices: 3", "failed: 1", "issues added: 3", "issues removed: 2", "has_issues -> validated: 2"} {
		if !strings.Contains(printed.String(), want) {
			t.Errorf("printed totals lack %q:\n%s", want, printed.String())
		}
	}

	// JSONL: one line per re-validated invoice, in id order.
	type line struct {
		InvoiceID  uuid.UUID `json:"invoice_id"`
		FromRunID  uuid.UUID `json:"from_run_id"`
		ToRunID    uuid.UUID `json:"to_run_id"`
		FromStatus string    `json:"from_status"`
		ToStatus   string    `json:"to_status"`
		Added      []struct {
			RuleID   string `json:"rule_id"`
			Path     string `json:"path"`
			Severity string `json:"severity"`
		} `json:"added"`
		Removed []struct {
			RuleID   string `json:"rule_id"`
			Path     string `json:"path"`
			Severity string `json:"severity"`
		} `json:"removed"`
	}
	byInv := map[uuid.UUID]line{}
	rows := strings.Split(strings.TrimSpace(report.String()), "\n")
	if len(rows) != 3 {
		t.Fatalf("report has %d lines:\n%s", len(rows), report.String())
	}
	var prev uuid.UUID
	for _, r := range rows {
		var l line
		dec := json.NewDecoder(strings.NewReader(r))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&l); err != nil {
			t.Fatalf("%q: %v", r, err)
		}
		if l.InvoiceID.String() <= prev.String() {
			t.Errorf("report not in id order")
		}
		prev = l.InvoiceID
		byInv[l.InvoiceID] = l
	}
	if _, ok := byInv[f]; ok {
		t.Error("failed invoice is in the report")
	}
	la, lb, lc := byInv[a], byInv[b], byInv[c]
	if la.FromStatus != "has_issues" || la.ToStatus != "validated" || len(la.Added) != 0 || len(la.Removed) != 1 || la.Removed[0].RuleID != "ibr-001" {
		t.Errorf("A: %+v", la)
	}
	if lb.FromStatus != "validated" || lb.ToStatus != "has_issues" || len(lb.Added) != 2 || len(lb.Removed) != 0 {
		t.Errorf("B: %+v", lb)
	}
	if len(lc.Added) != 1 || lc.Added[0].Severity != "warning" || len(lc.Removed) != 1 || lc.Removed[0].Severity != "error" {
		t.Errorf("C (severity change is a different issue): %+v", lc)
	}
	if la.FromRunID == uuid.Nil || la.ToRunID == uuid.Nil || la.FromRunID == la.ToRunID {
		t.Errorf("run ids: %+v", la)
	}

	// The database agrees: new latest runs use the new RuleSet, trigger ruleset_upgrade; F and the other Firm are untouched.
	rs := func(firm, inv uuid.UUID) (string, string) {
		var ruleset, trig string
		if err := db.WithFirm(ctx, env.App, firm, func(q *sqlc.Queries) error {
			i, err := q.TrackCGetInvoice(ctx, inv)
			if err != nil {
				return err
			}
			r, err := q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: i.LatestRunID, InvoiceID: inv})
			ruleset, trig = r.RulesetVersion, r.Trigger
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ruleset, trig
	}
	if r, tr := rs(env.FirmA, a); r != newRS || tr != "ruleset_upgrade" {
		t.Errorf("A latest run: %s %s", r, tr)
	}
	if r, _ := rs(env.FirmA, f); r != oldRS {
		t.Errorf("F latest run: %s", r)
	}
	if r, _ := rs(env.FirmB, x); r != oldRS {
		t.Errorf("other firm was touched: %s", r)
	}

	// Idempotent: a second pass only retries the failed invoice; with the validator healthy, all firms.
	v.failNew = ""
	tot2, err := trackc.Revalidate(ctx, trackc.RevalidateOpts{Pool: env.App, Svc: svc, Ruleset: newRS, Rate: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if tot2.Invoices != 2 || tot2.Failed != 0 {
		t.Errorf("second pass = %+v (want F and the other Firm's X)", tot2)
	}
	tot3, err := trackc.Revalidate(ctx, trackc.RevalidateOpts{Pool: env.App, Svc: svc, Ruleset: newRS, Rate: 1000})
	if err != nil || tot3.Invoices != 0 {
		t.Errorf("third pass = %+v, %v", tot3, err)
	}
}
