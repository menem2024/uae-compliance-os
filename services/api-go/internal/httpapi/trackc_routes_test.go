package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/trackc"
)

type namedLimiter struct {
	ok    bool
	calls int
}

func (l *namedLimiter) Allow(context.Context, string) (bool, error) { l.calls++; return l.ok, nil }

// trackCRouter mounts WithTrackC over a module without a database: every request these tests send is
// rejected before the first query.
func trackCRouter(read, write *namedLimiter) http.Handler {
	fs := fakeStore{firm: uuid.New()}
	m := &trackc.Module{Firms: fs, ReadLimiter: read, WriteLimiter: write}
	return NewRouter(fakeVerifier{}, fs, &fakePub{}, fakeLimiter{ok: true}, func(context.Context) error { return nil }, WithTrackC(m))
}

func errBody(rec *httptest.ResponseRecorder) string { return strings.TrimSpace(rec.Body.String()) }

func TestTrackCRoutesRequireAuth(t *testing.T) {
	h := trackCRouter(&namedLimiter{ok: true}, &namedLimiter{ok: true})
	id := uuid.NewString()
	for _, r := range [][2]string{
		{"GET", "/v1/invoices"},
		{"GET", "/v1/invoices/" + id + "/validation"},
		{"POST", "/v1/invoices/" + id + "/validation"},
		{"GET", "/v1/invoices/" + id + "/validation/runs"},
		{"GET", "/v1/invoices/" + id + "/validation/runs/" + id},
		{"GET", "/v1/invoices/" + id + "/validation/runs/" + id + "/diff"},
		{"POST", "/v1/invoices/" + id + "/validation/corrections"},
		{"POST", "/v1/invoices/" + id + "/validation/approve"},
		{"POST", "/v1/invoices/" + id + "/validation/fixes"},
		{"GET", "/v1/invoices/" + id + "/validation/audit"},
		{"POST", "/v1/exports"},
		{"GET", "/v1/exports?invoice_id=" + id},
		{"GET", "/v1/exports/" + id},
		{"GET", "/v1/exports/" + id + "/xml"},
	} {
		if rec := do(h, r[0], r[1], "", ""); rec.Code != 401 {
			t.Errorf("%s %s without token: %d", r[0], r[1], rec.Code)
		}
		if rec := do(h, r[0], r[1], "tok-x", ""); rec.Code != 403 || errBody(rec) != `{"error":"unknown_organisation"}` {
			t.Errorf("%s %s unknown org: %d %s", r[0], r[1], rec.Code, rec.Body)
		}
	}
}

// GETs spend the c-read budget and mutations the c-write budget, never each other's or Phase 0's.
func TestTrackCRateLimiters(t *testing.T) {
	read, write := &namedLimiter{ok: false}, &namedLimiter{ok: true}
	h := trackCRouter(read, write)
	rec := do(h, "GET", "/v1/invoices", "tok-a", "")
	if rec.Code != 429 || errBody(rec) != `{"error":"rate_limited"}` || rec.Header().Get("Retry-After") != "60" {
		t.Errorf("read limited: %d %s", rec.Code, rec.Body)
	}
	if read.calls != 1 || write.calls != 0 {
		t.Errorf("calls read=%d write=%d", read.calls, write.calls)
	}
	// A write is not stopped by the exhausted read budget (bad body shows the handler ran).
	rec = do(h, "POST", "/v1/exports", "tok-a", "{")
	if rec.Code != 400 || write.calls != 1 || read.calls != 1 {
		t.Errorf("write: %d read=%d write=%d", rec.Code, read.calls, write.calls)
	}
	write.ok = false
	if rec := do(h, "POST", "/v1/exports", "tok-a", "{"); rec.Code != 429 {
		t.Errorf("write limited: %d", rec.Code)
	}
}

