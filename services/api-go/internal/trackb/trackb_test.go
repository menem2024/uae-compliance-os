package trackb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackb"
)

// fakeDeps builds Deps from an embedded JetStream and miniredis. The pool and the MinIO client are
// lazy: nothing in these tests reaches Postgres or MinIO.
func fakeDeps(t *testing.T) (trackb.Deps, eventstest.JetStream) {
	t.Helper()
	js := eventstest.StartJetStream(t)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	pool, err := pgxpool.New(context.Background(), "postgres://nobody:nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mc, err := storage.New("127.0.0.1:1", "ak", "sk", false)
	if err != nil {
		t.Fatal(err)
	}
	return trackb.Deps{
		Pool: pool, NatsConn: js.Conn, JS: js.JS, Redis: rdb, InternalMinio: mc,
		StoragePublicEndpoint: "files.example.test:9000", StorageAccessKey: "ak", StorageSecretKey: "sk",
		StorageRegion: "auto", StorageBucket: storage.DocumentsBucket,
		UploadPerMinute: 20, WritePerMinute: 120, ReadPerMinute: 600, RetryInterval: 50 * time.Millisecond,
	}, js
}

var wantRoutes = []string{
	"GET /v1/firm",
	"PATCH /v1/firm",
	"GET /v1/client-companies",
	"POST /v1/client-companies",
	"GET /v1/client-companies/{id}",
	"PATCH /v1/client-companies/{id}",
	"POST /v1/client-companies/{id}/archive",
	"POST /v1/client-companies/{id}/restore",
	"POST /v1/documents/uploads",
	"POST /v1/documents/complete",
	"GET /v1/documents",
	"GET /v1/documents/{id}",
	"GET /v1/documents/{id}/download",
	"POST /v1/documents/{id}/reprocess",
	"GET /v1/agents/runs",
	"GET /v1/agents/runs/{id}",
	"GET /v1/agents/summary",
	"GET /v1/agents/activity",
}

func TestBuildMountsEveryTrackBRoute(t *testing.T) {
	d, _ := fakeDeps(t)
	app, err := trackb.Build(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	app.RouterOption()(r)
	var got []string
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := append([]string(nil), wantRoutes...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("routes differ\n got: %q\nwant: %q", got, want)
	}
	for _, g := range got {
		if strings.Contains(g, "/v1/proposals") {
			t.Fatalf("Track B must not mount Track C route %s", g)
		}
	}
}

// Every route passes through the per-Firm middleware first: with a bare router (no auth group) the
// firm resolver sees no principal, and the request never reaches a handler or the (unreachable) DB.
func TestRoutesSitBehindTheFirmMiddleware(t *testing.T) {
	d, _ := fakeDeps(t)
	app, err := trackb.Build(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	app.RouterOption()(r)
	for _, route := range wantRoutes {
		method, path, _ := strings.Cut(route, " ")
		path = strings.ReplaceAll(path, "{id}", uuid.NewString())
		rec := httptest.NewRecorder()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		r.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, method, path, strings.NewReader("{}")))
		cancel()
		// Firm resolution fails (no organisation / DB unreachable) before any handler logic.
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status %d body %s", route, rec.Code, rec.Body.String())
		}
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	d, _ := fakeDeps(t)
	app, err := trackb.Build(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()

	// Ready turns nil once both durables are consuming.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rctx, rcancel := context.WithTimeout(ctx, time.Second)
		err := app.Ready(rctx)
		rcancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never became ready: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after cancel", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of cancel")
	}
}

func TestReadyFailsBeforeRun(t *testing.T) {
	d, _ := fakeDeps(t)
	app, err := trackb.Build(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Ready(context.Background()); err == nil {
		t.Fatal("Ready must fail while the durables are not consuming")
	}
}

func TestShutdownClosesLiveStreams(t *testing.T) {
	d, _ := fakeDeps(t)
	app, err := trackb.Build(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	firm := uuid.New()
	sub, err := app.Hub().Subscribe(firm)
	if err != nil {
		t.Fatal(err)
	}
	app.Shutdown()
	app.Shutdown() // idempotent
	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Fatal("got an event, want a closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not close the subscriber")
	}
}

func TestBuildRejectsMissingDeps(t *testing.T) {
	d, _ := fakeDeps(t)
	d.Pool = nil
	if _, err := trackb.Build(context.Background(), d); err == nil {
		t.Fatal("Build with a nil Pool must fail")
	}
}
