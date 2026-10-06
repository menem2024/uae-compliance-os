// Package trackc is the composition root of Track C (validation, approvals, audit, exports): it
// builds the services of internal/{validation,exports,audit,invoicefix}, owns the Firm middleware
// and rate-limit seam that the Track C routes sit behind, and carries the wiring points that Tasks
// 19 (proposals) and 20 (fix tasks) fill in once Track B's gate GB-2 is open.
package trackc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/exports"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// Limiter decides whether a Firm may make another call (ratelimit.Limiter implements it).
type Limiter interface {
	Allow(ctx context.Context, firmID string) (bool, error)
}

// FirmResolver maps a Zitadel organisation to its Firm id; db.ErrNotFound for an unknown one.
type FirmResolver interface {
	FirmIDForOrg(ctx context.Context, orgID string) (uuid.UUID, error)
}

// NamedLimiter gives inner its own key space. ratelimit.New builds "rl:<key>:<minute>", so the
// resulting keys are "rl:<name>:<firm>:<minute>", the layout of Track B's ratelimit.NewNamed. It is
// a local seam: this branch has no NewNamed yet.
func NamedLimiter(name string, inner Limiter) Limiter { return namedLimiter{name: name, inner: inner} }

type namedLimiter struct {
	name  string
	inner Limiter
}

func (l namedLimiter) Allow(ctx context.Context, firmID string) (bool, error) {
	return l.inner.Allow(ctx, l.name+":"+firmID)
}

type firmKey struct{}

// WithFirmID stores the resolved Firm in ctx (seam for Track B's httpx.WithFirmID).
func WithFirmID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, firmKey{}, id)
}

// FirmFrom returns the Firm that FirmMiddleware resolved (uuid.Nil outside it; seam for httpx.FirmFrom).
func FirmFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(firmKey{}).(uuid.UUID)
	return id
}

// WriteJSON writes v as a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the error shape {"error": "<code>"}.
func WriteError(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, map[string]string{"error": code})
}

// FirmMiddleware resolves the caller's Firm once, rate-limits it with l and puts the id in the
// context. It never calls next without a resolved Firm. Errors (the codes of Track B's httpx.Firm):
// 403 unknown_organisation, 500 internal, 503 rate_limiter_unavailable, 429 rate_limited.
func FirmMiddleware(fr FirmResolver, l Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, _ := auth.FromContext(r.Context())
			id, err := fr.FirmIDForOrg(r.Context(), p.OrgID)
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusForbidden, "unknown_organisation")
				return
			}
			if err != nil || id == uuid.Nil {
				slog.ErrorContext(r.Context(), "resolve firm", "org_id", p.OrgID, "err", err)
				WriteError(w, http.StatusInternalServerError, "internal")
				return
			}
			ok, err := l.Allow(r.Context(), id.String())
			if err != nil {
				slog.ErrorContext(r.Context(), "rate limiter", "firm_id", id, "err", err)
				WriteError(w, http.StatusServiceUnavailable, "rate_limiter_unavailable")
				return
			}
			if !ok {
				w.Header().Set("Retry-After", "60")
				WriteError(w, http.StatusTooManyRequests, "rate_limited")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithFirmID(r.Context(), id)))
		})
	}
}

// Middlewares are the two Firm stacks a Mount puts in front of its routes.
type Middlewares struct {
	Read  func(http.Handler) http.Handler // GET routes, limiter c-read
	Write func(http.Handler) http.Handler // mutations, limiter c-write
}

// Mount registers more Track C routes. The wiring files append Mounts; httpapi.WithTrackC calls each
// one inside the authenticated group.
type Mount func(r chi.Router, mw Middlewares)

// FixTask is what an on-demand fix request returns.
type FixTask struct {
	ID     uuid.UUID
	Mode   string
	Status string
}

var (
	// ErrNoFixableIssues: the latest run has no error issue the Fix agent could fix.
	ErrNoFixableIssues = errors.New("trackc: no fixable issues")
	// ErrFixInProgress: a fix task for the invoice's latest run is still running.
	ErrFixInProgress = errors.New("trackc: fix in progress")
)

// FixRequester asks the Fix agent for proposals (Task 20 implements it with fixtasks.Requester).
type FixRequester interface {
	RequestOnDemand(ctx context.Context, firmID, invoiceID uuid.UUID, requestedBy string) (FixTask, error)
}

// Module is the Track C composition root.
type Module struct {
	Pool         *pgxpool.Pool
	Validation   *validation.Service
	Exports      *exports.Service
	Firms        FirmResolver
	ReadLimiter  Limiter
	WriteLimiter Limiter
	Config       Config

	js jetstream.JetStream

	// Fixes is nil until Task 20 (gate GB-2) wires the Fix agent: POST .../fixes answers fix_unavailable.
	Fixes FixRequester
	// Fixes' background work (api-fix-tasks consumer, fix task sweeper) is registered by wireFixes; Run
	// starts it, Ready reports on it.
	loops []func(context.Context)
	ready []func(context.Context) error

	// Proposals maps every proposal kind to its Applier (built by wireProposals).
	Proposals *proposals.Registry
	// Mounts are extra routes added by wireProposals (Task 19).
	Mounts []Mount
}

// Deps are the collaborators New needs.
type Deps struct {
	Pool         *pgxpool.Pool
	Validator    validation.Validator
	Exporter     exports.Exporter
	Store        exports.ObjectStore
	Firms        FirmResolver
	ReadLimiter  Limiter // already named c-read (see NamedLimiter)
	WriteLimiter Limiter // already named c-write
	Config       Config
	// JS is the JetStream handle the Fix agent requests and answers travel on. Optional: when nil the
	// module serves no Fix agent (POST .../fixes answers fix_unavailable and runs trigger nothing).
	JS jetstream.JetStream
}

// New builds the module and runs the wiring steps.
func New(d Deps) (*Module, error) {
	if d.Pool == nil || d.Validator == nil || d.Exporter == nil || d.Store == nil || d.Firms == nil ||
		d.ReadLimiter == nil || d.WriteLimiter == nil {
		return nil, errors.New("trackc: every dependency is required")
	}
	m := &Module{
		Pool:         d.Pool,
		Validation:   &validation.Service{Pool: d.Pool, Validator: d.Validator},
		Exports:      &exports.Service{Pool: d.Pool, Exporter: d.Exporter, Store: d.Store},
		Firms:        d.Firms,
		ReadLimiter:  d.ReadLimiter,
		WriteLimiter: d.WriteLimiter,
		Config:       d.Config,
	}
	m.js = d.JS
	wireProposals(m)
	wireFixes(m)
	return m, nil
}

// Sweeper returns the background re-validation of invoices left in status "fixed" (spec §5.6.2).
func (m *Module) Sweeper() *validation.Sweeper {
	return &validation.Sweeper{Svc: m.Validation, Pool: m.Pool, Interval: 30 * time.Second, MinAge: 30 * time.Second}
}

// Run starts the module's background work (the Fix agent's consumer and sweeper) and blocks until ctx
// is done and every loop has returned. Without a Fix agent it only waits for ctx.
func (m *Module) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, loop := range m.loops {
		wg.Go(func() { loop(ctx) })
	}
	<-ctx.Done()
	wg.Wait()
}

// Ready reports whether the module's consumers are running.
func (m *Module) Ready(ctx context.Context) error {
	for _, r := range m.ready {
		if err := r(ctx); err != nil {
			return err
		}
	}
	return nil
}
