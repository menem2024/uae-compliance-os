//go:build integration

// The proposal decision routes over a real Postgres 17 (forced RLS) with the production Track C
// routes (httpapi.WithTrackC) and a scripted validator: AC-7 end to end (accept applies the fix,
// audits it in the same transaction, then re-validates), the reject and stale paths, a validator
// outage, and tenant isolation. No network, no model.
package trackc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
	"github.com/menem2024/uae-platform/services/api-go/internal/fixapply"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpapi"
	"github.com/menem2024/uae-platform/services/api-go/internal/invoicefix"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackc"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

const propPayload = `{"invoice_number":"INV-1","issue_date":"2026-09-27","seller_trn":"100000000000003","currency":"aed","total_amount":"1050.00"}`

// propValidator answers like validator-rs would for this payload: the lower-case currency is one
// fixable error, anything else is clean. down makes it unavailable.
type propValidator struct{ down atomic.Bool }

func (v *propValidator) Validate(_ context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	if v.down.Load() {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("validator down"))
	}
	run := &compliancev1.ValidationRun{RulesetVersion: "pint-ae@1.0.4+r1", RulesEvaluated: 300, DurationUs: 50}
	if req.Msg.GetInvoice().GetCurrency() == "aed" {
		run.Issues = []*compliancev1.ValidationIssue{{RuleId: "ibr-cl-04", Severity: compliancev1.Severity_SEVERITY_ERROR, Path: "currency",
			BusinessTerm: "BT-5", Message: "currency code", MessageAr: "رمز العملة", Fixable: true, SuggestedValue: "AED"}}
	}
	return connect.NewResponse(&compliancev1.ValidateResponse{Run: run}), nil
}

type propExporter struct{}

