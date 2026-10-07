// Package trackb is the Track B composition root: the one place that builds every Track B
// dependency, mounts every Track B route and runs every Track B background loop. cmd/api/main.go
// holds exactly one Build/Run block; httpapi reaches the routes only through WithTrackB.
package trackb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"

	"github.com/menem2024/uae-platform/services/api-go/internal/agents"
	"github.com/menem2024/uae-platform/services/api-go/internal/clients"
	"github.com/menem2024/uae-platform/services/api-go/internal/documents"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/firms"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
	"github.com/menem2024/uae-platform/services/api-go/internal/ratelimit"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
)

const (
	// maxStreamsPerFirm and streamBuffer are the SSE limits of the Global Constraints.
	maxStreamsPerFirm = 20
	streamBuffer      = 64
	// reconcileInterval is the documents reconciler's pass interval.
	reconcileInterval = 60 * time.Second
	// firstSessionWait bounds how long Run waits for the first durable before starting the rest.
	firstSessionWait = 3 * time.Second
)

// Deps is everything Build needs from the process.
type Deps struct {
	Pool          *pgxpool.Pool
	NatsConn      *nats.Conn
	JS            jetstream.JetStream
	Redis         *redis.Client
	InternalMinio *minio.Client // the server's own client (Open, Delete); never signs browser URLs

	// The presigner signs URLs the browser can reach, so it has its own public endpoint.
	StoragePublicEndpoint, StorageAccessKey, StorageSecretKey, StorageRegion, StorageBucket string
	StorageUseSSL                                                                           bool

	UploadPerMinute, WritePerMinute, ReadPerMinute int64
	RetryInterval                                  time.Duration
}

func (d Deps) validate() error {
	switch {
	case d.Pool == nil:
		return errors.New("trackb: Deps.Pool is nil")
	case d.NatsConn == nil:
		return errors.New("trackb: Deps.NatsConn is nil")
	case d.JS == nil:
		return errors.New("trackb: Deps.JS is nil")
	case d.Redis == nil:
		return errors.New("trackb: Deps.Redis is nil")
	case d.InternalMinio == nil:
		return errors.New("trackb: Deps.InternalMinio is nil")
	case d.StorageBucket == "":
		return errors.New("trackb: Deps.StorageBucket is empty")
	case d.UploadPerMinute < 1 || d.WritePerMinute < 1 || d.ReadPerMinute < 1:
		return errors.New("trackb: rate limits must be positive")
	}
	return nil
}

// App owns every Track B route, consumer and background loop.
type App struct {
	nc       *nats.Conn
	hub      *agents.Hub
	registry *proposals.Registry

	mount func(chi.Router)

	docsDurable   *events.Durable
	agentsDurable *events.Durable
	reconciler    *documents.Reconciler
	tap           *agents.LiveTap
}

