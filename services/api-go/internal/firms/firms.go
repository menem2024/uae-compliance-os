// Package firms serves the caller's Firm settings: name and accent colour (spec section 5.4, ADR 016).
package firms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

var colorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// View is the API representation of a Firm.
type View struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	BrandColor *string   `json:"brand_color"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Nullable distinguishes an absent field (Set=false) from null (Set=true, Value=nil).
type Nullable struct {
	Set   bool
	Value *string
}

// UnmarshalJSON is called for null too, so Set records presence.
func (n *Nullable) UnmarshalJSON(b []byte) error {
	n.Set = true
	if bytes.Equal(b, []byte("null")) {
		n.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	n.Value = &s
	return nil
}

// Patch is the PATCH /v1/firm body.
type Patch struct {
	Name       *string  `json:"name"`
	BrandColor Nullable `json:"brand_color"`
}

// ValidationError carries the error code.
type ValidationError struct{ Code string }

func (e ValidationError) Error() string { return e.Code }

// Apply merges p over cur and validates; the colour is stored upper-case.
func Apply(cur View, p Patch) (View, error) {
	out := cur
	if p.Name != nil {
		name := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, *p.Name)), " ")
		if name == "" || utf8.RuneCountInString(name) > 200 {
			return cur, ValidationError{"invalid_name"}
		}
		out.Name = name
	}
	if p.BrandColor.Set {
		if p.BrandColor.Value == nil {
			out.BrandColor = nil
		} else {
			c := strings.ToUpper(*p.BrandColor.Value)
			if !colorRe.MatchString(c) {
				return cur, ValidationError{"invalid_brand_color"}
			}
			out.BrandColor = &c
		}
	}
	return out, nil
}

// Store reads and updates the caller's Firm.
type Store interface {
	Get(ctx context.Context, firmID uuid.UUID) (View, error)
	Update(ctx context.Context, firmID uuid.UUID, p Patch) (View, error)
}

// PGStore implements Store; both methods run inside db.WithFirm, so the
// firms_update_own policy limits an update to the caller's own row.
type PGStore struct{ Pool *pgxpool.Pool }

var _ Store = PGStore{}

func view(id uuid.UUID, name string, color pgtype.Text, updated pgtype.Timestamptz) View {
	v := View{ID: id, Name: name, UpdatedAt: updated.Time}
	if color.Valid {
		c := color.String
		v.BrandColor = &c
	}
	return v
}

// Get returns the Firm.
func (s PGStore) Get(ctx context.Context, firmID uuid.UUID) (View, error) {
	var v View
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		f, err := q.GetFirm(ctx, firmID)
		if err != nil {
			return err
		}
		v = view(f.ID, f.Name, f.BrandColor, f.UpdatedAt)
		return nil
	})
	return v, err
}

// Update applies p in one transaction.
func (s PGStore) Update(ctx context.Context, firmID uuid.UUID, p Patch) (View, error) {
	var v View
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		f, err := q.GetFirm(ctx, firmID)
		if err != nil {
			return err
		}
		next, err := Apply(view(f.ID, f.Name, f.BrandColor, f.UpdatedAt), p)
		if err != nil {
			return err
		}
		color := pgtype.Text{}
		if next.BrandColor != nil {
			color = pgtype.Text{String: *next.BrandColor, Valid: true}
		}
		u, err := q.UpdateFirm(ctx, sqlc.UpdateFirmParams{ID: firmID, Name: next.Name, BrandColor: color})
		if err != nil {
			return err
		}
		v = view(u.ID, u.Name, u.BrandColor, u.UpdatedAt)
		return nil
	})
	return v, err
}

// Handler serves /v1/firm.
type Handler struct{ store Store }

// NewHandler returns the handlers over store.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// Mount registers GET and PATCH /v1/firm behind the read and write stacks.
func (h *Handler) Mount(r chi.Router, read, write func(http.Handler) http.Handler) {
	r.With(read).Get("/v1/firm", h.get)
	r.With(write).Patch("/v1/firm", h.patch)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.Get(r.Context(), httpx.FirmFrom(r.Context()))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	var p Patch
	if err := httpx.DecodeJSON(w, r, &p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	v, err := h.store.Update(r.Context(), httpx.FirmFrom(r.Context()), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ve ValidationError
	switch {
	case errors.As(err, &ve):
		httpx.WriteError(w, http.StatusBadRequest, ve.Code)
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found")
	default:
		slog.ErrorContext(r.Context(), "firm settings", "firm_id", httpx.FirmFrom(r.Context()), "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal")
	}
}
