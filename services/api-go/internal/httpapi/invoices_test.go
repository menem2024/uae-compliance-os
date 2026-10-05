package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
)

type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, raw string) (auth.Principal, error) {
	if raw == "tok-a" {
		return auth.Principal{Subject: "u", OrgID: "org-A"}, nil
	}
	if raw == "tok-x" {
		return auth.Principal{Subject: "u", OrgID: "org-unknown"}, nil
	}
	return auth.Principal{}, auth.ErrUnauthenticated
}

type fakeStore struct{ firm uuid.UUID }

func (f fakeStore) FirmIDForOrg(_ context.Context, org string) (uuid.UUID, error) {
	if org == "org-A" {
		return f.firm, nil
	}
	return uuid.Nil, db.ErrNotFound
}
func (fakeStore) Create(context.Context, uuid.UUID, []byte) (uuid.UUID, error) {
	return uuid.New(), nil
}
func (fakeStore) Get(context.Context, uuid.UUID, uuid.UUID) (InvoiceView, error) {
	return InvoiceView{}, db.ErrNotFound
}
func (f fakeStore) Firm(_ context.Context, org string) (FirmView, error) {
	if org == "org-A" {
		return FirmView{ID: f.firm, Name: "Demo Firm A", BrandColor: nil}, nil
	}
	return FirmView{}, db.ErrNotFound
}

type fakePub struct {
	got []*compliancev1.InvoiceSubmitted
}

func (p *fakePub) PublishSubmitted(_ context.Context, ev *compliancev1.InvoiceSubmitted) error {
	p.got = append(p.got, ev)
	return nil
}

type fakeLimiter struct{ ok bool }

func (l fakeLimiter) Allow(context.Context, string) (bool, error) { return l.ok, nil }

const body = `{"invoice_number":"INV-1","issue_date":"2026-09-27","seller_trn":"123","buyer_trn":"100000000000003","currency":"AED","total_amount":"1050.00","vat_amount":"50.00"}`

