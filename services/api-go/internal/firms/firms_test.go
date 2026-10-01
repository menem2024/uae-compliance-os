package firms_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/firms"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

type memStore struct{ v firms.View }

func (m *memStore) Get(context.Context, uuid.UUID) (firms.View, error) { return m.v, nil }

func (m *memStore) Update(_ context.Context, _ uuid.UUID, p firms.Patch) (firms.View, error) {
	next, err := firms.Apply(m.v, p)
	if err != nil {
		return m.v, err
	}
	m.v = next
	return next, nil
}

func patch(t *testing.T, h http.Handler, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/v1/firm", strings.NewReader(body))
	req = req.WithContext(httpx.WithFirmID(req.Context(), uuid.New()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestPatchDistinguishesAbsentFromNull(t *testing.T) {
	gold := "#C8A45D"
	store := &memStore{v: firms.View{Name: "Firm", BrandColor: &gold}}
	r := chi.NewRouter()
	pass := func(next http.Handler) http.Handler { return next }
	firms.NewHandler(store).Mount(r, pass, pass)

	if code, body := patch(t, r, `{"name":"  New   Name "}`); code != http.StatusOK || body["name"] != "New Name" || body["brand_color"] != gold {
		t.Fatalf("absent colour kept: %d %v", code, body)
	}
	if code, body := patch(t, r, `{"brand_color":"#0a7c66"}`); code != http.StatusOK || body["brand_color"] != "#0A7C66" {
		t.Fatalf("set colour: %d %v", code, body)
	}
	if code, body := patch(t, r, `{"brand_color":null}`); code != http.StatusOK || body["brand_color"] != nil {
		t.Fatalf("null clears: %d %v", code, body)
	}
	for body, want := range map[string]string{
		`{"brand_color":"red"}`:                       "invalid_brand_color",
		`{"brand_color":"#12345"}`:                    "invalid_brand_color",
		`{"brand_color":"#123456;background:url(x)"}`: "invalid_brand_color",
		`{"name":""}`:                                 "invalid_name",
		`{"name":"a","logo":"x"}`:                     "invalid_json",
	} {
		if code, out := patch(t, r, body); code != http.StatusBadRequest || out["error"] != want {
			t.Errorf("%s: %d %v", body, code, out)
		}
	}
}
