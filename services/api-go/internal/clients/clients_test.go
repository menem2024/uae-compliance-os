package clients_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/clients"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

func sp(s string) *string { return &s }

func TestMergeValidates(t *testing.T) {
	for in, code := range map[clients.Input]string{
		{Name: sp("  ")}:                                      clients.CodeInvalidName,
		{Name: sp(strings.Repeat("x", 201))}:                  clients.CodeInvalidName,
		{Name: sp("A"), TRN: sp("12345678901234")}:            clients.CodeInvalidTRN,
		{Name: sp("A"), TRN: sp("١٠٠٢٣٤٥٦٧٨٠٠٠٠٣")}:           clients.CodeInvalidTRN,
		{Name: sp("A"), Emirate: sp("DUBAI")}:                 clients.CodeInvalidEmirate,
		{Name: sp("A"), NameAr: sp(strings.Repeat("ب", 201))}: clients.CodeInvalidNameAr,
	} {
		_, err := clients.Merge(clients.Fields{}, in)
		if ve, ok := err.(clients.ValidationError); !ok || ve.Code != code {
			t.Errorf("%+v: err=%v want %s", in, err, code)
		}
	}
	f, err := clients.Merge(clients.Fields{}, clients.Input{Name: sp(" Oasis \t Trading\x00 "), TRN: sp("100234567800003"), Emirate: sp("dxb")})
	if err != nil || f.Name != "Oasis Trading" || f.Emirate != "DXB" {
		t.Fatalf("%+v %v", f, err)
	}
	kept, err := clients.Merge(f, clients.Input{TRN: sp("")})
	if err != nil || kept.Name != "Oasis Trading" || kept.TRN != "" || kept.Emirate != "DXB" {
		t.Fatalf("patch semantics: %+v %v", kept, err)
	}
}

// memStore is a per-Firm in-memory Store.
type memStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]map[uuid.UUID]clients.View
}

func (m *memStore) firm(id uuid.UUID) map[uuid.UUID]clients.View {
	if m.rows == nil {
		m.rows = map[uuid.UUID]map[uuid.UUID]clients.View{}
	}
	if m.rows[id] == nil {
		m.rows[id] = map[uuid.UUID]clients.View{}
	}
	return m.rows[id]
}

func toView(id uuid.UUID, f clients.Fields, status string) clients.View {
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	return clients.View{ID: id, Name: f.Name, NameAr: f.NameAr, TRN: opt(f.TRN), Emirate: opt(f.Emirate), Status: status, CreatedAt: time.Unix(0, 0)}
}