func (propExporter) Export(context.Context, *connect.Request[compliancev1.ExportRequest]) (*connect.Response[compliancev1.ExportResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not used"))
}

type propStore struct{}

func (propStore) Put(context.Context, string, []byte, string) error { return errors.New("not used") }
func (propStore) Get(context.Context, string) ([]byte, error)       { return nil, errors.New("not used") }

type propVerifier struct{}

// Bearer "<org>/<subject>".
func (propVerifier) Verify(_ context.Context, tok string) (auth.Principal, error) {
	org, sub, ok := strings.Cut(tok, "/")
	if !ok || org == "" || sub == "" {
		return auth.Principal{}, errors.New("bad token")
	}
	return auth.Principal{Subject: sub, OrgID: org}, nil
}

type propFirms map[string]uuid.UUID

func (f propFirms) FirmIDForOrg(_ context.Context, org string) (uuid.UUID, error) {
	if id, ok := f[org]; ok {
		return id, nil
	}
	return uuid.Nil, db.ErrNotFound
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string) (bool, error) { return true, nil }

type propStack struct {
	env  trackctest.Env
	val  *propValidator
	svc  *validation.Service
	srv  *httptest.Server
	tc   *trackc.Module
	http *http.Client
}

func newPropStack(t *testing.T) *propStack { return newPropStackJS(t, nil) }

func newPropStackJS(t *testing.T, js jetstream.JetStream) *propStack {
	t.Helper()
	env := trackctest.Setup(t)
	val := &propValidator{}
	tc, err := trackc.New(trackc.Deps{Pool: env.App, Validator: val, Exporter: propExporter{}, Store: propStore{},
		Firms: propFirms{"org-a": env.FirmA, "org-b": env.FirmB}, ReadLimiter: allowAll{}, WriteLimiter: allowAll{},
		Config: trackc.Config{FixAgentAuto: true, ReadPerMinute: 1000, WritePerMinute: 1000, ExportsBucket: "documents"}, JS: js})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(auth.Middleware(propVerifier{}))
	httpapi.WithTrackC(tc)(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &propStack{env: env, val: val, svc: tc.Validation, srv: srv, tc: tc, http: srv.Client()}
}

// do calls the API as user in org ("org-a"/"org-b").
func (s *propStack) do(t *testing.T, org, user, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	switch v := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(v)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+org+"/"+user)
	resp, err := s.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (s *propStack) must(t *testing.T, org, user, method, path string, body any, want int, out any) {
	t.Helper()
	code, raw := s.do(t, org, user, method, path, body)
	if code != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, code, want, bytes.TrimSpace(raw))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
}

// invoiceWithFixableError seeds an invoice and validates it to has_issues; an open fix proposal for
// the lower-case currency is stored the way Track B's consumer stores the Fix agent's output.
func (s *propStack) invoiceWithProposal(t *testing.T) (invoice, proposal uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	invoice = s.env.SeedInvoice(t, s.env.FirmA, propPayload)
	res, err := s.svc.Run(ctx, s.env.FirmA, invoice, validation.RunOpts{Trigger: validation.TriggerExtracted})
	if err != nil || res.Status != "has_issues" {
		t.Fatalf("run = %+v, %v", res, err)
	}
	detail, err := anypb.New(&compliancev1.FixProposalDetail{ValidationRunId: res.RunID.String(), PayloadVersion: 1,
		RulesetVersion: "pint-ae@1.0.4+r1", ErrorsBefore: 1, ErrorsAfter: 0, ResolvedRuleIds: []string{"ibr-cl-04"},
		Notes: []*compliancev1.FixChangeNote{{Path: "currency", RuleIds: []string{"ibr-cl-04"}, Source: "deterministic", Rationale: "upper case"}}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	arg, err := proposals.InsertParams(&compliancev1.Proposal{ProposalId: uuid.NewString(), FirmId: s.env.FirmA.String(), RunId: uuid.NewString(),
		Agent: "fix", Kind: fixapply.KindFieldFix, TargetType: "invoice", TargetId: invoice.String(),
		SummaryKey: "P2Review.proposal.summary", SummaryArgs: map[string]string{"changes": "1", "resolved": "1"},
		Rationale: "currency codes are upper case", Confidence: 1, Detail: detail, CreatedAt: timestamppb.New(now),
		Changes: []*compliancev1.FieldChange{{Path: "currency", OldValue: "aed", NewValue: "AED"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithFirm(ctx, s.env.App, s.env.FirmA, func(q *sqlc.Queries) error {
		_, err := proposals.Insert(ctx, q, arg)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return invoice, arg.ID
}

type propDecisionResp struct {
	Proposal struct {
		ID    uuid.UUID `json:"id"`
		State string    `json:"state"`
	} `json:"proposal"`
	Revalidation string `json:"revalidation"`
	Run          *struct {
		InvoiceID      uuid.UUID `json:"invoice_id"`
		Status         string    `json:"status"`
		Errors         int       `json:"errors"`
		PayloadVersion int32     `json:"payload_version"`
	} `json:"run"`
}

type propInvoiceDetail struct {
	Status         string          `json:"status"`
	PayloadVersion int32           `json:"payload_version"`
	Payload        map[string]any  `json:"payload"`
	OpenProposal   json.RawMessage `json:"open_proposal"`
	LatestRun      struct {
		Trigger string `json:"trigger"`
	} `json:"latest_run"`
}

func (s *propStack) invoice(t *testing.T, org string, id uuid.UUID) propInvoiceDetail {
	t.Helper()
	var d propInvoiceDetail
	s.must(t, org, "u", http.MethodGet, "/v1/invoices/"+id.String()+"/validation", nil, http.StatusOK, &d)
	return d
}

func (s *propStack) auditActions(t *testing.T, id uuid.UUID) []string {
	t.Helper()
	var out struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	s.must(t, "org-a", "u", http.MethodGet, "/v1/invoices/"+id.String()+"/validation/audit", nil, http.StatusOK, &out)
	var actions []string
	for _, i := range out.Items {
		actions = append(actions, i.Action)
	}
	return actions
}

func TestProposalsAcceptEndToEnd(t *testing.T) {
	s := newPropStack(t)
	inv, prop := s.invoiceWithProposal(t)

	// The review queue shows the open proposal with its decoded detail.
	var list struct {
		Items []struct {
			ID     uuid.UUID `json:"id"`
			Kind   string    `json:"kind"`
			Detail struct {
				ErrorsBefore int32    `json:"errors_before"`
				ErrorsAfter  int32    `json:"errors_after"`
				Resolved     []string `json:"resolved_rule_ids"`
			} `json:"detail"`
			Changes []fieldpath.FieldChange `json:"changes"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	s.must(t, "org-a", "u", http.MethodGet, "/v1/proposals?state=proposed&target_type=invoice&target_id="+inv.String(), nil, http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].ID != prop || list.Items[0].Kind != "invoice.field_fix" ||
		list.Items[0].Detail.ErrorsBefore != 1 || list.Items[0].Detail.ErrorsAfter != 0 || len(list.Items[0].Changes) != 1 || list.NextCursor != nil {
		t.Fatalf("list = %+v", list)
	}
	if d := s.invoice(t, "org-a", inv); len(d.OpenProposal) == 0 || string(d.OpenProposal) == "null" || d.Status != "has_issues" {
		t.Fatalf("invoice detail open_proposal = %s, status %s", d.OpenProposal, d.Status)
	}

	var got propDecisionResp
	s.must(t, "org-a", "alice", http.MethodPost, "/v1/proposals/"+prop.String()+"/decision",
		map[string]string{"decision": "accept", "reason": "ok"}, http.StatusOK, &got)
	if got.Proposal.State != "accepted" || got.Revalidation != "done" || got.Run == nil || got.Run.Status != "validated" ||
		got.Run.Errors != 0 || got.Run.PayloadVersion != 2 || got.Run.InvoiceID != inv {
		t.Fatalf("decision = %+v run=%+v", got, got.Run)
	}
	d := s.invoice(t, "org-a", inv)
	if d.Status != "validated" || d.PayloadVersion != 2 || d.Payload["currency"] != "AED" || d.LatestRun.Trigger != "fix_accepted" ||
		string(d.OpenProposal) != "null" {
		t.Fatalf("invoice = %+v open=%s", d, d.OpenProposal)
	}
	// E3: the change and the decision are both in the invoice's audit trail (newest first).
	actions := s.auditActions(t, inv)
	if len(actions) != 2 || actions[0] != "proposal.accepted" || actions[1] != "invoice.fields_changed" {
		t.Fatalf("audit = %v", actions)
	}
	// The same proposal cannot be decided twice.
	code, raw := s.do(t, "org-a", "alice", http.MethodPost, "/v1/proposals/"+prop.String()+"/decision", map[string]string{"decision": "accept"})
	if code != http.StatusConflict || !strings.Contains(string(raw), "proposal_not_open") {
		t.Fatalf("second decision = %d %s", code, raw)
	}
}

func TestProposalsAcceptWhileValidatorIsDown(t *testing.T) {
	s := newPropStack(t)
	inv, prop := s.invoiceWithProposal(t)
	s.val.down.Store(true)

	var got propDecisionResp
	s.must(t, "org-a", "alice", http.MethodPost, "/v1/proposals/"+prop.String()+"/decision", map[string]string{"decision": "accept"}, http.StatusOK, &got)
	if got.Proposal.State != "accepted" || got.Revalidation != "pending" || got.Run != nil {
		t.Fatalf("decision = %+v", got)
	}
	if d := s.invoice(t, "org-a", inv); d.Status != "fixed" || d.PayloadVersion != 2 || d.Payload["currency"] != "AED" {
		t.Fatalf("invoice = %+v", d)
	}
	// The sweeper finishes the revalidation once the validator is back.
	s.val.down.Store(false)
	sw := &validation.Sweeper{Svc: s.svc, Pool: s.env.App, MinAge: time.Nanosecond}
	if n, err := sw.Sweep(context.Background()); err != nil || n < 1 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
	if d := s.invoice(t, "org-a", inv); d.Status != "validated" {
		t.Fatalf("invoice after sweep = %+v", d)
	}
}

func TestProposalsRejectChangesNothingElse(t *testing.T) {
	s := newPropStack(t)
	inv, prop := s.invoiceWithProposal(t)

	var got propDecisionResp
	s.must(t, "org-a", "bob", http.MethodPost, "/v1/proposals/"+prop.String()+"/decision",
		map[string]string{"decision": "reject", "reason": "customer pays in lower case"}, http.StatusOK, &got)
	if got.Proposal.State != "rejected" || got.Revalidation != "none" || got.Run != nil {
		t.Fatalf("decision = %+v", got)
	}
	d := s.invoice(t, "org-a", inv)
	if d.Status != "has_issues" || d.PayloadVersion != 1 || d.Payload["currency"] != "aed" || string(d.OpenProposal) != "null" {
		t.Fatalf("a rejection changed the invoice: %+v", d)
	}
	if actions := s.auditActions(t, inv); len(actions) != 1 || actions[0] != "proposal.rejected" {
		t.Fatalf("audit = %v", actions)
	}
	var one struct {
		State          string `json:"state"`
		DecidedBy      string `json:"decided_by"`
		DecisionReason string `json:"decision_reason"`
	}
	s.must(t, "org-a", "u", http.MethodGet, "/v1/proposals/"+prop.String(), nil, http.StatusOK, &one)
	if one.State != "rejected" || one.DecidedBy != "bob" || one.DecisionReason != "customer pays in lower case" {
		t.Fatalf("proposal = %+v", one)
	}
}

func TestProposalsStaleOldValueIs409AndChangesNothing(t *testing.T) {
	s := newPropStack(t)
	ctx := context.Background()
	inv, prop := s.invoiceWithProposal(t)
	// A person corrects the same field differently after the agent looked.
	if err := db.WithFirm(ctx, s.env.App, s.env.FirmA, func(q *sqlc.Queries) error {
		_, err := invoicefix.Apply(ctx, q, s.env.FirmA, inv, 0, []fieldpath.FieldChange{{Path: "currency", OldValue: "aed", NewValue: "USD"}},
			audit.Actor{Type: "user", ID: "carol"}, false, "invoice.fields_changed", "paid in USD")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	code, raw := s.do(t, "org-a", "alice", http.MethodPost, "/v1/proposals/"+prop.String()+"/decision", map[string]string{"decision": "accept"})
	if code != http.StatusConflict || !strings.Contains(string(raw), "stale_proposal") {
		t.Fatalf("decision = %d %s", code, raw)
	}
	var one struct {
		State string `json:"state"`
	}
	s.must(t, "org-a", "u", http.MethodGet, "/v1/proposals/"+prop.String(), nil, http.StatusOK, &one)
	if one.State != "proposed" {
		t.Fatalf("state = %s", one.State)
	}
	if actions := s.auditActions(t, inv); len(actions) != 1 || actions[0] != "invoice.fields_changed" {
		t.Fatalf("audit = %v (only the human's own correction)", actions)
	}
}

func TestProposalsAreTenantIsolated(t *testing.T) {
	s := newPropStack(t)
	inv, prop := s.invoiceWithProposal(t)

	var list struct {
		Items []any `json:"items"`
	}
	s.must(t, "org-b", "mallory", http.MethodGet, "/v1/proposals", nil, http.StatusOK, &list)
	if len(list.Items) != 0 {
		t.Fatalf("firm B sees %d of firm A's proposals", len(list.Items))
	}
	if code, _ := s.do(t, "org-b", "mallory", http.MethodGet, "/v1/proposals/"+prop.String(), nil); code != http.StatusNotFound {
		t.Errorf("GET other firm's proposal = %d", code)
	}
	code, _ := s.do(t, "org-b", "mallory", http.MethodPost, "/v1/proposals/"+prop.String()+"/decision", map[string]string{"decision": "accept"})
	if code != http.StatusNotFound {
		t.Errorf("decide other firm's proposal = %d", code)
	}
	if d := s.invoice(t, "org-a", inv); d.PayloadVersion != 1 || d.Payload["currency"] != "aed" {
		t.Fatalf("invoice = %+v", d)
	}
}

func TestProposalsRejectBadRequests(t *testing.T) {
	s := newPropStack(t)
	_, prop := s.invoiceWithProposal(t)
	path := "/v1/proposals/" + prop.String() + "/decision"
	for name, tc := range map[string]struct {
		method, path string
		body         any
		want         int
		code         string
	}{
		"unknown decision": {"POST", path, map[string]string{"decision": "maybe"}, 400, "bad_request"},
		"unknown field":    {"POST", path, `{"decision":"accept","by":"me"}`, 400, "bad_request"},
		"trailing data":    {"POST", path, `{"decision":"accept"} {}`, 400, "bad_request"},
		"reason too long":  {"POST", path, map[string]string{"decision": "reject", "reason": strings.Repeat("ع", 501)}, 400, "bad_request"},
		"not a uuid":       {"POST", "/v1/proposals/nope/decision", map[string]string{"decision": "accept"}, 404, "not_found"},
		"unknown id":       {"POST", "/v1/proposals/" + uuid.NewString() + "/decision", map[string]string{"decision": "accept"}, 404, "not_found"},
		"bad state":        {"GET", "/v1/proposals?state=open", nil, 400, "bad_query"},
		"bad kind":         {"GET", "/v1/proposals?kind=Nope", nil, 400, "bad_query"},
		"bad target id":    {"GET", "/v1/proposals?target_id=x", nil, 400, "bad_query"},
		"bad limit":        {"GET", "/v1/proposals?limit=0", nil, 400, "bad_query"},
		"bad cursor":       {"GET", "/v1/proposals?cursor=!!!", nil, 400, "bad_query"},
		"detail not uuid":  {"GET", "/v1/proposals/nope", nil, 404, "not_found"},
	} {
		code, raw := s.do(t, "org-a", "alice", tc.method, tc.path, tc.body)
		if code != tc.want || !strings.Contains(string(raw), tc.code) {
			t.Errorf("%s: %d %s, want %d %s", name, code, bytes.TrimSpace(raw), tc.want, tc.code)
		}
	}
	// None of it decided the proposal.
	var one struct {
		State string `json:"state"`
	}
	s.must(t, "org-a", "u", http.MethodGet, "/v1/proposals/"+prop.String(), nil, http.StatusOK, &one)
	if one.State != "proposed" {
		t.Fatalf("state = %s", one.State)
	}
}

func TestProposalsListPaginates(t *testing.T) {
	s := newPropStack(t)
	for range 3 {
		s.invoiceWithProposal(t)
	}
	var seen = map[uuid.UUID]bool{}
	cursor := ""
	for page := 0; page < 5; page++ {
		var out struct {
			Items []struct {
				ID uuid.UUID `json:"id"`
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}
		path := "/v1/proposals?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		s.must(t, "org-a", "u", http.MethodGet, path, nil, http.StatusOK, &out)
		for _, i := range out.Items {
			if seen[i.ID] {
				t.Fatalf("proposal %s listed twice", i.ID)
			}
			seen[i.ID] = true
		}
		if out.NextCursor == nil {
			break
		}
		cursor = *out.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("saw %d proposals, want 3", len(seen))
	}
}
