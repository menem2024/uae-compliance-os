// Command api is the api-go server. Subcommands: `migrate` applies database
// migrations with DATABASE_OWNER_URL; `healthcheck` probes /readyz (the
// distroless image has no curl).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/config"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpapi"
	"github.com/menem2024/uae-platform/services/api-go/internal/ratelimit"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
	"github.com/menem2024/uae-platform/services/api-go/internal/telemetry"
	"github.com/menem2024/uae-platform/services/api-go/internal/validator"
)

// Shutdown budget: the whole sequence must finish well under 10s.
const (
	httpShutdownTimeout  = 3 * time.Second
	tracerFlushTimeout   = 2 * time.Second
	startupRetryInterval = 2 * time.Second
)

func main() {
	slog.SetDefault(telemetry.NewLogger("api-go"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "migrate":
		err = migrate(ctx)
	case "healthcheck":
		err = healthcheck()
	case "", "serve":
		err = serve(ctx)
	default:
		err = fmt.Errorf("unknown command %q (want serve, migrate or healthcheck)", cmd)
	}
	if err != nil {
		slog.Error("api exited with error", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

func migrate(ctx context.Context) error {
	url, err := config.OwnerDatabaseURL()
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, url); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	slog.Info("migrations applied")
	return nil
}

func healthcheck() error {
	port := "8080"
	if _, p, err := net.SplitHostPort(os.Getenv("HTTP_ADDR")); err == nil && p != "" {
		port = p
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/readyz")
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: readyz returned %d", resp.StatusCode)
	}
	return nil
}

// retry runs fn until it succeeds or ctx is cancelled (SIGTERM during startup).
func retry(ctx context.Context, name string, fn func(context.Context) error) error {
	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := fn(attemptCtx)
		cancel()
		if err == nil {
			if attempt > 1 {
				slog.Info("dependency ready", "dependency", name, "attempts", attempt)
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("dependency not ready, retrying", "dependency", name, "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(startupRetryInterval):
		}
	}
}

func serve(ctx context.Context) (err error) {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(telemetry.NewLogger(cfg.ServiceName))

	shutdownTracer, err := telemetry.Init(ctx, cfg.ServiceName)
	if err != nil {
		return err
	}
	defer func() {
		fctx, cancel := context.WithTimeout(context.Background(), tracerFlushTimeout)
		defer cancel()
		if serr := shutdownTracer(fctx); serr != nil {
			slog.Warn("tracer flush failed", "err", serr)
		} else {
			slog.Info("tracer flushed")
		}
	}()

	// A SIGTERM while waiting for dependencies is a clean exit.
	startupAborted := func(e error) error {
		if errors.Is(e, context.Canceled) && ctx.Err() != nil {
			slog.Info("shutdown requested during startup")
			return nil
		}
		return e
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()
	if err := retry(ctx, "postgres", pool.Ping); err != nil {
		return startupAborted(err)
	}

	nc, jsh, err := connectNATS(ctx, cfg.NATSURL)
	if err != nil {
		return startupAborted(err)
	}
	defer nc.Close()

	vc, err := validator.New(cfg.ValidatorAddr)
	if err != nil {
		return err
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.ValkeyAddr, DialTimeout: 3 * time.Second})
	defer func() { _ = rdb.Close() }()

	mc, err := storage.New(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioUseSSL)
	if err != nil {
		return err
	}
	if err := retry(ctx, "minio", func(c context.Context) error {
		return storage.EnsureBucket(c, mc, storage.DocumentsBucket)
	}); err != nil {
		return startupAborted(err)
	}

	verifier, err := auth.NewJWKSVerifier(ctx, cfg.ZitadelIssuer, cfg.ZitadelJWKSURL)
	if err != nil {
		return startupAborted(err)
	}

	store := httpapi.PGStore{Pool: pool}
	consumerCtx, stopConsumer := context.WithCancel(ctx)
	defer stopConsumer()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runConsumer(consumerCtx, jsh, httpapi.HandleExtracted(store, vc))
	}()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(verifier, store, events.NewPublisher(jsh), ratelimit.New(rdb, cfg.RateLimitPerMinute), readiness(pool, nc, rdb, mc)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	srvErr := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
		close(srvErr)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err = <-srvErr:
		slog.Error("http server failed", "err", err)
	}

	// Stop HTTP and drain the consumer concurrently to stay within budget.
	stopConsumer()
	sctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	if serr := srv.Shutdown(sctx); serr != nil {
		slog.Warn("http shutdown", "err", serr)
	}
	wg.Wait()
	slog.Info("shutdown complete")
	return err
}

func connectNATS(ctx context.Context, url string) (*nats.Conn, jetstream.JetStream, error) {
	var nc *nats.Conn
	var js jetstream.JetStream
	err := retry(ctx, "nats", func(c context.Context) error {
		conn, j, err := events.Connect(url)
		if err != nil {
			return err
		}
		if err := events.EnsureStreams(c, j); err != nil {
			conn.Close()
			return err
		}
		nc, js = conn, j
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nc, js, nil
}

// runConsumer keeps the validation consumer running until ctx is cancelled,
// re-creating it if setup fails.
func runConsumer(ctx context.Context, js jetstream.JetStream, h events.ExtractedHandler) {
	for {
		err := events.RunValidationConsumer(ctx, js, h)
		if ctx.Err() != nil {
			return
		}
		slog.Error("validation consumer stopped, restarting", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(startupRetryInterval):
		}
	}
}

// readiness checks every dependency the API needs to serve traffic.
func readiness(pool *pgxpool.Pool, nc *nats.Conn, rdb *redis.Client, mc *minio.Client) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
		if st := nc.Status(); st != nats.CONNECTED {
			return fmt.Errorf("nats: status %s", st)
		}
		if err := rdb.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("valkey: %w", err)
		}
		if err := storage.CheckBucket(ctx, mc, storage.DocumentsBucket); err != nil {
			return fmt.Errorf("minio: %w", err)
		}
		return nil
	}
}
