// Package httpx holds the HTTP helpers shared by the Track B packages (clients, firms, documents,
// agents): strict JSON decoding, the {"error": code} writer, per-Firm resolution and rate limiting,
// keyset cursors and ILIKE escaping.
package httpx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
)

// MaxJSONBody bounds every Track B JSON request body (100 upload items fit comfortably).
const MaxJSONBody = 256 << 10

// ErrBadJSON wraps every body decoding failure.
var ErrBadJSON = errors.New("invalid json body")

// DecodeJSON strictly decodes one JSON object: unknown fields and trailing data are errors.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", ErrBadJSON, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing data", ErrBadJSON)
	}
	return nil
}

// WriteJSON writes v with status code.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes {"error": code}, the Phase 0 error shape.
func WriteError(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, map[string]string{"error": code})
}

// FirmResolver maps a Zitadel organisation to its Firm (httpapi.PGStore implements it).
type FirmResolver interface {
	FirmIDForOrg(ctx context.Context, orgID string) (uuid.UUID, error)
}

// Limiter is a per-Firm fixed-window limiter (ratelimit.Limiter implements it).
type Limiter interface {
	Allow(ctx context.Context, firmID string) (bool, error)
}

type firmKey struct{}

// FirmFrom returns the Firm resolved by Firm; uuid.Nil outside it.
func FirmFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(firmKey{}).(uuid.UUID)
	return id
}

// WithFirmID returns ctx carrying firmID (tests and the SSE handler use it).
func WithFirmID(ctx context.Context, firmID uuid.UUID) context.Context {
	return context.WithValue(ctx, firmKey{}, firmID)
}

// Firm resolves the caller's Firm once (403 unknown_organisation, 500 internal) and applies the
// limiter (429 rate_limited with Retry-After: 60, 503 rate_limiter_unavailable). Mount it inside the
// auth group, per route group, with the limiter that group needs.
func Firm(fr FirmResolver, l Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, _ := auth.FromContext(r.Context())
			firmID, err := fr.FirmIDForOrg(r.Context(), p.OrgID)
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusForbidden, "unknown_organisation")
				return
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "resolve firm", "org_id", p.OrgID, "err", err)
				WriteError(w, http.StatusInternalServerError, "internal")
				return
			}
			ok, err := l.Allow(r.Context(), firmID.String())
			if err != nil {
				slog.ErrorContext(r.Context(), "rate limiter", "firm_id", firmID, "err", err)
				WriteError(w, http.StatusServiceUnavailable, "rate_limiter_unavailable")
				return
			}
			if !ok {
				w.Header().Set("Retry-After", "60")
				WriteError(w, http.StatusTooManyRequests, "rate_limited")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithFirmID(r.Context(), firmID)))
		})
	}
}

// PageLimit parses ?limit= (default def, 1..max); 0 and garbage are errors.
func PageLimit(r *http.Request, def, max int) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("limit must be 1..%d", max)
	}
	return n, nil
}

// TimeCursor is an opaque keyset cursor over (created_at DESC, id DESC).
func TimeCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(t.UnixMicro(), 10) + "|" + id.String()))
}

// ParseTimeCursor reverses TimeCursor.
func ParseTimeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, errors.New("bad cursor")
	}
	ts, idStr, ok := strings.Cut(string(raw), "|")
	micros, perr := strconv.ParseInt(ts, 10, 64)
	id, uerr := uuid.Parse(idStr)
	if !ok || perr != nil || uerr != nil {
		return time.Time{}, uuid.Nil, errors.New("bad cursor")
	}
	return time.UnixMicro(micros).UTC(), id, nil
}

// NameCursor is an opaque keyset cursor over (name, id) ascending.
func NameCursor(name string, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String() + "|" + name))
}

// ParseNameCursor reverses NameCursor.
func ParseNameCursor(s string) (string, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", uuid.Nil, errors.New("bad cursor")
	}
	idStr, name, ok := strings.Cut(string(raw), "|")
	id, uerr := uuid.Parse(idStr)
	if !ok || uerr != nil {
		return "", uuid.Nil, errors.New("bad cursor")
	}
	return name, id, nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ContainsPattern returns an ILIKE pattern matching q anywhere, with q's wildcards escaped
// (Postgres' default LIKE escape character is the backslash).
func ContainsPattern(q string) string { return "%" + likeEscaper.Replace(q) + "%" }