func do(h http.Handler, method, path, tok, b string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(b))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestInvoicesAPI(t *testing.T) {
	firm := uuid.New()
	pub := &fakePub{}
	h := NewRouter(fakeVerifier{}, fakeStore{firm: firm}, pub, fakeLimiter{ok: true}, func(context.Context) error { return nil })

	if rec := do(h, "POST", "/v1/invoices", "", body); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
	rec := do(h, "POST", "/v1/invoices", "tok-a", body)
	if rec.Code != 202 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var out struct{ ID, Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(pub.got) != 1 || pub.got[0].InvoiceId != out.ID || pub.got[0].FirmId != firm.String() || out.Status != "uploaded" {
		t.Errorf("event mismatch: %+v vs %+v", pub.got, out)
	}
	if rec := do(h, "POST", "/v1/invoices", "tok-a", "{"); rec.Code != 400 {
		t.Errorf("bad json: %d", rec.Code)
	}
	if rec := do(h, "POST", "/v1/invoices", "tok-a", strings.Replace(body, `"1050.00"`, `"abc"`, 1)); rec.Code != 400 {
		t.Errorf("bad amount: %d", rec.Code)
	}
	if rec := do(h, "POST", "/v1/invoices", "tok-x", body); rec.Code != 403 {
		t.Errorf("unknown org: %d", rec.Code)
	}
	if rec := do(h, "GET", "/v1/invoices/"+uuid.NewString(), "tok-a", ""); rec.Code != 404 {
		t.Errorf("not found: %d", rec.Code)
	}

	limited := &fakePub{}
	h2 := NewRouter(fakeVerifier{}, fakeStore{firm: firm}, limited, fakeLimiter{ok: false}, func(context.Context) error { return nil })
	if rec := do(h2, "POST", "/v1/invoices", "tok-a", body); rec.Code != 429 || len(limited.got) != 0 {
		t.Errorf("rate limit: %d published=%d", rec.Code, len(limited.got))
	}
}

// TestGetInvoiceIsRateLimited: only POST /v1/invoices was rate-limited;
// repeated GETs (each a DB transaction) could generate unbounded backend
// load. GET must be limited too.
func TestGetInvoiceIsRateLimited(t *testing.T) {
	firm := uuid.New()
	h := NewRouter(fakeVerifier{}, fakeStore{firm: firm}, &fakePub{}, fakeLimiter{ok: false}, func(context.Context) error { return nil })
	if rec := do(h, "GET", "/v1/invoices/"+uuid.NewString(), "tok-a", ""); rec.Code != 429 {
		t.Errorf("get invoice: %d, want 429", rec.Code)
	}
}

// capturingStore records the stored payload.
type capturingStore struct {
	fakeStore
	payloads [][]byte
}

func (c *capturingStore) Create(_ context.Context, _ uuid.UUID, p []byte) (uuid.UUID, error) {
	c.payloads = append(c.payloads, p)
	return uuid.New(), nil
}

const fullBody = `{"invoice_number":"INV-2","issue_date":"2026-09-27","seller_trn":"100000000000003","buyer_trn":"100000000000011",` +
	`"currency":"AED","total_amount":"1050.00","vat_amount":"50.00","invoice_type_code":"380","uuid":"u-1",` +
	`"seller":{"name":"Seller LLC","postal_address":{"city":"Dubai","country_code":"AE"}},` +
	`"lines":[{"id":"1","quantity":"2","unit_code":"C62"}]}`

func newCapturingRouter(firm uuid.UUID) (http.Handler, *capturingStore, *fakePub) {
	st := &capturingStore{fakeStore: fakeStore{firm: firm}}
	pub := &fakePub{}
	return NewRouter(fakeVerifier{}, st, pub, fakeLimiter{ok: true}, func(context.Context) error { return nil }), st, pub
}

func TestCreateInvoiceAcceptsAFullCanonicalInvoice(t *testing.T) {
	h, st, pub := newCapturingRouter(uuid.New())
	if rec := do(h, "POST", "/v1/invoices", "tok-a", fullBody); rec.Code != 202 {
		t.Fatalf("full invoice: %d %s", rec.Code, rec.Body)
	}
	if len(pub.got) != 1 {
		t.Fatalf("published %d events", len(pub.got))
	}
	inv := pub.got[0].GetInvoice()
	if inv.GetInvoiceTypeCode() != "380" || inv.GetSeller().GetName() != "Seller LLC" || inv.GetSeller().GetPostalAddress().GetCity() != "Dubai" ||
		len(inv.GetLines()) != 1 || inv.GetLines()[0].GetQuantity() != "2" || inv.GetTotalAmount() != "1050.00" {
		t.Errorf("InvoiceSubmitted does not carry the full invoice: %v", inv)
	}
	// The stored payload uses proto field names (CI §11) and keeps nested data.
	var got map[string]any
	if err := json.Unmarshal(st.payloads[0], &got); err != nil {
		t.Fatal(err)
	}
	if got["invoice_type_code"] != "380" || got["seller_trn"] != "100000000000003" {
		t.Errorf("payload keys: %s", st.payloads[0])
	}
	if _, camel := got["invoiceTypeCode"]; camel {
		t.Errorf("payload uses camelCase JSON names: %s", st.payloads[0])
	}
	if seller, _ := got["seller"].(map[string]any); seller["name"] != "Seller LLC" {
		t.Errorf("seller: %s", st.payloads[0])
	}
}

func TestCreateInvoiceLegacySevenKeyBodyStillWorks(t *testing.T) {
	h, st, pub := newCapturingRouter(uuid.New())
	if rec := do(h, "POST", "/v1/invoices", "tok-a", body); rec.Code != 202 {
		t.Fatalf("legacy: %d %s", rec.Code, rec.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(st.payloads[0], &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"invoice_number", "issue_date", "seller_trn", "buyer_trn", "currency", "total_amount", "vat_amount"} {
		if got[k] == "" {
			t.Errorf("legacy payload lost %s: %s", k, st.payloads[0])
		}
	}
	if pub.got[0].GetInvoice().GetVatAmount() != "50.00" || pub.got[0].GetInvoice().GetInvoiceNumber() != "INV-1" {
		t.Errorf("event invoice = %v", pub.got[0].GetInvoice())
	}
}

func TestCreateInvoiceRejections(t *testing.T) {
	h, st, pub := newCapturingRouter(uuid.New())
	cases := map[string]string{
		"unknown top-level field": strings.Replace(fullBody, `"uuid":"u-1"`, `"uuid":"u-1","bogus":"x"`, 1),
		"unknown nested field":    strings.Replace(fullBody, `"city":"Dubai"`, `"city":"Dubai","bogus":"x"`, 1),
		"trailing data":           fullBody + ` {}`,
		"non-decimal total":       strings.Replace(fullBody, `"1050.00"`, `"abc"`, 1),
		"non-decimal vat":         strings.Replace(fullBody, `"50.00"`, `"x"`, 1),
		"missing totals":          `{"invoice_number":"INV-3"}`,
		"number for a string":     strings.Replace(fullBody, `"50.00"`, `50.00`, 1),
		"not json":                `{`,
		"empty body":              ``,
		"too large":               `{"invoice_number":"` + strings.Repeat("a", MaxBodyBytes) + `"}`,
	}
	for name, b := range cases {
		if rec := do(h, "POST", "/v1/invoices", "tok-a", b); rec.Code != 400 {
			t.Errorf("%s: %d, want 400 (%s)", name, rec.Code, rec.Body)
		}
	}
	if len(st.payloads) != 0 || len(pub.got) != 0 {
		t.Errorf("stored %d, published %d on rejected bodies", len(st.payloads), len(pub.got))
	}
}
