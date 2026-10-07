package agents

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

// DefaultLimit and MaxPageLimit bound GET /v1/agents/runs (like documents and client-companies).
const (
	DefaultLimit = 20
	MaxPageLimit = 100
	backfillCap  = 50
)

// RunStatuses are the valid values of ?status= on GET /v1/agents/runs (agent_runs.status CHECK).
var RunStatuses = map[string]bool{
	"running": true, "succeeded": true, "failed": true, "budget_exceeded": true,
	"cancelled": true, "abandoned": true,
}

// Handler serves /v1/agents*.
type Handler struct {
	pool *pgxpool.Pool
	hub  *Hub

	keepAlive time.Duration // overridable by same-package tests; default 15s
}

// NewHandler returns the HTTP handlers over pool and hub.
func NewHandler(pool *pgxpool.Pool, hub *Hub) *Handler {
	return &Handler{pool: pool, hub: hub, keepAlive: 15 * time.Second}
}

// SetKeepAlive overrides the SSE keep-alive comment interval (default 15s); tests use a short interval
// to assert the connection survives past a short server ReadTimeout/WriteTimeout (F1).
func (h *Handler) SetKeepAlive(d time.Duration) { h.keepAlive = d }

// Mount registers the routes. read and write are the per-Firm middleware stacks (httpx.Firm with the
// b-read / b-write limiters); every Track B route here is a GET, so only read is used.
func (h *Handler) Mount(r chi.Router, read, write func(http.Handler) http.Handler) {
	_ = write
	r.With(read).Get("/v1/agents/runs", h.list)
	r.With(read).Get("/v1/agents/runs/{id}", h.detail)
	r.With(read).Get("/v1/agents/summary", h.summary)
	r.With(read).Get("/v1/agents/activity", h.activity)
}

func internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	slog.ErrorContext(r.Context(), what, "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal")
}

// --------------------------------------------------------------------------------- GET /v1/agents/runs

