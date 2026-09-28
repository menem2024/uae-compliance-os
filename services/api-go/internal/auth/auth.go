// Package auth verifies Zitadel access tokens and carries the caller's
// Principal through the request context.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// OrgClaim is the Zitadel claim holding the user's resource-owner organisation.
const OrgClaim = "urn:zitadel:iam:user:resourceowner:id"

// Principal is the authenticated caller.
type Principal struct{ Subject, OrgID string }

// Verifier validates a raw bearer token.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (Principal, error)
}

type ctxKey struct{}

// FromContext returns the Principal stored by Middleware.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// ErrUnauthenticated is returned for missing, malformed or invalid tokens.
var ErrUnauthenticated = errors.New("unauthenticated")

// Middleware rejects requests without a valid Bearer token.
func Middleware(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || raw == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			p, err := v.Verify(r.Context(), raw)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}
