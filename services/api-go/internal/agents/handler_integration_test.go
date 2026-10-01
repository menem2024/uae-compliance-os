//go:build integration

package agents_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/menem2024/uae-platform/services/api-go/internal/agents"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

// seedRunDetail inserts, directly by SQL (both tables are FORCE RLS, so app.firm_id is set
// transaction-locally), one running agent_runs row with two agent_steps and one open proposal.
func seedRunDetail(t *testing.T, env dbtest.Env, firm uuid.UUID) (runID, step1, step2 uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	runID, step1, step2 = uuid.New(), uuid.New(), uuid.New()
	doc := uuid.New()
	err := pgx.BeginFunc(ctx, env.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.firm_id', $1, true)`, firm.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_runs (id, firm_id, subject_type, subject_id, status, started_at)
			VALUES ($1, $2, 'document', $3, 'running', now())`, runID, firm, doc.String()); err != nil {
			return err
		}
		// Inserted out of seq order: the handler must return them ordered by seq regardless.
		if _, err := tx.Exec(ctx, `INSERT INTO agent_steps
			(id, firm_id, run_id, seq, node_id, agent, action, kind, status, attempt, at)
			VALUES ($1, $2, $3, 2, 'extract', 'extraction', 'extract', 'llm', 'succeeded', 1, now())`,
			step2, firm, runID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_steps
			(id, firm_id, run_id, seq, node_id, agent, action, kind, status, attempt, at)
			VALUES ($1, $2, $3, 1, 'fetch', 'orchestrator', 'fetch', 'deterministic', 'succeeded', 1, now())`,
			step1, firm, runID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO proposals
			(id, firm_id, run_id, agent, kind, target_type, target_id, summary_key, confidence)
			VALUES ($1, $2, $3, 'intake', 'document.attribution', 'document', $4, 'k', 0.9)`,
			uuid.New(), firm, runID, doc)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return runID, step1, step2
}

func mount(h *agents.Handler, firm uuid.UUID) http.Handler {
	r := chi.NewRouter()
	readMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(httpx.WithFirmID(req.Context(), firm)))
		})
	}
	h.Mount(r, readMW, readMW)
	return r
}

func TestRunDetailNestedAndOrderedBySeq(t *testing.T) {
	env := dbtest.Setup(t)
	runID, step1, step2 := seedRunDetail(t, env, env.FirmA)

	h := agents.NewHandler(env.App, agents.NewHub(20, 64))
	router := mount(h, env.FirmA)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/runs/"+runID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	var out struct {
		Run       agents.RunView    `json:"run"`
		Steps     []agents.StepView `json:"steps"`
		Proposals []struct {
			ID string `json:"id"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Run.ID != runID.String() {
		t.Fatalf("run id = %s, want %s", out.Run.ID, runID)
	}
	if len(out.Steps) != 2 || out.Steps[0].ID != step1.String() || out.Steps[1].ID != step2.String() {
		t.Fatalf("steps not ordered by seq: %+v", out.Steps)
	}
	if len(out.Proposals) != 1 {
		t.Fatalf("proposals = %d, want 1", len(out.Proposals))
	}

	// A Firm B caller gets 404 for Firm A's run id: RLS hides the row entirely.
	routerB := mount(h, env.FirmB)
	reqB := httptest.NewRequest(http.MethodGet, "/v1/agents/runs/"+runID.String(), nil)
	wB := httptest.NewRecorder()
	routerB.ServeHTTP(wB, reqB)
	if wB.Code != http.StatusNotFound {
		t.Fatalf("firm B status = %d, want 404", wB.Code)
	}
}
