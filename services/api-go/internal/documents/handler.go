package documents

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
)

// Handler serves /v1/documents*.
type Handler struct{ svc *Service }

// NewHandler returns the HTTP handlers over svc.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the routes. read, write and uploads are per-Firm middleware stacks (httpx.Firm with
// the b-read, b-write and uploads limiters).
func (h *Handler) Mount(r chi.Router, read, write, uploads func(http.Handler) http.Handler) {
	r.With(uploads).Post("/v1/documents/uploads", h.uploads)
	r.With(write).Post("/v1/documents/complete", h.complete)
	r.With(read).Get("/v1/documents", h.list)
	r.With(read).Get("/v1/documents/{id}", h.detail)
	r.With(read).Get("/v1/documents/{id}/download", h.download)
}

type uploadsRequest struct {
	ClientCompanyID string   `json:"client_company_id"`
	Files           []FileIn `json:"files"`
}

func (h *Handler) uploads(w http.ResponseWriter, r *http.Request) {
	var in uploadsRequest
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	cc, err := uuid.Parse(in.ClientCompanyID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_client_company_id")
		return
	}
	if len(in.Files) < 1 || len(in.Files) > MaxFiles {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_files")
		return
	}
	p, _ := auth.FromContext(r.Context())
	items, err := h.svc.RequestUploads(r.Context(), httpx.FirmFrom(r.Context()), cc, p.Subject, in.Files)
	switch {
	case errors.Is(err, ErrClientNotFound):
		httpx.WriteError(w, http.StatusNotFound, "client_company_not_found")
	case errors.Is(err, ErrClientArchived):
		httpx.WriteError(w, http.StatusConflict, "client_company_archived")
	case err != nil:
		internal(w, r, "request uploads", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, map[string][]UploadItem{"items": items})
	}
}

type completeRequest struct {
	DocumentIDs []string `json:"document_ids"`
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	var in completeRequest
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if len(in.DocumentIDs) < 1 || len(in.DocumentIDs) > MaxFiles {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_document_ids")
		return
	}
	items := h.svc.Complete(r.Context(), httpx.FirmFrom(r.Context()), in.DocumentIDs)
	httpx.WriteJSON(w, http.StatusOK, map[string][]CompleteItem{"items": items})
}

type page struct {
	Items      []View  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f ListFilter
	if raw := q.Get("client_company_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_client_company_id")
			return
		}
		f.ClientCompanyID = id
	}
	if f.Status = q.Get("status"); f.Status != "" && !Statuses[f.Status] {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_status")
		return
	}
	limit, err := httpx.PageLimit(r, DefaultLimit, MaxPageLimit)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
		return
	}
	if c := q.Get("cursor"); c != "" {
		if f.BeforeCreatedAt, f.BeforeID, err = httpx.ParseTimeCursor(c); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_cursor")
			return
		}
	}
	f.Limit = limit + 1
	rows, err := h.svc.Store.List(r.Context(), httpx.FirmFrom(r.Context()), f)
	if err != nil {
		internal(w, r, "list documents", err)
		return
	}
	out := page{Items: make([]View, 0, len(rows))}
	for _, d := range rows {
		out.Items = append(out.Items, ViewOf(d))
	}
	if len(out.Items) > limit {
		last := out.Items[limit-1]
		c := httpx.TimeCursor(last.CreatedAt, last.ID)
		out.Items, out.NextCursor = out.Items[:limit], &c
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := h.svc.Detail(r.Context(), httpx.FirmFrom(r.Context()), id)
	if errors.Is(err, db.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, CodeNotFound)
		return
	}
	if err != nil {
		internal(w, r, "document detail", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, exp, err := h.svc.Download(r.Context(), httpx.FirmFrom(r.Context()), id)
	switch {
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, CodeNotFound)
	case errors.Is(err, ErrNotDownloadable):
		httpx.WriteError(w, http.StatusConflict, "not_downloadable")
	case err != nil:
		internal(w, r, "download", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, struct {
			URL       string    `json:"url"`
			ExpiresAt time.Time `json:"expires_at"`
		}{u, exp})
	}
}

// pathID parses {id}; a malformed id is 404 like a missing one.
func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, CodeNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	slog.ErrorContext(r.Context(), what, "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal")
}
