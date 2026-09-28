// Package config loads api-go settings from the environment.
package config

import (
	"fmt"
	"os"
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
	ServiceName        string
	RateLimitPerMinute int64
}

// Load reads the environment and reports every missing required variable.
func Load() (Config, error) {
	var missing []string
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
		ServiceName:        env("OTEL_SERVICE_NAME", "api-go"),
		RateLimitPerMinute: 60,
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
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
