package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// jwksStartupTimeout bounds how long NewJWKSVerifier waits for the JWKS
// endpoint (Zitadel starts slowly).
var jwksStartupTimeout = 60 * time.Second

const jwksRetryInterval = 2 * time.Second

// JWKSVerifier verifies RS256 tokens against a JWKS refreshed in the background.
type JWKSVerifier struct {
	issuer string
	keys   jwk.Set
}

// NewJWKSVerifier builds a verifier that refreshes the JWKS in the background.
// It retries the initial fetch for up to 60s and returns early if ctx is
// cancelled. The background refresher lives as long as ctx.
func NewJWKSVerifier(ctx context.Context, issuer, jwksURL string) (*JWKSVerifier, error) {
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
	return &JWKSVerifier{issuer: issuer, keys: set}, nil
}

func waitForJWKS(ctx context.Context, jwksURL string) error {
	deadline := time.Now().Add(jwksStartupTimeout)
	for attempt := 1; ; attempt++ {
		fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
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

// Verify checks signature, issuer and expiry, and extracts the org claim.
func (v *JWKSVerifier) Verify(_ context.Context, raw string) (Principal, error) {
	tok, err := jwt.Parse([]byte(raw), jwt.WithKeySet(v.keys), jwt.WithIssuer(v.issuer), jwt.WithValidate(true))
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	var org string
	if err := tok.Get(OrgClaim, &org); err != nil || org == "" {
		return Principal{}, fmt.Errorf("%w: missing %s", ErrUnauthenticated, OrgClaim)
	}
	sub, _ := tok.Subject()
	return Principal{Subject: sub, OrgID: org}, nil
}
