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

func TestLoadTrackB(t *testing.T) {
	setAll(t)
	for _, k := range []string{"STORAGE_PUBLIC_ENDPOINT", "STORAGE_REGION", "UPLOAD_RATE_LIMIT_PER_MINUTE",
		"B_WRITE_RATE_LIMIT_PER_MINUTE", "B_READ_RATE_LIMIT_PER_MINUTE"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.StoragePublicEndpoint != "minio:9000" || c.StorageRegion != "auto" ||
		c.UploadRateLimitPerMinute != 20 || c.BWriteRateLimitPerMinute != 120 || c.BReadRateLimitPerMinute != 600 {
		t.Errorf("defaults: %+v", c)
	}

	t.Setenv("STORAGE_PUBLIC_ENDPOINT", "files.localhost:9000")
	t.Setenv("STORAGE_REGION", "us-east-1")
	t.Setenv("UPLOAD_RATE_LIMIT_PER_MINUTE", "5")
	t.Setenv("B_WRITE_RATE_LIMIT_PER_MINUTE", "6")
	t.Setenv("B_READ_RATE_LIMIT_PER_MINUTE", "7")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.StoragePublicEndpoint != "files.localhost:9000" || c.StorageRegion != "us-east-1" ||
		c.UploadRateLimitPerMinute != 5 || c.BWriteRateLimitPerMinute != 6 || c.BReadRateLimitPerMinute != 7 {
		t.Errorf("overrides: %+v", c)
	}
}

func TestLoadTrackBRejectsBadLimits(t *testing.T) {
	for _, v := range []string{"abc", "0", "-3"} {
		setAll(t)
		t.Setenv("B_READ_RATE_LIMIT_PER_MINUTE", v)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "B_READ_RATE_LIMIT_PER_MINUTE") {
			t.Errorf("%q: err = %v", v, err)
		}
	}
}