// Build constructs every Track B dependency. It starts nothing: call Run for the loops and mount
// RouterOption on the router.
func Build(_ context.Context, d Deps) (*App, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	retry := d.RetryInterval
	if retry <= 0 {
		retry = 2 * time.Second
	}

	presigner, err := storage.NewPresigner(d.StoragePublicEndpoint, d.StorageAccessKey, d.StorageSecretKey,
		d.StorageRegion, d.StorageUseSSL, d.StorageBucket)
	if err != nil {
		return nil, fmt.Errorf("trackb: %w", err)
	}
	objects := storage.NewObjectStore(d.InternalMinio, d.StorageBucket)
	bus := events.NewBus(d.JS)

	registry := proposals.NewRegistry()
	registry.Register(proposals.KindDocumentAttribution, proposals.AttributionApplier{})

	docStore := documents.PGStore{Pool: d.Pool}
	svc := &documents.Service{Store: docStore, Presigner: presigner, Objects: objects, Bus: bus}
	hub := agents.NewHub(maxStreamsPerFirm, streamBuffer)

	resolver := firmResolver{pool: d.Pool}
	read := httpx.Firm(resolver, ratelimit.NewNamed(d.Redis, "b-read", d.ReadPerMinute))
	write := httpx.Firm(resolver, ratelimit.NewNamed(d.Redis, "b-write", d.WritePerMinute))
	uploads := httpx.Firm(resolver, ratelimit.NewNamed(d.Redis, "uploads", d.UploadPerMinute))

	firmsH := firms.NewHandler(firms.PGStore{Pool: d.Pool})
	clientsH := clients.NewHandler(clients.PGStore{Pool: d.Pool})
	docsH := documents.NewHandler(svc)
	agentsH := agents.NewHandler(d.Pool, hub)

	docConsumer := &documents.Consumer{Store: docStore, Bus: bus}
	agentConsumer := &agents.Consumer{Pool: d.Pool}

	return &App{
		nc:       d.NatsConn,
		hub:      hub,
		registry: registry,
		mount: func(r chi.Router) {
			firmsH.Mount(r, read, write)
			clientsH.Mount(r, read, write)
			docsH.Mount(r, read, write, uploads)
			agentsH.Mount(r, read, write)
		},
		docsDurable:   events.NewDurable(d.JS, documents.EventsDurableConfig(), docConsumer.Handle, retry),
		agentsDurable: events.NewDurable(d.JS, agents.EventsDurableConfig(), agentConsumer.Handle, retry),
		reconciler: &documents.Reconciler{Firms: docStore, Store: docStore, Service: svc, Bus: bus,
			Interval: reconcileInterval},
		tap: &agents.LiveTap{Hub: hub},
	}, nil
}

// RouterOption mounts every Track B route. It is the only registration path; httpapi.WithTrackB
// forwards to it. The returned func has httpapi.RouterOption's underlying type (trackb cannot
// import httpapi without a cycle).
func (a *App) RouterOption() func(chi.Router) { return a.mount }

// Run starts every Track B background loop (both durables, the documents reconciler and the live
// tap) and blocks until ctx is done, or until the tap cannot subscribe (the only fatal start-up
// error: the consumers and the reconciler retry internally). Every loop has returned when Run does.
func (a *App) Run(ctx context.Context) error {
	unsubscribe, err := a.tap.Start(a.nc)
	if err != nil {
		return fmt.Errorf("trackb: live tap: %w", err)
	}
	defer unsubscribe()

	var wg sync.WaitGroup
	wg.Go(func() { a.docsDurable.Run(ctx) })
	// Each durable's first session re-ensures the streams (a stream update). Start the second only
	// once the first is consuming, so two updates of the same stream never overlap.
	awaitReady(ctx, a.docsDurable, firstSessionWait)
	wg.Go(func() { a.agentsDurable.Run(ctx) })
	wg.Go(func() { a.reconciler.Run(ctx) })
	<-ctx.Done()
	wg.Wait()
	return nil
}

// awaitReady polls d.Ready until it succeeds, ctx is done or max elapses (a down NATS server must
// not hold the other loops back: they retry on their own).
func awaitReady(ctx context.Context, d *events.Durable, max time.Duration) {
	deadline := time.NewTimer(max)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		rctx, cancel := context.WithTimeout(ctx, time.Second)
		err := d.Ready(rctx)
		cancel()
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-tick.C:
		}
	}
}

// Ready reports whether Track B can serve traffic: NATS connected and both durables consuming.
func (a *App) Ready(ctx context.Context) error {
	if st := a.nc.Status(); st != nats.CONNECTED {
		return fmt.Errorf("nats: status %s", st)
	}
	if err := a.docsDurable.Ready(ctx); err != nil {
		return err
	}
	return a.agentsDurable.Ready(ctx)
}

// Shutdown closes every live SSE stream so no handler goroutine outlives the HTTP server. Call it
// before the server's shutdown deadline; it is idempotent.
func (a *App) Shutdown() { a.hub.CloseAll() }
