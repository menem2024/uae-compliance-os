package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

// jwksServer serves a mutable JWKS and counts fetches.
type jwksServer struct {
	*httptest.Server
	mu      sync.Mutex
	set     jwk.Set
	fetches atomic.Int64
}

func newJWKSServer(t *testing.T) *jwksServer {
	t.Helper()
	s := &jwksServer{set: jwk.NewSet()}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.fetches.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.set)
	}))
	t.Cleanup(s.Close)
	return s
}

// addKey generates an RS256 key with kid, publishes its public half and
// returns the private key.
func (s *jwksServer) addKey(t *testing.T, kid string) jwk.Key {
	t.Helper()
	priv := newKey(t, kid)
	pub, err := priv.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.set.AddKey(pub); err != nil {
		t.Fatal(err)
	}
	return priv
}

func newKey(t *testing.T, kid string) jwk.Key {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := jwk.Import(raw)
	if err != nil {
		t.Fatal(err)
	}
	_ = priv.Set(jwk.KeyIDKey, kid)
	_ = priv.Set(jwk.AlgorithmKey, jwa.RS256())
	return priv
}

func TestVerifyRefetchesJWKSOnUnknownKID(t *testing.T) {
	srv := newJWKSServer(t)
	srv.addKey(t, "k1")
	v, err := NewJWKSVerifier(context.Background(), srv.URL, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// The IdP rotates: a new signing key appears after startup.
	k2 := srv.addKey(t, "k2")
	p, err := v.Verify(context.Background(), sign(t, k2, srv.URL, time.Now().Add(time.Hour), "org-A"))
	if err != nil || p.OrgID != "org-A" {
		t.Fatalf("token with rotated kid rejected on first try: %+v, %v", p, err)
	}
}

func TestVerifyAcceptsKeyAddedAfterEmptyStartupJWKS(t *testing.T) {
	srv := newJWKSServer(t) // Zitadel may serve {"keys":[]} until the first sign-in
	v, err := NewJWKSVerifier(context.Background(), srv.URL, srv.URL)
	if err != nil {
		t.Fatalf("empty JWKS must not fail startup: %v", err)
	}
	k1 := srv.addKey(t, "k1")
	if _, err := v.Verify(context.Background(), sign(t, k1, srv.URL, time.Now().Add(time.Hour), "org-A")); err != nil {
		t.Fatalf("token rejected after key appeared: %v", err)
	}
}

func TestUnknownKIDRefreshIsRateLimited(t *testing.T) {
	old := jwksMinRefreshInterval
	jwksMinRefreshInterval = 300 * time.Millisecond
	t.Cleanup(func() { jwksMinRefreshInterval = old })

	srv := newJWKSServer(t)
	srv.addKey(t, "k1")
	v, err := NewJWKSVerifier(context.Background(), srv.URL, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	rogue := newKey(t, "unknown") // never published
	tok := sign(t, rogue, srv.URL, time.Now().Add(time.Hour), "org-A")

	before := srv.fetches.Load()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.Verify(context.Background(), tok); err == nil {
				t.Error("token with unpublished kid accepted")
			}
		}()
	}
	wg.Wait()
	if got := srv.fetches.Load() - before; got != 1 {
		t.Fatalf("flood of unknown-kid tokens caused %d JWKS fetches, want exactly 1", got)
	}

	time.Sleep(jwksMinRefreshInterval + 50*time.Millisecond)
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("token with unpublished kid accepted")
	}
	if got := srv.fetches.Load() - before; got != 2 {
		t.Fatalf("after the refresh interval, fetches = %d, want 2", got)
	}
}

func signAud(t *testing.T, key jwk.Key, iss string, aud ...string) string {
	t.Helper()
	b := jwt.NewBuilder().Issuer(iss).Subject("user-1").Expiration(time.Now().Add(time.Hour)).Claim(OrgClaim, "org-A")
	if len(aud) > 0 {
		b = b.Audience(aud)
	}
	tok, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	s, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), key))
	if err != nil {
		t.Fatal(err)
	}
	return string(s)
}

func TestVerifyAudience(t *testing.T) {
	srv := newJWKSServer(t)
	key := srv.addKey(t, "k1")
	ctx := context.Background()

	v, err := NewJWKSVerifier(ctx, srv.URL, srv.URL, WithAudience("project-1", "client-2"))
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		aud    []string
		wantOK bool
	}{
		"matching aud":          {aud: []string{"project-1"}, wantOK: true},
		"one of several auds":   {aud: []string{"other", "client-2"}, wantOK: true},
		"wrong aud":             {aud: []string{"someone-else"}},
		"missing aud":           {},
		"empty string aud only": {aud: []string{""}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(ctx, signAud(t, key, srv.URL, tc.aud...))
			if (err == nil) != tc.wantOK {
				t.Fatalf("aud %q: err = %v, want ok=%v", tc.aud, err, tc.wantOK)
			}
		})
	}

	// Without WithAudience (ZITADEL_AUDIENCE unset) any audience is accepted.
	open, err := NewJWKSVerifier(ctx, srv.URL, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open.Verify(ctx, signAud(t, key, srv.URL, "anything")); err != nil {
		t.Fatalf("audience checked although none configured: %v", err)
	}
	// Blank configured values are ignored rather than matching an empty aud.
	blank, err := NewJWKSVerifier(ctx, srv.URL, srv.URL, WithAudience("", " "))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blank.Verify(ctx, signAud(t, key, srv.URL, "anything")); err != nil {
		t.Fatalf("blank audiences must disable the check: %v", err)
	}
}
