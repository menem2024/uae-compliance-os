//go:build integration

package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackc"
)

// scriptedValidator reports one fixable error ("invoice_number" empty) until the invoice has a number.
type scriptedValidator struct {
	mu    sync.Mutex
	calls int
	down  bool
}

func (v *scriptedValidator) Validate(_ context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls++
	if v.down {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("validator down"))
	}
	run := &compliancev1.ValidationRun{RulesetVersion: "pint-ae@1.0.4+r1", RulesEvaluated: 302, DurationUs: 90}
	if req.Msg.GetInvoice().GetInvoiceNumber() == "" {
		run.Issues = append(run.Issues, &compliancev1.ValidationIssue{RuleId: "ibr-002", Severity: compliancev1.Severity_SEVERITY_ERROR,
			Path: "invoice_number", BusinessTerm: "IBT-001", Message: "number missing", MessageAr: "رقم الفاتورة مفقود", Fixable: true, SuggestedValue: "INV-1"})
	}
	if req.Msg.GetInvoice().GetNote() == "warn" {
		run.Issues = append(run.Issues, &compliancev1.ValidationIssue{RuleId: "AE-FMT-001", Severity: compliancev1.Severity_SEVERITY_WARNING,
			Path: "note", Message: "w"})
	}
	return connect.NewResponse(&compliancev1.ValidateResponse{Run: run}), nil
}

type xmlExporter struct {
	mu   sync.Mutex
	down bool
}

func (e *xmlExporter) Export(_ context.Context, r *connect.Request[compliancev1.ExportRequest]) (*connect.Response[compliancev1.ExportResponse], error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("exporter down"))
	}
	doc := []byte(`<Invoice>` + r.Msg.GetInvoice().GetInvoiceNumber() + `</Invoice>`)
	h := sha256.Sum256(doc)
	return connect.NewResponse(&compliancev1.ExportResponse{Xml: doc, Sha256: hex.EncodeToString(h[:]),
		Format: "pint-ae-billing-1.0.4/ubl-2.1", Exported: true, DocumentKind: "invoice"}), nil
}

type memObjects struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memObjects) Put(_ context.Context, k string, d []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = append([]byte(nil), d...)
	return nil
}

func (s *memObjects) Get(_ context.Context, k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[k], nil
}

type fakeFixes struct {
	err   error
	calls int
}

func (f *fakeFixes) RequestOnDemand(_ context.Context, _, _ uuid.UUID, by string) (trackc.FixTask, error) {
	f.calls++
	if f.err != nil {
		return trackc.FixTask{}, f.err
	}
	return trackc.FixTask{ID: uuid.New(), Mode: "on_demand", Status: "requested"}, nil
}

type orgMap map[string]uuid.UUID

func (o orgMap) FirmIDForOrg(_ context.Context, org string) (uuid.UUID, error) {
	if id, ok := o[org]; ok {
		return id, nil
	}
	return uuid.Nil, db.ErrNotFound
}

type tcVerifier struct{}

func (tcVerifier) Verify(_ context.Context, raw string) (auth.Principal, error) {
	switch raw {
	case "tok-a":
		return auth.Principal{Subject: "user-a", OrgID: "org-A"}, nil
	case "tok-b":
		return auth.Principal{Subject: "user-b", OrgID: "org-B"}, nil
	}
	return auth.Principal{}, auth.ErrUnauthenticated
}

type tcEnv struct {
	trackctest.Env
	h     http.Handler
	val   *scriptedValidator
	exp   *xmlExporter
	fixes *fakeFixes
	mod   *trackc.Module
}

