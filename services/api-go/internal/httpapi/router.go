// Package httpapi exposes the public HTTP API of api-go.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
)

// readyzCacheTTL bounds how often /readyz actually runs the backend check
// (Postgres, JetStream, Valkey and MinIO). /readyz is unauthenticated and
// unrate-limited, so without this a caller polling it could generate
// unbounded backend load. A var so tests can shrink it.
var readyzCacheTTL = 2 * time.Second

// cachedReady memoises a readiness check for readyzCacheTTL.
type cachedReady struct {
	check func(context.Context) error
	now   func() time.Time

	mu  sync.Mutex
	at  time.Time
	err error
}

func newCachedReady(check func(context.Context) error) *cachedReady {
	return &cachedReady{check: check, now: time.Now}
}

func (c *cachedReady) Ready(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && c.now().Sub(c.at) < readyzCacheTTL {
		return c.err
	}
	c.err = c.check(ctx)
	c.at = c.now()
	return c.err
}

// NewRouter wires the HTTP routes. ready is called by /readyz, at most once
// per readyzCacheTTL.
func NewRouter(v auth.Verifier, s Store, p Publisher, l Limiter, ready func(context.Context) error) http.Handler {
	h := &handlers{store: s, pub: p, limiter: l}
	cr := newCachedReady(ready)
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		defer cancel()
		if err := cr.Ready(ctx); err != nil {
			slog.WarnContext(ctx, "not ready", "err", err)
			writeError(w, http.StatusServiceUnavailable, "not ready")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(v))
		r.Post("/v1/invoices", h.createInvoice)
		r.Get("/v1/invoices/{id}", h.getInvoice)
		r.Get("/v1/me", h.me)
	})
	return otelhttp.NewHandler(r, "api-go")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