type runsPage struct {
	Items      []RunView `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var p sqlc.ListRunsParams
	if st := q.Get("subject_type"); st != "" {
		p.SubjectType = pgtype.Text{String: st, Valid: true}
	}
	if sid := q.Get("subject_id"); sid != "" {
		p.SubjectID = pgtype.Text{String: sid, Valid: true}
	}
	if status := q.Get("status"); status != "" {
		if !RunStatuses[status] {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_status")
			return
		}
		p.Status = pgtype.Text{String: status, Valid: true}
	}
	limit, err := httpx.PageLimit(r, DefaultLimit, MaxPageLimit)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
		return
	}
	if c := q.Get("cursor"); c != "" {
		t, id, perr := httpx.ParseTimeCursor(c)
		if perr != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_cursor")
			return
		}
		p.BeforeCreatedAt = pgtype.Timestamptz{Time: t, Valid: true}
		p.BeforeID = uuid.NullUUID{UUID: id, Valid: true}
	}
	p.PageLimit = int32(limit + 1) //nolint:gosec // bounded by MaxPageLimit
	var rows []sqlc.AgentRun
	err = db.WithFirm(r.Context(), h.pool, httpx.FirmFrom(r.Context()), func(q *sqlc.Queries) error {
		var err error
		rows, err = q.ListRuns(r.Context(), p)
		return err
	})
	if err != nil {
		internal(w, r, "list runs", err)
		return
	}
	out := runsPage{Items: make([]RunView, 0, len(rows))}
	for _, row := range rows {
		out.Items = append(out.Items, ViewOfRun(row))
	}
	if len(out.Items) > limit {
		last := rows[limit-1]
		c := httpx.TimeCursor(last.CreatedAt.Time, last.ID)
		out.Items, out.NextCursor = out.Items[:limit], &c
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ----------------------------------------------------------------------------- GET /v1/agents/runs/{id}

type runDetail struct {
	Run       RunView              `json:"run"`
	Steps     []StepView           `json:"steps"`
	Proposals []proposals.Proposal `json:"proposals"`
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	var run sqlc.AgentRun
	var steps []sqlc.AgentStep
	var props []proposals.Proposal
	err = db.WithFirm(r.Context(), h.pool, httpx.FirmFrom(r.Context()), func(q *sqlc.Queries) error {
		var err error
		if run, err = q.GetRun(r.Context(), id); err != nil {
			return err
		}
		if steps, err = q.ListRunSteps(r.Context(), id); err != nil {
			return err
		}
		props, err = proposals.ForRun(r.Context(), q, id)
		return err
	})
	if errors.Is(err, db.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		internal(w, r, "run detail", err)
		return
	}
	out := runDetail{Run: ViewOfRun(run), Steps: make([]StepView, 0, len(steps)), Proposals: props}
	for _, s := range steps {
		out.Steps = append(out.Steps, ViewOfStep(s))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// -------------------------------------------------------------------------------- GET /v1/agents/summary

type summaryView struct {
	Runs   sqlc.RunSummaryRow     `json:"runs"`
	Agents []sqlc.AgentSummaryRow `json:"agents"`
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)
	ts := func(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
	var out summaryView
	err := db.WithFirm(r.Context(), h.pool, httpx.FirmFrom(r.Context()), func(q *sqlc.Queries) error {
		var err error
		if out.Runs, err = q.RunSummary(r.Context(), sqlc.RunSummaryParams{DayStart: ts(dayStart), DayEnd: ts(dayEnd)}); err != nil {
			return err
		}
		out.Agents, err = q.AgentSummary(r.Context(), sqlc.AgentSummaryParams{DayStart: ts(dayStart), DayEnd: ts(dayEnd)})
		return err
	})
	if err != nil {
		internal(w, r, "agent summary", err)
		return
	}
	if out.Agents == nil {
		out.Agents = []sqlc.AgentSummaryRow{}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ------------------------------------------------------------------------------- GET /v1/agents/activity

// parseLastEventID extracts the timestamp out of an EventID ("<unix_ms>-<id>"); a missing or malformed
// header means "the beginning", same as no Last-Event-ID at all.
func parseLastEventID(raw string) time.Time {
	msStr, _, ok := strings.Cut(raw, "-")
	if !ok {
		return time.Time{}
	}
	ms, err := strconv.ParseInt(msStr, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, ev Event) {
	var b strings.Builder
	if ev.ID != "" {
		b.WriteString("id: ")
		b.WriteString(ev.ID)
		b.WriteString("\n")
	}
	b.WriteString("event: ")
	b.WriteString(ev.Type)
	b.WriteString("\n")
	data := ev.Data
	if len(data) == 0 {
		data = []byte("{}")
	}
	b.WriteString("data: ")
	b.Write(data)
	b.WriteString("\n\n")
	_, _ = w.Write([]byte(b.String()))
	if flusher != nil {
		flusher.Flush()
	}
}

func (h *Handler) activity(w http.ResponseWriter, r *http.Request) {
	firm := httpx.FirmFrom(r.Context())
	sub, err := h.hub.Subscribe(firm)
	if err != nil {
		httpx.WriteError(w, http.StatusTooManyRequests, "too_many_streams")
		return
	}
	defer sub.Close()

	// F1: clear the server's Read/WriteTimeout for this connection before the first byte, or an idle
	// SSE stream gets cancelled when cmd/api/main.go's 20s deadlines fire.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	since := time.Time{}
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		since = parseLastEventID(raw)
	} else if raw := r.URL.Query().Get("last_event_id"); raw != "" {
		since = parseLastEventID(raw)
	}

	ctx := r.Context()
	var runs []sqlc.AgentRun
	var steps []sqlc.AgentStep
	err = db.WithFirm(ctx, h.pool, firm, func(q *sqlc.Queries) error {
		var err error
		if runs, err = q.RecentRuns(ctx, sqlc.RecentRunsParams{
			Since: pgtype.Timestamptz{Time: since, Valid: true}, PageLimit: backfillCap,
		}); err != nil {
			return err
		}
		steps, err = q.RecentSteps(ctx, sqlc.RecentStepsParams{
			Since: pgtype.Timestamptz{Time: since, Valid: true}, PageLimit: backfillCap,
		})
		return err
	})
	if err != nil {
		slog.ErrorContext(ctx, "agents activity backfill", "err", err)
		return
	}
	for _, run := range runs {
		v := ViewOfRun(run)
		data, _ := json.Marshal(v)
		writeSSEEvent(w, flusher, Event{Type: "run", ID: EventID(v.ID, run.UpdatedAt.Time), Data: data})
	}
	for _, step := range steps {
		v := ViewOfStep(step)
		data, _ := json.Marshal(v)
		writeSSEEvent(w, flusher, Event{Type: "step", ID: EventID(v.ID, step.UpdatedAt.Time), Data: data})
	}

	keepAlive := time.NewTicker(h.keepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			writeSSEEvent(w, flusher, ev)
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}
