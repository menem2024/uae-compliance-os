package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
)

type meResponse struct {
	FirmID     string  `json:"firm_id"`
	FirmName   string  `json:"firm_name"`
	BrandColor *string `json:"brand_color"`
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.FromContext(ctx)
	f, err := h.store.Firm(ctx, p.OrgID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusForbidden, "unknown organisation")
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "resolve firm", "org_id", p.OrgID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{FirmID: f.ID.String(), FirmName: f.Name, BrandColor: f.BrandColor})
}
