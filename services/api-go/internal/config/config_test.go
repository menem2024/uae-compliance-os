package config

import (
	"strings"
	"testing"
)

func setAll(t *testing.T) {
	for k, v := range map[string]string{
		"DATABASE_URL": "postgres://app@pg/db", "NATS_URL": "nats://nats:4222", "VALIDATOR_ADDR": "http://validator-rs:50051",
		"VALKEY_ADDR": "valkey:6379", "MINIO_ENDPOINT": "minio:9000", "MINIO_ACCESS_KEY": "a", "MINIO_SECRET_KEY": "s",
		"ZITADEL_ISSUER": "http://zitadel.localhost:8085", "ZITADEL_JWKS_URL": "http://zitadel.localhost:8085/oauth/v2/keys",
	} {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setAll(t)
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("OTEL_SERVICE_NAME", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.ServiceName != "api-go" || c.ValidatorAddr != "http://validator-rs:50051" || c.RateLimitPerMinute != 60 {
		t.Errorf("got %+v", c)
	}
}

func TestLoadMissing(t *testing.T) {
	setAll(t)
	t.Setenv("ZITADEL_JWKS_URL", "")
	t.Setenv("DATABASE_URL", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ZITADEL_JWKS_URL") || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadAudience(t *testing.T) {
	setAll(t)
	t.Setenv("ZITADEL_AUDIENCE", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ZitadelAudience) != 0 {
		t.Errorf("unset ZITADEL_AUDIENCE: got %q, want none", c.ZitadelAudience)
	}

	t.Setenv("ZITADEL_AUDIENCE", " 312345678901234567 , client-web,, ")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.ZitadelAudience, "|") != "312345678901234567|client-web" {
		t.Errorf("got %q", c.ZitadelAudience)
	}
}
