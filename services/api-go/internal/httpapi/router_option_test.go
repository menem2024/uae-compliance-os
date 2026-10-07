package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRouterOptionMountsInsideAuthGroup(t *testing.T) {
	opt := func(r chi.Router) {
		r.Get("/v1/extra", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	}
	h := NewRouter(fakeVerifier{}, nil, nil, nil, func(context.Context) error { return nil }, opt)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/extra", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request reached the option's route: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/extra", nil)
	req.Header.Set("Authorization", "Bearer tok-a")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("authenticated request: %d", rec.Code)
	}
}
