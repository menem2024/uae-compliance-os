package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

func newIssuer(t *testing.T) (*httptest.Server, jwk.Key) {
	t.Helper()
	raw, _ := rsa.GenerateKey(rand.Reader, 2048)
	priv, _ := jwk.Import(raw)
	_ = priv.Set(jwk.KeyIDKey, "k1")
	_ = priv.Set(jwk.AlgorithmKey, jwa.RS256())
	pub, _ := priv.PublicKey()
	set := jwk.NewSet()
	_ = set.AddKey(pub)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(srv.Close)
	return srv, priv
}

func sign(t *testing.T, key jwk.Key, iss string, exp time.Time, org string) string {
	t.Helper()
	b := jwt.NewBuilder().Issuer(iss).Subject("user-1").Expiration(exp).IssuedAt(time.Now())
	if org != "" {
		b = b.Claim(OrgClaim, org)
	}
	tok, _ := b.Build()
	s, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), key))
	if err != nil {
		t.Fatal(err)
	}
	return string(s)
}

func TestVerify(t *testing.T) {
	srv, key := newIssuer(t)
	v, err := NewJWKSVerifier(context.Background(), srv.URL, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	p, err := v.Verify(ctx, sign(t, key, srv.URL, time.Now().Add(time.Hour), "org-A"))
	if err != nil || p.Subject != "user-1" || p.OrgID != "org-A" {
		t.Fatalf("got %+v, %v", p, err)
	}
	if _, err := v.Verify(ctx, sign(t, key, "http://evil", time.Now().Add(time.Hour), "org-A")); err == nil {
		t.Error("wrong issuer accepted")
	}
	if _, err := v.Verify(ctx, sign(t, key, srv.URL, time.Now().Add(-time.Hour), "org-A")); err == nil {
		t.Error("expired token accepted")
	}
	if _, err := v.Verify(ctx, sign(t, key, srv.URL, time.Now().Add(time.Hour), "")); err == nil {
		t.Error("token without org accepted")
	}
}

func TestNewJWKSVerifierGivesUp(t *testing.T) {
	old := jwksStartupTimeout
	jwksStartupTimeout = 100 * time.Millisecond
	t.Cleanup(func() { jwksStartupTimeout = old })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	if _, err := NewJWKSVerifier(context.Background(), srv.URL, srv.URL); err == nil {
		t.Fatal("expected error when JWKS is unavailable")
	}
}

func TestNewJWKSVerifierHonoursCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := NewJWKSVerifier(ctx, "http://127.0.0.1:1", "http://127.0.0.1:1"); err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("cancelled context not honoured: %s", time.Since(start))
	}
}
