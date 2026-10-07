package trackc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/fixapply"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

type recLimiter struct {
	keys []string
	ok   bool
	err  error
}

func (l *recLimiter) Allow(_ context.Context, key string) (bool, error) {
	l.keys = append(l.keys, key)
	return l.ok, l.err
}

type orgs map[string]uuid.UUID

func (o orgs) FirmIDForOrg(_ context.Context, org string) (uuid.UUID, error) {
	if id, ok := o[org]; ok {
		return id, nil
	}
	if org == "boom" {
		return uuid.Nil, errors.New("db down")
	}
	return uuid.Nil, db.ErrNotFound
}

// NamedLimiter keys must equal Track B's ratelimit.NewNamed layout (rl:<name>:<firm>:<minute>) once the
// inner limiter adds "rl:" and the minute: the name sits before the firm id.
func TestNamedLimiterKeyspace(t *testing.T) {
	inner := &recLimiter{ok: true}
	l := NamedLimiter("c-read", inner)
	if ok, err := l.Allow(context.Background(), "firm-1"); !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(inner.keys) != 1 || inner.keys[0] != "c-read:firm-1" {
		t.Errorf("keys = %v", inner.keys)
	}
}

func serve(t *testing.T, fr FirmResolver, l Limiter, org string) (*httptest.ResponseRecorder, uuid.UUID) {
	t.Helper()
	var seen uuid.UUID
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = FirmFrom(r.Context()) })
	req := httptest.NewRequest("GET", "/x", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{Subject: "u", OrgID: org}))
	rec := httptest.NewRecorder()
	FirmMiddleware(fr, l)(next).ServeHTTP(rec, req)
	return rec, seen
}

func TestFirmMiddleware(t *testing.T) {
	firm := uuid.New()
	fr := orgs{"org-a": firm}
	l := &recLimiter{ok: true}
	rec, seen := serve(t, fr, l, "org-a")
	if rec.Code != 200 || seen != firm || len(l.keys) != 1 || l.keys[0] != firm.String() {
		t.Errorf("ok path: code=%d seen=%v keys=%v", rec.Code, seen, l.keys)
	}

	for name, c := range map[string]struct {
		org  string
		lim  *recLimiter
		code int
		body string
	}{
		"unknown org":     {"org-x", &recLimiter{ok: true}, 403, `{"error":"unknown_organisation"}`},
		"resolver error":  {"boom", &recLimiter{ok: true}, 500, `{"error":"internal"}`},
		"limited":         {"org-a", &recLimiter{ok: false}, 429, `{"error":"rate_limited"}`},
		"limiter failing": {"org-a", &recLimiter{err: errors.New("valkey")}, 503, `{"error":"rate_limiter_unavailable"}`},
	} {
		rec, seen := serve(t, fr, c.lim, c.org)
		if rec.Code != c.code || seen != uuid.Nil || rec.Body.String() != c.body+"\n" {
			t.Errorf("%s: code=%d seen=%v body=%q", name, rec.Code, seen, rec.Body.String())
		}
		if name == "limited" && rec.Header().Get("Retry-After") != "60" {
			t.Errorf("limited: Retry-After = %q", rec.Header().Get("Retry-After"))
		}
	}
}

func TestWireProposalsRegistersEveryKindAndMountsTheRoutes(t *testing.T) {
	m := &Module{}
	wireProposals(m)
	if len(m.Mounts) != 1 || m.Proposals == nil {
		t.Fatalf("module = %+v", m)
	}
	for _, kind := range []string{proposals.KindDocumentAttribution, fixapply.KindFieldFix} {
		if _, ok := m.Proposals.Get(kind); !ok {
			t.Errorf("no Applier registered for %s", kind)
		}
	}
	if _, ok := m.Proposals.Get("invoice.other"); ok {
		t.Error("an unknown kind has an Applier")
	}
}

func TestWireFixesIsANoOpUntilTask20(t *testing.T) {
	m := &Module{}
	wireFixes(m)
	if m.Fixes != nil {
		t.Errorf("wireFixes changed the module: %+v", m)
	}
}
