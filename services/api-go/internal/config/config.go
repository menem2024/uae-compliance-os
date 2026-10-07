// Package config loads api-go settings from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds runtime settings for the api server.
type Config struct {
	HTTPAddr           string
	DatabaseURL        string
	NATSURL            string
	ValidatorAddr      string
	ValkeyAddr         string
	MinioEndpoint      string
	MinioAccessKey     string
	MinioSecretKey     string
	MinioUseSSL        bool
	ZitadelIssuer      string
	ZitadelJWKSURL     string
	ZitadelAudience    []string // ZITADEL_AUDIENCE, comma-separated accepted aud values; empty disables the check
	ServiceName        string
	RateLimitPerMinute int64

	// Track B. The presigner signs URLs the browser can reach, so its endpoint is separate from the
	// internal MinIO client's; it reuses the MinIO keys and bucket.
	StoragePublicEndpoint    string // STORAGE_PUBLIC_ENDPOINT, default MINIO_ENDPOINT
	StorageRegion            string // STORAGE_REGION, default "auto" (R2-compatible)
	UploadRateLimitPerMinute int64  // UPLOAD_RATE_LIMIT_PER_MINUTE, default 20
	BWriteRateLimitPerMinute int64  // B_WRITE_RATE_LIMIT_PER_MINUTE, default 120
	BReadRateLimitPerMinute  int64  // B_READ_RATE_LIMIT_PER_MINUTE, default 600
}

// Load reads the environment and reports every missing required variable.
func Load() (Config, error) {
	var missing, invalid []string
	req := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			missing = append(missing, k)
		}
		return v
	}
	c := Config{
		HTTPAddr:           env("HTTP_ADDR", ":8080"),
		DatabaseURL:        req("DATABASE_URL"),
		NATSURL:            req("NATS_URL"),
		ValidatorAddr:      req("VALIDATOR_ADDR"),
		ValkeyAddr:         req("VALKEY_ADDR"),
		MinioEndpoint:      req("MINIO_ENDPOINT"),
		MinioAccessKey:     req("MINIO_ACCESS_KEY"),
		MinioSecretKey:     req("MINIO_SECRET_KEY"),
		MinioUseSSL:        os.Getenv("MINIO_USE_SSL") == "true",
		ZitadelIssuer:      req("ZITADEL_ISSUER"),
		ZitadelJWKSURL:     req("ZITADEL_JWKS_URL"),
		ZitadelAudience:    list(os.Getenv("ZITADEL_AUDIENCE")),
		ServiceName:        env("OTEL_SERVICE_NAME", "api-go"),
		RateLimitPerMinute: 60,

		StorageRegion:            env("STORAGE_REGION", "auto"),
		UploadRateLimitPerMinute: positive("UPLOAD_RATE_LIMIT_PER_MINUTE", 20, &invalid),
		BWriteRateLimitPerMinute: positive("B_WRITE_RATE_LIMIT_PER_MINUTE", 120, &invalid),
		BReadRateLimitPerMinute:  positive("B_READ_RATE_LIMIT_PER_MINUTE", 600, &invalid),
	}
	c.StoragePublicEndpoint = env("STORAGE_PUBLIC_ENDPOINT", c.MinioEndpoint)
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	if len(invalid) > 0 {
		return Config{}, fmt.Errorf("invalid env (want a positive integer): %s", strings.Join(invalid, ", "))
	}
	return c, nil
}

// OwnerDatabaseURL returns DATABASE_OWNER_URL, used only by `api migrate`.
func OwnerDatabaseURL() (string, error) {
	v := os.Getenv("DATABASE_OWNER_URL")
	if v == "" {
		return "", fmt.Errorf("missing required env: DATABASE_OWNER_URL")
	}
	return v, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// positive reads a positive integer env var, recording k in invalid when it is set to anything else.
func positive(k string, def int64, invalid *[]string) int64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		*invalid = append(*invalid, k)
		return def
	}
	return n
}

// list splits a comma-separated value, dropping blanks.
func list(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
