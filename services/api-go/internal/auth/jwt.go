package auth

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// jwksStartupTimeout bounds how long NewJWKSVerifier waits for the JWKS
// endpoint (Zitadel starts slowly).
var jwksStartupTimeout = 60 * time.Second

const (
	jwksRetryInterval = 2 * time.Second
	jwksFetchTimeout  = 5 * time.Second
)

// jwksMinRefreshInterval rate-limits JWKS refetches triggered by an unknown
// kid, so a flood of forged tokens cannot hammer the IdP.
var jwksMinRefreshInterval = 10 * time.Second

// clockSkew is the acceptable difference between this server's clock and the
// IdP's when checking exp, nbf and iat, so a token issued right at the edge
// of validity is not spuriously rejected by clock drift between hosts.
const clockSkew = 30 * time.Second

// JWKSVerifier verifies RS256 tokens against a JWKS refreshed in the
// background, and refetched on demand when a token names an unknown kid
// (key rotation, or a key Zitadel created lazily after startup).
type JWKSVerifier struct {
	issuer    string
	audiences []string
	keys      jwk.Set
	refresh   func(context.Context) error

	mu          sync.Mutex // serialises on-demand refreshes
	lastRefresh time.Time  // zero until the first on-demand refresh
}

// Option configures a JWKSVerifier.
type Option func(*JWKSVerifier)

// WithAudience requires the token's aud claim to contain at least one of
// auds. Blank values are ignored; with none left, aud is not checked.
func WithAudience(auds ...string) Option {
	return func(v *JWKSVerifier) {
		for _, a := range auds {
			if a = strings.TrimSpace(a); a != "" {
				v.audiences = append(v.audiences, a)
			}
		}
	}
}

// NewJWKSVerifier builds a verifier that refreshes the JWKS in the background.
// It retries the initial fetch for up to 60s and returns early if ctx is
// cancelled. An empty key set is accepted: unknown kids trigger a refetch.
// The background refresher lives as long as ctx.
func NewJWKSVerifier(ctx context.Context, issuer, jwksURL string, opts ...Option) (*JWKSVerifier, error) {
	if err := waitForJWKS(ctx, jwksURL); err != nil {
		return nil, err
	}
	cache, err := jwk.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return nil, fmt.Errorf("jwks cache: %w", err)
	}
	regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cache.Register(regCtx, jwksURL); err != nil {
		return nil, fmt.Errorf("jwks register %s: %w", jwksURL, err)
	}
	set, err := cache.CachedSet(jwksURL)
	if err != nil {
		return nil, fmt.Errorf("jwks cached set: %w", err)
	}
	v := &JWKSVerifier{
		issuer: issuer,
		keys:   set,
		refresh: func(c context.Context) error {
			_, err := cache.Refresh(c, jwksURL)
			return err
		},
	}
	for _, o := range opts {
		o(v)
	}
	return v, nil
}

func waitForJWKS(ctx context.Context, jwksURL string) error {
	deadline := time.Now().Add(jwksStartupTimeout)
	for attempt := 1; ; attempt++ {
		fetchCtx, cancel := context.WithTimeout(ctx, jwksFetchTimeout)
		_, err := jwk.Fetch(fetchCtx, jwksURL)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().Add(jwksRetryInterval).After(deadline) {
			return fmt.Errorf("jwks %s unavailable after %s: %w", jwksURL, jwksStartupTimeout, err)
		}
		slog.WarnContext(ctx, "jwks not ready, retrying", "url", jwksURL, "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("jwks wait: %w", ctx.Err())
		case <-time.After(jwksRetryInterval):
		}
	}
}

// Verify checks signature, issuer, expiry and (if configured) audience, and
// extracts the org claim. A token signed with a kid missing from the cached
// JWKS triggers one rate-limited refetch before it is rejected.
func (v *JWKSVerifier) Verify(ctx context.Context, raw string) (Principal, error) {
	tok, err := v.parse(raw)
	if err != nil {
		if kid := unverifiedKID(raw); kid != "" && !v.hasKey(kid) && v.refreshForKID(ctx, kid) {
			tok, err = v.parse(raw)
		}
	}
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	if len(v.audiences) > 0 {
		aud, _ := tok.Audience()
		if !slices.ContainsFunc(aud, func(a string) bool { return slices.Contains(v.audiences, a) }) {
			return Principal{}, fmt.Errorf("%w: audience %q not accepted", ErrUnauthenticated, aud)
		}
	}
	var org string
	if err := tok.Get(OrgClaim, &org); err != nil || org == "" {
		return Principal{}, fmt.Errorf("%w: missing %s", ErrUnauthenticated, OrgClaim)
	}
	sub, _ := tok.Subject()
	return Principal{Subject: sub, OrgID: org}, nil
}

func (v *JWKSVerifier) parse(raw string) (jwt.Token, error) {
	return jwt.Parse([]byte(raw), jwt.WithKeySet(v.keys), jwt.WithIssuer(v.issuer),
		jwt.WithRequiredClaim("exp"), jwt.WithAcceptableSkew(clockSkew), jwt.WithValidate(true))
}

func (v *JWKSVerifier) hasKey(kid string) bool {
	_, ok := v.keys.LookupKeyID(kid)
	return ok
}

// refreshForKID refetches the JWKS at most once per jwksMinRefreshInterval
// and reports whether kid is now known. Concurrent callers wait for an
// in-flight refresh and then see its result.
func (v *JWKSVerifier) refreshForKID(ctx context.Context, kid string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.hasKey(kid) {
		return true // another request refreshed while we waited
	}
	if !v.lastRefresh.IsZero() && time.Since(v.lastRefresh) < jwksMinRefreshInterval {
		return false
	}
	v.lastRefresh = time.Now()
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jwksFetchTimeout)
	defer cancel()
	if err := v.refresh(rctx); err != nil {
		slog.WarnContext(ctx, "jwks refetch for unknown kid failed", "kid", kid, "err", err)
		return false
	}
	known := v.hasKey(kid)
	slog.InfoContext(ctx, "jwks refetched for unknown kid", "kid", kid, "found", known)
	return known
}

// unverifiedKID returns the kid from the token's protected header without
// verifying it; it is only used to decide whether a refetch could help.
func unverifiedKID(raw string) string {
	msg, err := jws.Parse([]byte(raw))
	if err != nil {
		return ""
	}
	sigs := msg.Signatures()
	if len(sigs) != 1 || sigs[0].ProtectedHeaders() == nil {
		return ""
	}
	kid, _ := sigs[0].ProtectedHeaders().KeyID()
	return kid
}
