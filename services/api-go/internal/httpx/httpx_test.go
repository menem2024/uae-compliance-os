package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

type resolver map[string]uuid.UUID

func (r resolver) FirmIDForOrg(_ context.Context, org string) (uuid.UUID, error) {
	if id, ok := r[org]; ok {
		return id, nil
	}
	return uuid.Nil, db.ErrNotFound
}

type limiter struct {
	allow bool
	err   error
}

func (l limiter) Allow(context.Context, string) (bool, error) { return l.allow, l.err }

func TestDecodeJSONIsStrict(t *testing.T) {
	var v struct{ Name string }
	for body, ok := range map[string]bool{`{"Name":"a"}`: true, `{"Name":"a","x":1}`: false, `{"Name":"a"} {}`: false, `[`: false} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		err := httpx.DecodeJSON(httptest.NewRecorder(), r, &v)
		if (err == nil) != ok || (err != nil && !errors.Is(err, httpx.ErrBadJSON)) {
			t.Errorf("%s: err=%v", body, err)
		}
	}
}

func TestFirmMiddleware(t *testing.T) {
	firm := uuid.New()
	var seen uuid.UUID
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = httpx.FirmFrom(r.Context()) })
	cases := []struct {
		org   string
		l     limiter
		code  int
		error string
	}{
		{"org", limiter{allow: true}, 200, ""},
		{"nope", limiter{allow: true}, 403, "unknown_organisation"},
		{"org", limiter{allow: false}, 429, "rate_limited"},
		{"org", limiter{err: errors.New("down")}, 503, "rate_limiter_unavailable"},
	}
	for _, c := range cases {
		seen = uuid.Nil
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{OrgID: c.org}))
		rec := httptest.NewRecorder()
		httpx.Firm(resolver{"org": firm}, c.l)(next).ServeHTTP(rec, req)
		if rec.Code != c.code || (c.error != "" && !strings.Contains(rec.Body.String(), `"`+c.error+`"`)) {
			t.Errorf("%+v: got %d %s", c, rec.Code, rec.Body.String())
		}
		if c.code == 200 && seen != firm {
			t.Error("firm not in context")
		}
		if c.code == 429 && rec.Header().Get("Retry-After") != "60" {
			t.Error("missing Retry-After")
		}
	}
}

func TestCursorsAndPattern(t *testing.T) {
	id := uuid.New()
	at := time.Date(2026, 9, 29, 10, 0, 0, 123456000, time.UTC)
	gotT, gotID, err := httpx.ParseTimeCursor(httpx.TimeCursor(at, id))
	if err != nil || !gotT.Equal(at) || gotID != id {
		t.Fatalf("time cursor: %v %v %v", gotT, gotID, err)
	}
	name, nid, err := httpx.ParseNameCursor(httpx.NameCursor("الواحة|x", id))
	if err != nil || name != "الواحة|x" || nid != id {
		t.Fatalf("name cursor: %q %v %v", name, nid, err)
	}
	if _, _, err := httpx.ParseTimeCursor("!!"); err == nil {
		t.Fatal("garbage cursor accepted")
	}
	if got := httpx.ContainsPattern(`50%_a\b`); got != `%50\%\_a\\b%` {
		t.Fatalf("pattern %q", got)
	}
	r := httptest.NewRequest(http.MethodGet, "/?limit=101", nil)
	if _, err := httpx.PageLimit(r, 50, 100); err == nil {
		t.Fatal("limit over max accepted")
	}
}