func (m *memStore) List(_ context.Context, firmID uuid.UUID, f clients.ListFilter) ([]clients.View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []clients.View
	for _, v := range m.firm(firmID) {
		if (f.Status == "all" || v.Status == f.Status) && (f.AfterID == uuid.Nil || v.Name > f.AfterName) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (m *memStore) Create(_ context.Context, firmID uuid.UUID, f clients.Fields) (clients.View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.firm(firmID) {
		if f.TRN != "" && v.TRN != nil && *v.TRN == f.TRN {
			return clients.View{}, clients.ErrTRNTaken
		}
	}
	v := toView(uuid.New(), f, "active")
	m.firm(firmID)[v.ID] = v
	return v, nil
}

func (m *memStore) Get(_ context.Context, firmID, id uuid.UUID) (clients.View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.firm(firmID)[id]
	if !ok {
		return v, db.ErrNotFound
	}
	return v, nil
}

func (m *memStore) Update(ctx context.Context, firmID, id uuid.UUID, in clients.Input) (clients.View, error) {
	cur, err := m.Get(ctx, firmID, id)
	if err != nil {
		return cur, err
	}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	f, err := clients.Merge(clients.Fields{Name: cur.Name, NameAr: cur.NameAr, TRN: deref(cur.TRN), Emirate: deref(cur.Emirate)}, in)
	if err != nil {
		return cur, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v := toView(id, f, cur.Status)
	m.firm(firmID)[id] = v
	return v, nil
}

func (m *memStore) SetStatus(ctx context.Context, firmID, id uuid.UUID, status string) (clients.View, error) {
	cur, err := m.Get(ctx, firmID, id)
	if err != nil {
		return cur, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur.Status = status
	m.firm(firmID)[id] = cur
	return cur, nil
}

func server(store clients.Store) *chi.Mux {
	r := chi.NewRouter()
	pass := func(next http.Handler) http.Handler { return next }
	clients.NewHandler(store).Mount(r, pass, pass)
	return r
}

func call(t *testing.T, h http.Handler, firm uuid.UUID, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(httpx.WithFirmID(req.Context(), firm))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestHandlersCRUDAndIsolation(t *testing.T) {
	h := server(&memStore{})
	a, b := uuid.New(), uuid.New()
	code, body := call(t, h, a, http.MethodPost, "/v1/client-companies", `{"name":"Oasis","trn":"100234567800003","emirate":"DXB"}`)
	if code != http.StatusCreated || body["status"] != "active" || body["trn"] != "100234567800003" {
		t.Fatalf("create: %d %v", code, body)
	}
	id := body["id"].(string)
	if code, body = call(t, h, a, http.MethodPost, "/v1/client-companies", `{"name":"Dup","trn":"100234567800003"}`); code != http.StatusConflict || body["error"] != "trn_taken" {
		t.Fatalf("dup: %d %v", code, body)
	}
	if code, body = call(t, h, a, http.MethodPost, "/v1/client-companies", `{"name":"X","trn":"12"}`); code != http.StatusBadRequest || body["error"] != "invalid_trn" {
		t.Fatalf("invalid: %d %v", code, body)
	}
	if code, _ = call(t, h, a, http.MethodPost, "/v1/client-companies", `{"name":"X","extra":1}`); code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", code)
	}
	if code, _ = call(t, h, b, http.MethodGet, "/v1/client-companies/"+id, ""); code != http.StatusNotFound {
		t.Fatalf("firm B sees firm A's client: %d", code)
	}
	if code, body = call(t, h, a, http.MethodPatch, "/v1/client-companies/"+id, `{"name_ar":"الواحة"}`); code != http.StatusOK || body["name"] != "Oasis" || body["name_ar"] != "الواحة" {
		t.Fatalf("patch: %d %v", code, body)
	}
	if code, body = call(t, h, a, http.MethodPost, "/v1/client-companies/"+id+"/archive", ""); code != http.StatusOK || body["status"] != "archived" {
		t.Fatalf("archive: %d %v", code, body)
	}
	if code, _ = call(t, h, a, http.MethodGet, "/v1/client-companies/not-a-uuid", ""); code != http.StatusNotFound {
		t.Fatalf("bad id: %d", code)
	}
}

func TestListPagesWithCursor(t *testing.T) {
	store := &memStore{}
	h := server(store)
	firm := uuid.New()
	for _, n := range []string{"a", "b", "c"} {
		if code, _ := call(t, h, firm, http.MethodPost, "/v1/client-companies", `{"name":"`+n+`"}`); code != http.StatusCreated {
			t.Fatal(code)
		}
	}
	code, body := call(t, h, firm, http.MethodGet, "/v1/client-companies?limit=2", "")
	if code != http.StatusOK || len(body["items"].([]any)) != 2 || body["next_cursor"] == nil {
		t.Fatalf("page 1: %d %v", code, body)
	}
	_, body = call(t, h, firm, http.MethodGet, "/v1/client-companies?limit=2&cursor="+body["next_cursor"].(string), "")
	if items := body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["name"] != "c" || body["next_cursor"] != nil {
		t.Fatalf("page 2: %v", body)
	}
	for _, q := range []string{"status=gone", "limit=0", "cursor=@@"} {
		if code, _ := call(t, h, firm, http.MethodGet, "/v1/client-companies?"+q, ""); code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, code)
		}
	}
}