func newTCEnv(t *testing.T) *tcEnv {
	t.Helper()
	env := trackctest.Setup(t)
	e := &tcEnv{Env: env, val: &scriptedValidator{}, exp: &xmlExporter{}}
	m, err := trackc.New(trackc.Deps{
		Pool: env.App, Validator: e.val, Exporter: e.exp, Store: &memObjects{m: map[string][]byte{}},
		Firms:        orgMap{"org-A": env.FirmA, "org-B": env.FirmB},
		ReadLimiter:  fakeLimiter{ok: true},
		WriteLimiter: fakeLimiter{ok: true},
		Config:       trackc.Config{FixAgentAuto: true, ReadPerMinute: 600, WritePerMinute: 120, ExportsBucket: "documents"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.mod = m
	e.h = NewRouter(tcVerifier{}, PGStore{Pool: env.App}, &fakePub{}, fakeLimiter{ok: true}, func(context.Context) error { return nil }, WithTrackC(m))
	return e
}

func (e *tcEnv) call(method, path, tok, body string) (*httptest.ResponseRecorder, map[string]any) {
	rec := do(e.h, method, path, tok, body)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func (e *tcEnv) must(t *testing.T, code int, method, path, tok, body string) map[string]any {
	t.Helper()
	rec, out := e.call(method, path, tok, body)
	if rec.Code != code {
		t.Fatalf("%s %s: %d (want %d) %s", method, path, rec.Code, code, rec.Body)
	}
	return out
}

func (e *tcEnv) wantErr(t *testing.T, code int, errCode, method, path, tok, body string) {
	t.Helper()
	rec, out := e.call(method, path, tok, body)
	if rec.Code != code || out["error"] != errCode || len(out) != 1 {
		t.Errorf("%s %s: %d %s (want %d %q)", method, path, rec.Code, rec.Body, code, errCode)
	}
}

const (
	noNumber = `{"issue_date":"2026-09-27","seller_trn":"100000000000003","currency":"AED","total_amount":"10.00","vat_amount":"0.50","seller":{"name":"Seller LLC"},"buyer":{"name":"Buyer LLC"}}`
	withNum  = `{"invoice_number":"INV-7","issue_date":"2026-09-27","seller_trn":"100000000000003","currency":"AED","total_amount":"10.00","vat_amount":"0.50"}`
)

func vpath(id uuid.UUID, tail string) string {
	return "/v1/invoices/" + id.String() + "/validation" + tail
}

func TestTrackCListInvoices(t *testing.T) {
	e := newTCEnv(t)
	var ids []uuid.UUID
	for i := 0; i < 5; i++ {
		ids = append(ids, e.SeedInvoice(t, e.FirmA, strings.Replace(withNum, "INV-7", "INV-"+string(rune('A'+i)), 1)))
	}
	other := e.SeedInvoice(t, e.FirmB, withNum)

	// Keyset pagination: pages of 2, newest first, nothing repeated or skipped.
	var got []string
	cursor := ""
	for page := 0; page < 5; page++ {
		path := "/v1/invoices?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		out := e.must(t, 200, "GET", path, "tok-a", "")
		items := out["items"].([]any)
		for _, it := range items {
			got = append(got, it.(map[string]any)["id"].(string))
		}
		next, _ := out["next_cursor"].(string)
		if next == "" {
			if len(items) > 2 {
				t.Fatalf("page %d has %d items", page, len(items))
			}
			break
		}
		cursor = next
	}
	if len(got) != 5 {
		t.Fatalf("paged %d invoices: %v", len(got), got)
	}
	for i, id := range got {
		if id != ids[4-i].String() {
			t.Errorf("position %d: %s, want %s", i, id, ids[4-i])
		}
	}
	for _, id := range got {
		if id == other.String() {
			t.Error("foreign invoice listed")
		}
	}

	first := e.must(t, 200, "GET", "/v1/invoices?limit=1&q=inv-c", "tok-a", "")["items"].([]any)
	if len(first) != 1 || first[0].(map[string]any)["invoice_number"] != "INV-C" {
		t.Errorf("q filter: %v", first)
	}
	row := first[0].(map[string]any)
	for _, k := range []string{"id", "status", "payload_version", "invoice_number", "issue_date", "currency", "total_amount", "seller_name", "buyer_name", "error_count", "warning_count", "created_at", "updated_at"} {
		if _, ok := row[k]; !ok {
			t.Errorf("list row lacks %q: %v", k, row)
		}
	}
	if row["total_amount"] != "10.00" {
		t.Errorf("money must stay a string: %v", row["total_amount"])
	}
	if n := len(e.must(t, 200, "GET", "/v1/invoices?status=ready", "tok-a", "")["items"].([]any)); n != 0 {
		t.Errorf("status filter returned %d", n)
	}
	if n := len(e.must(t, 200, "GET", "/v1/invoices?q=%25", "tok-a", "")["items"].([]any)); n != 0 {
		t.Errorf("q is a plain substring, not a LIKE pattern: %d rows", n)
	}
	if n := len(e.must(t, 200, "GET", "/v1/invoices", "tok-b", "")["items"].([]any)); n != 1 {
		t.Errorf("firm B sees %d invoices", n)
	}
}

func TestTrackCValidationFlow(t *testing.T) {
	e := newTCEnv(t)
	id := e.SeedInvoice(t, e.FirmA, noNumber)

	// Detail before any run.
	d := e.must(t, 200, "GET", vpath(id, ""), "tok-a", "")
	if d["status"] != "uploaded" || d["payload_version"] != float64(1) || d["latest_run"] != nil || d["approval"] != nil ||
		d["latest_fix_task"] != nil || d["open_proposal"] != nil {
		t.Errorf("fresh detail: %v", d)
	}
	if p, _ := d["payload"].(map[string]any); p["currency"] != "AED" {
		t.Errorf("payload not embedded as JSON: %v", d["payload"])
	}

	// Validate now.
	r := e.must(t, 200, "POST", vpath(id, ""), "tok-a", "")
	if r["status"] != "has_issues" || r["errors"] != float64(1) || r["fixable_errors"] != float64(1) || r["ruleset_version"] != "pint-ae@1.0.4+r1" {
		t.Errorf("run result: %v", r)
	}
	issues := r["issues"].([]any)
	is := issues[0].(map[string]any)
	if is["rule_id"] != "ibr-002" || is["path"] != "invoice_number" || is["fixable"] != true || is["suggested_value"] != "INV-1" ||
		is["message_ar"] == "" || is["business_term"] != "IBT-001" || is["severity"] != "error" {
		t.Errorf("issue json: %v", is)
	}
	run1 := r["run_id"].(string)

	d = e.must(t, 200, "GET", vpath(id, ""), "tok-a", "")
	lr := d["latest_run"].(map[string]any)
	if d["status"] != "has_issues" || lr["id"] != run1 || lr["trigger"] != "manual" || len(lr["issues"].([]any)) != 1 {
		t.Errorf("detail after run: %v", d)
	}

	// Validate-now audit trail.
	a := e.must(t, 200, "GET", vpath(id, "/audit"), "tok-a", "")["items"].([]any)
	if len(a) != 1 || a[0].(map[string]any)["action"] != "invoice.validation_requested" ||
		a[0].(map[string]any)["actor_type"] != "user" || a[0].(map[string]any)["actor_id"] != "user-a" {
		t.Errorf("audit: %v", a)
	}

	// Second run (a warning appears: add note via correction would bump version; use the validator knob).
	e.must(t, 200, "POST", vpath(id, ""), "tok-a", "")
	runs := e.must(t, 200, "GET", vpath(id, "/runs"), "tok-a", "")["items"].([]any)
	if len(runs) != 2 || runs[0].(map[string]any)["id"] == run1 || runs[1].(map[string]any)["id"] != run1 {
		t.Errorf("runs newest first: %v", runs)
	}
	one := e.must(t, 200, "GET", vpath(id, "/runs/"+run1), "tok-a", "")
	if one["id"] != run1 || len(one["issues"].([]any)) != 1 || one["error_count"] != float64(1) {
		t.Errorf("run detail: %v", one)
	}
	e.wantErr(t, 404, "not_found", "GET", vpath(id, "/runs/"+uuid.NewString()), "tok-a", "")

	// Validator down: 503 validator_unavailable, nothing stored.
	e.val.mu.Lock()
	e.val.down = true
	e.val.mu.Unlock()
	e.wantErr(t, 503, "validator_unavailable", "POST", vpath(id, ""), "tok-a", "")
	e.val.mu.Lock()
	e.val.down = false
	e.val.mu.Unlock()
}

func TestTrackCRunDiff(t *testing.T) {
	e := newTCEnv(t)
	id := e.SeedInvoice(t, e.FirmA, noNumber)
	run1 := e.must(t, 200, "POST", vpath(id, ""), "tok-a", "")["run_id"].(string)

	// First run has no predecessor: everything is "added", against is null.
	d := e.must(t, 200, "GET", vpath(id, "/runs/"+run1+"/diff"), "tok-a", "")
	if d["against_run_id"] != nil || len(d["added"].([]any)) != 1 || len(d["removed"].([]any)) != 0 || d["run_id"] != run1 {
		t.Errorf("first diff: %v", d)
	}

	// Correct the number (re-validates automatically), then diff the new run against the default (previous).
	c := e.must(t, 200, "POST", vpath(id, "/corrections"), "tok-a",
		`{"payload_version":1,"changes":[{"path":"invoice_number","old_value":"","new_value":"INV-9"}],"reason":"typed it"}`)
	run2 := c["run"].(map[string]any)["run_id"].(string)
	d = e.must(t, 200, "GET", vpath(id, "/runs/"+run2+"/diff"), "tok-a", "")
	if d["against_run_id"] != run1 || len(d["added"].([]any)) != 0 {
		t.Errorf("default against: %v", d)
	}
	removed := d["removed"].([]any)
	if len(removed) != 1 {
		t.Fatalf("removed: %v", removed)
	}
	rm := removed[0].(map[string]any)
	if rm["rule_id"] != "ibr-002" || rm["path"] != "invoice_number" || rm["severity"] != "error" || len(rm) != 3 {
		t.Errorf("removed shape: %v", rm)
	}
	// Reverse: diff run1 against run2 = the issue is added.
	d = e.must(t, 200, "GET", vpath(id, "/runs/"+run1+"/diff?against="+run2), "tok-a", "")
	if len(d["added"].([]any)) != 1 || len(d["removed"].([]any)) != 0 || d["against_run_id"] != run2 {
		t.Errorf("explicit against: %v", d)
	}
	e.wantErr(t, 404, "not_found", "GET", vpath(id, "/runs/"+run1+"/diff?against="+uuid.NewString()), "tok-a", "")
	e.wantErr(t, 404, "not_found", "GET", vpath(id, "/runs/"+run1+"/diff?against=nope"), "tok-a", "")
}

func TestTrackCCorrectionsAndApproval(t *testing.T) {
	e := newTCEnv(t)
	id := e.SeedInvoice(t, e.FirmA, noNumber)
	e.must(t, 200, "POST", vpath(id, ""), "tok-a", "")

	// Approve before it is clean.
	e.wantErr(t, 409, "not_validated", "POST", vpath(id, "/approve"), "tok-a", `{"payload_version":1}`)

	fix := func(v int, path, old, nw string) string {
		b, _ := json.Marshal(map[string]any{"payload_version": v, "changes": []map[string]string{{"path": path, "old_value": old, "new_value": nw}}})
		return string(b)
	}
	e.wantErr(t, 400, "bad_path", "POST", vpath(id, "/corrections"), "tok-a", fix(1, "no_such_field", "", "x"))
	e.wantErr(t, 409, "old_value_mismatch", "POST", vpath(id, "/corrections"), "tok-a", fix(1, "invoice_number", "WRONG", "INV-9"))
	e.wantErr(t, 409, "stale_payload", "POST", vpath(id, "/corrections"), "tok-a", fix(7, "invoice_number", "", "INV-9"))

	c := e.must(t, 200, "POST", vpath(id, "/corrections"), "tok-a", fix(1, "invoice_number", "", "INV-9"))
	if c["payload_version"] != float64(2) || c["revalidation"] != "done" || c["run"].(map[string]any)["status"] != "validated" {
		t.Errorf("correction result: %v", c)
	}
	// The old version is stale now.
	e.wantErr(t, 409, "stale_payload", "POST", vpath(id, "/corrections"), "tok-a", fix(1, "invoice_number", "INV-9", "INV-10"))
	e.wantErr(t, 409, "stale_payload", "POST", vpath(id, "/approve"), "tok-a", `{"payload_version":1}`)

	ap := e.must(t, 200, "POST", vpath(id, "/approve"), "tok-a", `{"payload_version":2}`)
	if ap["status"] != "ready" || ap["payload_version"] != float64(2) || ap["approved_by"] != "user-a" || ap["approved_at"] == "" {
		t.Errorf("approve: %v", ap)
	}
	d := e.must(t, 200, "GET", vpath(id, ""), "tok-a", "")
	if d["status"] != "ready" || d["approval"].(map[string]any)["approved_payload_version"] != float64(2) {
		t.Errorf("detail after approval: %v", d)
	}
	e.wantErr(t, 409, "not_validated", "POST", vpath(id, "/approve"), "tok-a", `{"payload_version":2}`)

	// Another correction clears the approval and revalidates.
	e.must(t, 200, "POST", vpath(id, "/corrections"), "tok-a", fix(2, "invoice_number", "INV-9", "INV-11"))
	d = e.must(t, 200, "GET", vpath(id, ""), "tok-a", "")
	if d["status"] != "validated" || d["approval"] != nil {
		t.Errorf("approval survived a correction: %v", d)
	}

	// Validator down after a correction: the change is stored, revalidation pending, status fixed.
	e.val.mu.Lock()
	e.val.down = true
	e.val.mu.Unlock()
	c = e.must(t, 200, "POST", vpath(id, "/corrections"), "tok-a", fix(3, "invoice_number", "INV-11", "INV-12"))
	if c["revalidation"] != "pending" || c["run"] != nil || c["payload_version"] != float64(4) {
		t.Errorf("pending revalidation: %v", c)
	}
	if d = e.must(t, 200, "GET", vpath(id, ""), "tok-a", ""); d["status"] != "fixed" {
		t.Errorf("status = %v, want fixed", d["status"])
	}

	// Audit trail, newest first: fields_changed x3, approved, validation_requested.
	var actions []string
	for _, it := range e.must(t, 200, "GET", vpath(id, "/audit"), "tok-a", "")["items"].([]any) {
		actions = append(actions, it.(map[string]any)["action"].(string))
	}
	want := []string{"invoice.fields_changed", "invoice.fields_changed", "invoice.approved", "invoice.fields_changed", "invoice.validation_requested"}
	if strings.Join(actions, ",") != strings.Join(want, ",") {
		t.Errorf("audit actions = %v, want %v", actions, want)
	}
}

func TestTrackCFixes(t *testing.T) {
	e := newTCEnv(t)
	id := e.SeedInvoice(t, e.FirmA, noNumber)

	// Before GB-2 nothing is wired.
	e.wantErr(t, 503, "fix_unavailable", "POST", vpath(id, "/fixes"), "tok-a", "")

	e.fixes = &fakeFixes{}
	e.mod.Fixes = e.fixes
	e.must(t, 202, "POST", vpath(id, "/fixes"), "tok-a", "")
	if e.fixes.calls != 1 {
		t.Errorf("requester calls = %d", e.fixes.calls)
	}
	a := e.must(t, 200, "GET", vpath(id, "/audit"), "tok-a", "")["items"].([]any)
	if len(a) != 1 || a[0].(map[string]any)["action"] != "invoice.fix_requested" {
		t.Errorf("audit: %v", a)
	}
	e.fixes.err = trackc.ErrNoFixableIssues
	e.wantErr(t, 409, "no_fixable_issues", "POST", vpath(id, "/fixes"), "tok-a", "")
	e.fixes.err = trackc.ErrFixInProgress
	e.wantErr(t, 409, "fix_in_progress", "POST", vpath(id, "/fixes"), "tok-a", "")
	if a = e.must(t, 200, "GET", vpath(id, "/audit"), "tok-a", "")["items"].([]any); len(a) != 1 {
		t.Errorf("a refused request was audited: %d events", len(a))
	}
}

func TestTrackCExports(t *testing.T) {
	e := newTCEnv(t)
	id := e.SeedInvoice(t, e.FirmA, withNum)
	e.must(t, 200, "POST", vpath(id, ""), "tok-a", "")
	create := func(tok string) (*httptest.ResponseRecorder, map[string]any) {
		return e.call("POST", "/v1/exports", tok, `{"invoice_id":"`+id.String()+`"}`)
	}
	if rec, out := create("tok-a"); rec.Code != 409 || out["error"] != "not_ready" {
		t.Errorf("unapproved: %d %s", rec.Code, rec.Body)
	}
	e.must(t, 200, "POST", vpath(id, "/approve"), "tok-a", `{"payload_version":1}`)

	e.exp.mu.Lock()
	e.exp.down = true
	e.exp.mu.Unlock()
	if rec, out := create("tok-a"); rec.Code != 503 || out["error"] != "exporter_unavailable" {
		t.Errorf("exporter down: %d %s", rec.Code, rec.Body)
	}
	e.exp.mu.Lock()
	e.exp.down = false
	e.exp.mu.Unlock()

	rec, out := create("tok-a")
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	xid := out["id"].(string)
	if out["invoice_id"] != id.String() || out["document_kind"] != "invoice" || out["format"] != "pint-ae-billing-1.0.4/ubl-2.1" ||
		out["filename"] != "INV-7-invoice.xml" || out["size_bytes"] != float64(len("<Invoice>INV-7</Invoice>")) || len(out["sha256"].(string)) != 64 {
		t.Errorf("view: %v", out)
	}

	got := e.must(t, 200, "GET", "/v1/exports/"+xid, "tok-a", "")
	if got["id"] != xid {
		t.Errorf("get: %v", got)
	}
	list := e.must(t, 200, "GET", "/v1/exports?invoice_id="+id.String(), "tok-a", "")["items"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != xid {
		t.Errorf("list: %v", list)
	}

	dl := do(e.h, "GET", "/v1/exports/"+xid+"/xml", "tok-a", "")
	if dl.Code != 200 || dl.Body.String() != "<Invoice>INV-7</Invoice>" {
		t.Fatalf("download: %d %q", dl.Code, dl.Body)
	}
	for k, v := range map[string]string{
		"Content-Type": "application/xml; charset=utf-8", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store",
		"Content-Disposition": `attachment; filename="INV-7-invoice.xml"`,
	} {
		if dl.Header().Get(k) != v {
			t.Errorf("%s = %q, want %q", k, dl.Header().Get(k), v)
		}
	}

	// Tenant isolation.
	e.wantErr(t, 404, "not_found", "GET", "/v1/exports/"+xid, "tok-b", "")
	e.wantErr(t, 404, "not_found", "GET", "/v1/exports/"+xid+"/xml", "tok-b", "")
	e.wantErr(t, 404, "not_found", "POST", "/v1/exports", "tok-b", `{"invoice_id":"`+id.String()+`"}`)
	if l := e.must(t, 200, "GET", "/v1/exports?invoice_id="+id.String(), "tok-b", "")["items"].([]any); len(l) != 0 {
		t.Errorf("firm B lists %d exports", len(l))
	}
	e.wantErr(t, 404, "not_found", "GET", "/v1/exports/"+uuid.NewString(), "tok-a", "")

	// A corrupted object is reported, not served.
	e.mod.Exports.Store.(*memObjects).m["firms/"+e.FirmA.String()+"/exports/"+xid+".xml"] = []byte("tampered")
	e.wantErr(t, 500, "export_corrupt", "GET", "/v1/exports/"+xid+"/xml", "tok-a", "")
}

// AC-9 parity: every invoice-scoped route answers 404 not_found for another Firm's invoice, whatever
// the method, and never changes it.
func TestTrackCCrossFirmIsolation(t *testing.T) {
	e := newTCEnv(t)
	id := e.SeedInvoice(t, e.FirmA, noNumber)
	run := e.must(t, 200, "POST", vpath(id, ""), "tok-a", "")["run_id"].(string)
	e.fixes = &fakeFixes{}
	e.mod.Fixes = e.fixes
	before := e.must(t, 200, "GET", vpath(id, ""), "tok-a", "")

	corr := `{"payload_version":1,"changes":[{"path":"invoice_number","old_value":"","new_value":"HACK"}]}`
	for _, r := range [][3]string{
		{"GET", vpath(id, ""), ""},
		{"POST", vpath(id, ""), ""},
		{"GET", vpath(id, "/runs"), ""},
		{"GET", vpath(id, "/runs/"+run), ""},
		{"GET", vpath(id, "/runs/"+run+"/diff"), ""},
		{"POST", vpath(id, "/corrections"), corr},
		{"POST", vpath(id, "/approve"), `{"payload_version":1}`},
		{"POST", vpath(id, "/fixes"), ""},
		{"GET", vpath(id, "/audit"), ""},
	} {
		e.wantErr(t, 404, "not_found", r[0], r[1], "tok-b", r[2])
	}
	if e.fixes.calls != 0 {
		t.Error("fix requested for a foreign invoice")
	}
	after := e.must(t, 200, "GET", vpath(id, ""), "tok-a", "")
	if before["payload_version"] != after["payload_version"] || before["status"] != after["status"] {
		t.Errorf("foreign calls changed the invoice: %v -> %v", before, after)
	}
	// Nothing was audited or validated on behalf of the stranger.
	if n := len(e.must(t, 200, "GET", vpath(id, "/audit"), "tok-a", "")["items"].([]any)); n != 1 {
		t.Errorf("audit events = %d, want only the owner's validate-now", n)
	}
}

func TestTrackCFirmsShareNothingInLists(t *testing.T) {
	e := newTCEnv(t)
	a := e.SeedInvoice(t, e.FirmA, withNum)
	b := e.SeedInvoice(t, e.FirmB, withNum)
	e.must(t, 200, "POST", vpath(b, ""), "tok-b", "")
	ra := e.must(t, 200, "GET", "/v1/invoices", "tok-a", "")["items"].([]any)
	if len(ra) != 1 || ra[0].(map[string]any)["id"] != a.String() {
		t.Errorf("firm A list: %v", ra)
	}
}
