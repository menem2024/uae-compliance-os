package clients

import (
	"errors"
	"log/slog"
	"net/http"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

// Handler serves /v1/client-companies*.
type Handler struct{ store Store }

// NewHandler returns the HTTP handlers over store.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// Mount registers the routes. read and write are the per-Firm middleware
// stacks (httpx.Firm with the b-read / b-write limiters).
func (h *Handler) Mount(r chi.Router, read, write func(http.Handler) http.Handler) {
	r.With(read).Get("/v1/client-companies", h.list)
	r.With(read).Get("/v1/client-companies/{id}", h.get)
	r.With(write).Post("/v1/client-companies", h.create)
	r.With(write).Patch("/v1/client-companies/{id}", h.patch)
	r.With(write).Post("/v1/client-companies/{id}/archive", h.setStatus("archived"))
	r.With(write).Post("/v1/client-companies/{id}/restore", h.setStatus("active"))
}

type page struct {
	Items      []View  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := ListFilter{Status: q.Get("status"), Query: q.Get("q")}
	if f.Status == "" {
		f.Status = "active"
	}
	if f.Status != "active" && f.Status != "archived" && f.Status != "all" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_status")
		return
	}
	if utf8.RuneCountInString(f.Query) > 100 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	if f.Query != "" {
		f.Query = httpx.ContainsPattern(f.Query)
	}
	limit, err := httpx.PageLimit(r, 50, 100)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
		return
	}
	if c := q.Get("cursor"); c != "" {
		if f.AfterName, f.AfterID, err = httpx.ParseNameCursor(c); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_cursor")
			return
		}
	}
	f.Limit = limit + 1
	items, err := h.store.List(r.Context(), httpx.FirmFrom(r.Context()), f)
	if err != nil {
		h.internal(w, r, "list client companies", err)
		return
	}
	out := page{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		c := httpx.NameCursor(last.Name, last.ID)
		out.Items, out.NextCursor = items[:limit], &c
	}
	if out.Items == nil {
		out.Items = []View{}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	f, err := Merge(Fields{}, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	v, err := h.store.Create(r.Context(), httpx.FirmFrom(r.Context()), f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, v)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := h.store.Get(r.Context(), httpx.FirmFrom(r.Context()), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in Input
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	v, err := h.store.Update(r.Context(), httpx.FirmFrom(r.Context()), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *Handler) setStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		v, err := h.store.SetStatus(r.Context(), httpx.FirmFrom(r.Context()), id, status)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, v)
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found") // same as a missing row: ids are not guessable
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ve ValidationError
	switch {
	case errors.As(err, &ve):
		httpx.WriteError(w, http.StatusBadRequest, ve.Code)
	case errors.Is(err, ErrTRNTaken):
		httpx.WriteError(w, http.StatusConflict, CodeTRNTaken)
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found")
	default:
		h.internal(w, r, "client companies", err)
	}
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	slog.ErrorContext(r.Context(), what, "firm_id", httpx.FirmFrom(r.Context()), "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal")
}
