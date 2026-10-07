package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/protojson"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
)

// MaxBodyBytes caps a request body. It also sizes the http.Server read
// timeout in cmd/api (large enough for a slow client to upload it fully).
const MaxBodyBytes = 1 << 20

// Store is the persistence the handlers need. Get returns db.ErrNotFound
// when the invoice is missing or hidden by RLS; FirmIDForOrg and Firm return
// db.ErrNotFound for an unknown organisation.
type Store interface {
	FirmIDForOrg(ctx context.Context, orgID string) (uuid.UUID, error)
	Create(ctx context.Context, firmID uuid.UUID, payload []byte) (uuid.UUID, error)
	Get(ctx context.Context, firmID, id uuid.UUID) (InvoiceView, error)
	Firm(ctx context.Context, orgID string) (FirmView, error)
}

// Publisher emits domain events.
type Publisher interface {
	PublishSubmitted(ctx context.Context, ev *compliancev1.InvoiceSubmitted) error
}

// Limiter decides whether a firm may submit another invoice.
type Limiter interface {
	Allow(ctx context.Context, firmID string) (bool, error)
}

// InvoiceView is the GET /v1/invoices/{id} response.
type InvoiceView struct {
	ID             string      `json:"id"`
	Status         string      `json:"status"`
	RulesetVersion *string     `json:"ruleset_version"`
	Issues         []IssueView `json:"issues"`
}

// IssueView is one validation issue.
type IssueView struct {
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

// FirmView is the caller's firm, used for theming.
type FirmView struct {
	ID         uuid.UUID
	Name       string
	BrandColor *string
}

type handlers struct {
	store   Store
	pub     Publisher
	limiter Limiter
}

// decodeInvoice reads a canonical invoice (proto field names, spec §5.6.6). Unknown fields, trailing
// data and bodies over MaxBodyBytes are rejected. The Phase 0 seven-key body is a valid invoice, and
// total_amount and vat_amount must still be decimals.
func decodeInvoice(r *http.Request, w http.ResponseWriter) (*compliancev1.Invoice, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	in := &compliancev1.Invoice{}
	if err := (protojson.UnmarshalOptions{}).Unmarshal(raw, in); err != nil {
		return nil, fmt.Errorf("decode body: %w", err)
	}
	if _, err := decimal.NewFromString(in.GetTotalAmount()); err != nil {
		return nil, fmt.Errorf("total_amount: %w", err)
	}
	if _, err := decimal.NewFromString(in.GetVatAmount()); err != nil {
		return nil, fmt.Errorf("vat_amount: %w", err)
	}
	return in, nil
}

// firmID resolves the caller's firm, writing 403/500 on failure.
func (h *handlers) firmID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	p, _ := auth.FromContext(r.Context())
	id, err := h.store.FirmIDForOrg(r.Context(), p.OrgID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusForbidden, "unknown organisation")
		return uuid.Nil, false
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "resolve firm", "org_id", p.OrgID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return uuid.Nil, false
	}
	return id, true
}

// checkRateLimit enforces the per-firm limit shared by every authenticated
// route (not just POST /v1/invoices): unlimited GETs would still let an
// authenticated caller generate unbounded backend load. It writes the
// response and reports whether the caller should proceed.
func (h *handlers) checkRateLimit(w http.ResponseWriter, r *http.Request, firmID uuid.UUID) bool {
	allowed, err := h.limiter.Allow(r.Context(), firmID.String())
	if err != nil {
		slog.ErrorContext(r.Context(), "rate limiter", "firm_id", firmID, "err", err)
		writeError(w, http.StatusServiceUnavailable, "rate limiter unavailable")
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}

func (h *handlers) createInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := decodeInvoice(r, w)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	firmID, ok := h.firmID(w, r)
	if !ok {
		return
	}
	if !h.checkRateLimit(w, r, firmID) {
		return
	}
	payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(in)
	if err != nil {
		slog.ErrorContext(ctx, "marshal payload", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	id, err := h.store.Create(ctx, firmID, payload)
	if err != nil {
		slog.ErrorContext(ctx, "create invoice", "firm_id", firmID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ev := &compliancev1.InvoiceSubmitted{
		InvoiceId: id.String(),
		FirmId:    firmID.String(),
		Invoice:   in,
	}
	if err := h.pub.PublishSubmitted(ctx, ev); err != nil {
		// The row stays "uploaded"; acceptable for Phase 0 (no outbox yet).
		slog.ErrorContext(ctx, "publish invoice.submitted", "invoice_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		w.Header().Set("X-Trace-Id", sc.TraceID().String())
	}
	slog.InfoContext(ctx, "invoice submitted", "invoice_id", id, "firm_id", firmID)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id.String(), "status": db.StatusUploaded})
}

func (h *handlers) getInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid invoice id")
		return
	}
	firmID, ok := h.firmID(w, r)
	if !ok {
		return
	}
	if !h.checkRateLimit(w, r, firmID) {
		return
	}
	v, err := h.store.Get(ctx, firmID, id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "get invoice", "invoice_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if v.Issues == nil {
		v.Issues = []IssueView{}
	}
	writeJSON(w, http.StatusOK, v)
}
