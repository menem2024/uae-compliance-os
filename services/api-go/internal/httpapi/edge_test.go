package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type okStore struct{ fakeStore }

func (okStore) Get(_ context.Context, _ uuid.UUID, id uuid.UUID) (InvoiceView, error) {
	rv := "pint-ae@0.0-skeleton"
	return InvoiceView{ID: id.String(), Status: "has_issues", RulesetVersion: &rv,
		Issues: []IssueView{{RuleID: "AE-TRN-001", Severity: "error", Path: "seller_trn", Message: "bad"}}}, nil
}

func TestEdgeCases(t *testing.T) {
	firm := uuid.New()
	pub := &fakePub{}
	ok := func(context.Context) error { return nil }
	h := NewRouter(fakeVerifier{}, okStore{fakeStore{firm: firm}}, pub, fakeLimiter{ok: true}, ok)

	if rec := do(h, "GET", "/healthz", "", ""); rec.Code != 200 {
		t.Errorf("healthz: %d", rec.Code)
	}
	if rec := do(h, "GET", "/readyz", "", ""); rec.Code != 200 {
		t.Errorf("readyz: %d", rec.Code)
	}
	bad := NewRouter(fakeVerifier{}, fakeStore{firm: firm}, pub, fakeLimiter{ok: true}, func(context.Context) error { return errors.New("down") })
	if rec := do(bad, "GET", "/readyz", "", ""); rec.Code != 503 {
		t.Errorf("readyz down: %d", rec.Code)
	}
	if rec := do(h, "POST", "/v1/invoices", "bogus", body); rec.Code != 401 {
		t.Errorf("invalid token: %d", rec.Code)
	}
	if rec := do(h, "POST", "/v1/invoices", "tok-a", strings.Replace(body, `}`, `,"extra":"x"}`, 1)); rec.Code != 400 {
		t.Errorf("unknown field: %d", rec.Code)
	}
	if rec := do(h, "POST", "/v1/invoices", "tok-a", strings.Replace(body, `"50.00"`, `50.00`, 1)); rec.Code != 400 {
		t.Errorf("numeric (non-string) amount: %d", rec.Code)
	}
	if rec := do(h, "POST", "/v1/invoices", "tok-a", strings.Replace(body, `"50.00"`, `""`, 1)); rec.Code != 400 {
		t.Errorf("empty vat: %d", rec.Code)
	}
	if rec := do(h, "POST", "/v1/invoices", "tok-a", body+`{}`); rec.Code != 400 {
		t.Errorf("trailing data: %d", rec.Code)
	}
	if len(pub.got) != 0 {
		t.Errorf("published on rejected requests: %d", len(pub.got))
	}
	if rec := do(h, "GET", "/v1/invoices/not-a-uuid", "tok-a", ""); rec.Code != 400 {
		t.Errorf("bad uuid: %d", rec.Code)
	}
	id := uuid.NewString()
	rec := do(h, "GET", "/v1/invoices/"+id, "tok-a", "")
	if rec.Code != 200 {
		t.Fatalf("get: %d", rec.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	issues, _ := got["issues"].([]any)
	if got["id"] != id || got["status"] != "has_issues" || got["ruleset_version"] != "pint-ae@0.0-skeleton" || len(issues) != 1 {
		t.Errorf("get body: %s", rec.Body)
	}
	if first, _ := issues[0].(map[string]any); first["rule_id"] != "AE-TRN-001" || first["severity"] != "error" {
		t.Errorf("issue shape: %v", issues[0])
	}
}

func TestTraceIDHeader(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev); _ = tp.Shutdown(context.Background()) })

	h := NewRouter(fakeVerifier{}, fakeStore{firm: uuid.New()}, &fakePub{}, fakeLimiter{ok: true}, func(context.Context) error { return nil })
	rec := do(h, "POST", "/v1/invoices", "tok-a", body)
	if rec.Code != 202 || len(rec.Header().Get("X-Trace-Id")) != 32 {
		t.Errorf("X-Trace-Id = %q (code %d)", rec.Header().Get("X-Trace-Id"), rec.Code)
	}
}
