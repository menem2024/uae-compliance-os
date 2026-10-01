package agents_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/menem2024/uae-platform/services/api-go/internal/agents"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

func passthrough(next http.Handler) http.Handler { return next }

func withFirm(firm uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithFirmID(r.Context(), firm)))
		})
	}
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var v struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return v.Error
}

func TestListRunsBadQuery(t *testing.T) {
	h := agents.NewHandler(nil, agents.NewHub(20, 64))
	r := chi.NewRouter()
	h.Mount(r, withFirm(uuid.New()), passthrough)

	for _, tc := range []struct {
		name, query, code string
	}{
		{"bad status", "status=not-a-status", "invalid_status"},
		{"zero limit", "limit=0", "invalid_limit"},
		{"too large limit", "limit=999", "invalid_limit"},
		{"non numeric limit", "limit=abc", "invalid_limit"},
		{"bad cursor", "cursor=not-a-valid-cursor", "invalid_cursor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/agents/runs?"+tc.query, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}
			if got := errorCode(t, w.Body.Bytes()); got != tc.code {
				t.Fatalf("error = %q, want %q", got, tc.code)
			}
		})
	}
}

func TestRunDetailMalformedIDIsNotFound(t *testing.T) {
	h := agents.NewHandler(nil, agents.NewHub(20, 64))
	r := chi.NewRouter()
	h.Mount(r, withFirm(uuid.New()), passthrough)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/runs/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if got := errorCode(t, w.Body.Bytes()); got != "not_found" {
		t.Fatalf("error = %q, want not_found", got)
	}
}

func Test21stConcurrentSubscribeGets429(t *testing.T) {
	hub := agents.NewHub(20, 64)
	firm := uuid.New()
	for i := 0; i < 20; i++ {
		if _, err := hub.Subscribe(firm); err != nil {
			t.Fatalf("filling subscriber %d: %v", i, err)
		}
	}
	h := agents.NewHandler(nil, hub)
	r := chi.NewRouter()
	h.Mount(r, withFirm(firm), passthrough)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/activity", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if got := errorCode(t, w.Body.Bytes()); got != "too_many_streams" {
		t.Fatalf("error = %q, want too_many_streams", got)
	}
}

// seedStep inserts one running agent_runs row and one succeeded agent_steps row directly (both tables are
// FORCE RLS, so app.firm_id must be set transaction-locally, like dbtest.ClientCompany).
func seedStep(t *testing.T, env dbtest.Env, firm uuid.UUID) (runID, stepID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	runID, stepID = uuid.New(), uuid.New()
	err := pgx.BeginFunc(ctx, env.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, firm.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_runs (id, firm_id, subject_type, subject_id, status, started_at)
			VALUES ($1, $2, 'document', $3, 'running', now())`, runID, firm, uuid.NewString()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agent_steps
			(id, firm_id, run_id, seq, node_id, agent, action, kind, status, attempt, at)
			VALUES ($1, $2, $3, 1, 'fetch', 'orchestrator', 'fetch', 'deterministic', 'succeeded', 1, now())`,
			stepID, firm, runID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return runID, stepID
}

// startActivityServer mounts the handler behind a real http.Server with short Read/WriteTimeout, as the
// production server (cmd/api/main.go) sets 20s timeouts (F1). It returns the server and a client request
// carrying firm's identity.
func startActivityServer(t *testing.T, h *agents.Handler, firm uuid.UUID) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	h.Mount(r, withFirm(firm), passthrough)
	srv := httptest.NewUnstartedServer(r)
	srv.Config.ReadTimeout = 200 * time.Millisecond
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestActivitySurvivesShortServerTimeouts(t *testing.T) {
	env := dbtest.Setup(t)
	h := agents.NewHandler(env.App, agents.NewHub(20, 64))
	h.SetKeepAlive(100 * time.Millisecond)
	srv := startActivityServer(t, h, env.FirmA)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/agents/activity", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	lines := make(chan string, 256)
	done := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		done <- sc.Err()
	}()

	gotPing := false
	deadline := time.After(1 * time.Second)
loop:
	for {
		select {
		case line := <-lines:
			if strings.Contains(line, "ping") {
				gotPing = true
			}
		case err := <-done:
			t.Fatalf("stream closed early (ReadTimeout/WriteTimeout not cleared?): %v", err)
		case <-deadline:
			break loop
		}
	}
	if !gotPing {
		t.Fatal("expected at least one keep-alive ping within 1s past the 200ms server timeouts")
	}
}

func TestActivityLastEventIDBackfill(t *testing.T) {
	env := dbtest.Setup(t)
	_, stepID := seedStep(t, env, env.FirmA)

	h := agents.NewHandler(env.App, agents.NewHub(20, 64))
	h.SetKeepAlive(5 * time.Second) // keep pings out of the way of this assertion
	srv := startActivityServer(t, h, env.FirmA)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/agents/activity", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "0-"+stepID.String())
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var buf bytes.Buffer
	var mu sync.Mutex
	go func() {
		b := make([]byte, 4096)
		for {
			n, rerr := resp.Body.Read(b)
			if n > 0 {
				mu.Lock()
				buf.Write(b[:n])
				mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	body := buf.String()
	mu.Unlock()

	if !strings.Contains(body, "event: step") {
		t.Fatalf("backfill did not include a step event:\n%s", body)
	}
	if !strings.Contains(body, "id: ") || !strings.Contains(body, stepID.String()) {
		t.Fatalf("backfill did not carry the seeded step's id in an SSE id: line:\n%s", body)
	}
}