func TestTrackCListBadQuery(t *testing.T) {
	h := trackCRouter(&namedLimiter{ok: true}, &namedLimiter{ok: true})
	for _, q := range []string{
		"status=bogus", "limit=0", "limit=101", "limit=abc", "limit=-1", "cursor=!!", "cursor=bm9wZQ",
		"q=" + strings.Repeat("x", 101),
	} {
		rec := do(h, "GET", "/v1/invoices?"+q, "tok-a", "")
		if rec.Code != 400 || errBody(rec) != `{"error":"bad_query"}` {
			t.Errorf("%s: %d %s", q, rec.Code, rec.Body)
		}
	}
	for _, p := range []string{"/v1/exports", "/v1/exports?invoice_id=nope"} {
		rec := do(h, "GET", p, "tok-a", "")
		if rec.Code != 400 || errBody(rec) != `{"error":"bad_query"}` {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestTrackCBadIDsAreNotFound(t *testing.T) {
	h := trackCRouter(&namedLimiter{ok: true}, &namedLimiter{ok: true})
	good := uuid.NewString()
	for _, r := range [][2]string{
		{"GET", "/v1/invoices/nope/validation"},
		{"POST", "/v1/invoices/nope/validation"},
		{"GET", "/v1/invoices/nope/validation/runs"},
		{"GET", "/v1/invoices/" + good + "/validation/runs/nope"},
		{"GET", "/v1/invoices/" + good + "/validation/runs/nope/diff"},
		{"POST", "/v1/invoices/nope/validation/corrections"},
		{"POST", "/v1/invoices/nope/validation/approve"},
		{"POST", "/v1/invoices/nope/validation/fixes"},
		{"GET", "/v1/invoices/nope/validation/audit"},
		{"GET", "/v1/exports/nope"},
		{"GET", "/v1/exports/nope/xml"},
	} {
		rec := do(h, r[0], r[1], "tok-a", `{}`)
		if rec.Code != 404 || errBody(rec) != `{"error":"not_found"}` {
			t.Errorf("%s %s: %d %s", r[0], r[1], rec.Code, rec.Body)
		}
	}
}

func TestTrackCBadBodies(t *testing.T) {
	h := trackCRouter(&namedLimiter{ok: true}, &namedLimiter{ok: true})
	id := uuid.NewString()
	one := `{"path":"invoice_number","old_value":"","new_value":"X"}`
	many := strings.TrimSuffix(strings.Repeat(one+",", 51), ",")
	for name, c := range map[string]struct{ path, body string }{
		"corrections not json":      {"/v1/invoices/" + id + "/validation/corrections", `{`},
		"corrections unknown field": {"/v1/invoices/" + id + "/validation/corrections", `{"payload_version":1,"changes":[` + one + `],"x":1}`},
		"corrections no version":    {"/v1/invoices/" + id + "/validation/corrections", `{"changes":[` + one + `]}`},
		"corrections no changes":    {"/v1/invoices/" + id + "/validation/corrections", `{"payload_version":1,"changes":[]}`},
		"corrections 51 changes":    {"/v1/invoices/" + id + "/validation/corrections", `{"payload_version":1,"changes":[` + many + `]}`},
		"corrections long reason":   {"/v1/invoices/" + id + "/validation/corrections", `{"payload_version":1,"changes":[` + one + `],"reason":"` + strings.Repeat("r", 501) + `"}`},
		"corrections empty path":    {"/v1/invoices/" + id + "/validation/corrections", `{"payload_version":1,"changes":[{"path":"","old_value":"","new_value":"x"}]}`},
		"approve not json":          {"/v1/invoices/" + id + "/validation/approve", `nope`},
		"approve no version":        {"/v1/invoices/" + id + "/validation/approve", `{}`},
		"export not json":           {"/v1/exports", `nope`},
		"export bad invoice id":     {"/v1/exports", `{"invoice_id":"x"}`},
		"export unknown field":      {"/v1/exports", `{"invoice_id":"` + id + `","y":1}`},
	} {
		rec := do(h, "POST", c.path, "tok-a", c.body)
		if rec.Code != 400 || errBody(rec) != `{"error":"bad_request"}` {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestTrackCRouterOptionAuthGroup(t *testing.T) {
	called := false
	opt := RouterOption(func(r chi.Router) {
		called = true
		r.Get("/v1/zz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	})
	h := NewRouter(fakeVerifier{}, fakeStore{firm: uuid.New()}, &fakePub{}, fakeLimiter{ok: true}, func(context.Context) error { return nil }, opt)
	if !called {
		t.Fatal("option not applied")
	}
	if rec := do(h, "GET", "/v1/zz", "", ""); rec.Code != 401 {
		t.Errorf("unauthenticated: %d", rec.Code)
	}
	if rec := do(h, "GET", "/v1/zz", "tok-a", ""); rec.Code != 204 {
		t.Errorf("authenticated: %d", rec.Code)
	}
}
