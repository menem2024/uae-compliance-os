//go:build chaos && unix

package chaos

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
)

// TestBatchChaos1000 is AC-E2: 1,000 uploads through the real HTTP path, a SIGKILL of the ai-py
// worker with a run in flight, a restart, and a within-10-minute proof that nothing was lost or
// duplicated. Run it only through scripts/phase1-chaos.sh, which owns the throwaway Postgres and
// MinIO containers and passes their endpoints in (env below); it fails, never skips, when they are
// missing, so a misconfigured run cannot pass silently.
func TestBatchChaos1000(t *testing.T) {
	cfg := RequireChaosEnv(t)

	// 1. Fixtures first (about 0.1 s per PDF): ai.synthetic.generator, seeds past the eval set.
	fx, err := GenerateBatch(context.Background(), cfg.AIPyDir, Batch, SeedBase, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// 2. api-go in-process on the throwaway Postgres and MinIO and an embedded JetStream.
	env := dbtest.Setup(t)
	js := eventstest.StartJetStream(t)
	mr := miniredis.RunT(t) // api-go's rate limiters and ai-py's VALKEY_URL
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	mc, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, false)
	if err != nil {
		t.Fatal(err)
	}
	bctx, bcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer bcancel()
	if err := storage.EnsureBucket(bctx, mc, storage.DocumentsBucket); err != nil {
		t.Fatal(err)
	}
	api := StartAPI(t, APIConfig{DB: env, JS: js, Redis: rdb, S3Endpoint: cfg.S3Endpoint,
		S3AccessKey: cfg.S3AccessKey, S3SecretKey: cfg.S3SecretKey})
	client := api.Client(env.OrgA)
	cc, err := client.CreateClientCompany(bctx, "Chaos Trading LLC")
	if err != nil {
		t.Fatal(err)
	}

	// 3. The real worker process, and proof it is consuming before the batch starts.
	aiEnv := AIPyEnv(AIPyConfig{NatsURL: js.URL, S3Endpoint: cfg.S3Endpoint, S3AccessKey: cfg.S3AccessKey,
		S3SecretKey: cfg.S3SecretKey, ValkeyURL: "redis://" + mr.Addr() + "/0", HealthPort: FreePort(t),
		Scenario: cfg.Scenario, LatencyMS: cfg.LatencyMS})
	worker := StartAIPy(t, cfg, aiEnv, "aipy-1")
	if err := WaitForConsumer(js.JS, events.DocumentsStream, AIDocumentsDurable, worker, 3*time.Minute); err != nil {
		t.Fatalf("%v\n%s", err, worker.LogTail())
	}

	// 4. 1,000 distinct Documents through /v1/documents/uploads, the presigned PUT and /complete,
	// while the worker is already extracting (the kill may land mid-upload: it is mid-batch either way).
	ctx, cancel := context.WithCancel(context.Background()) // go test -timeout bounds the whole run
	defer cancel()
	var ids []uuid.UUID
	uploaded := make(chan error, 1)
	go func() {
		var err error
		ids, err = client.UploadBatch(ctx, cc, fx, UploadConcurrency)
		uploaded <- err
	}()
	uploadDone := false
	awaitUpload := func() {
		t.Helper()
		if !uploadDone {
			uploadDone = true
			if err := <-uploaded; err != nil {
				t.Fatal(err)
			}
		}
	}

	// 5. The spec's kill condition, then an unclean death of the whole worker process group.
	atKill, err := PollUntil(ctx, 5*time.Minute, 50*time.Millisecond, func(ctx context.Context) (Counts, bool, error) {
		select {
		case err := <-uploaded:
			uploadDone = true
			if err != nil {
				return Counts{}, false, fmt.Errorf("upload: %w", err)
			}
		default:
		}
		c, err := ReadCounts(ctx, env, env.FirmA)
		if err == nil && c.Terminal >= Batch {
			return c, false, fmt.Errorf("all %d Documents finished before a kill window: raise CHAOS_AI_FAKE_LATENCY_MS",
				Batch)
		}
		return c, c.KillReady(), err
	})
	if err != nil {
		t.Fatalf("kill condition never met: %v (counts %+v)\n%s", err, atKill, worker.LogTail())
	}
	if err := worker.Kill(); err != nil {
		t.Fatal(err)
	}
	if !worker.KilledBySIGKILL() {
		t.Fatalf("worker state %v, want killed by SIGKILL", worker.State())
	}
	t.Logf("SIGKILL at %+v", atKill)

	// 6. Restart with the same env a moment later.
	time.Sleep(2 * time.Second)
	restarted := StartAIPy(t, cfg, aiEnv, "aipy-2")
	awaitUpload()
	if len(ids) != Batch {
		t.Fatalf("%d Documents uploaded, want %d", len(ids), Batch)
	}

	// 7. The pass condition within 10 minutes: every Document extracted, exactly one invoice each, no
	// dead letters, and at least one orphaned run abandoned by its redelivery.
	var lastErr error
	final, err := PollUntil(ctx, PassTimeout, time.Second, func(ctx context.Context) (Counts, bool, error) {
		c, err := ReadCounts(ctx, env, env.FirmA)
		if err != nil {
			return c, false, err
		}
		lastErr = c.Pass(Batch, eventstest.StreamMsgs(t, js.JS, events.DLQStream))
		return c, lastErr == nil, nil
	})
	if err != nil {
		t.Fatalf("not proven within %v: %v (last counts %+v)\n%s", PassTimeout, lastErr, final, restarted.LogTail())
	}
	if final.Extracted != Batch || final.Invoices != Batch || final.DistinctInvoiceDocs != Batch ||
		final.AbandonedRuns < 1 {
		t.Fatalf("final counts %+v", final)
	}
	if n := eventstest.StreamMsgs(t, js.JS, events.DLQStream); n != 0 {
		t.Fatalf("DLQ holds %d messages", n)
	}
	t.Logf("AC-E2 proven: %+v", final)
}

// The fixture step on its own (a handful of PDFs), so the uv/generator plumbing can be checked
// without the containers: go test -tags chaos -run TestGenerateBatch ./internal/chaos/
func TestGenerateBatch(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "ai-py"))
	if err != nil {
		t.Fatal(err)
	}
	fx, err := GenerateBatch(context.Background(), dir, 4, SeedBase, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(fx) != 4 {
		t.Fatalf("%d fixtures, want 4", len(fx))
	}
	seen := map[string]bool{}
	for _, f := range fx {
		b, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(b, []byte("%PDF-")) || int64(len(b)) != f.Size || f.ContentType != "application/pdf" {
			t.Fatalf("fixture %s: %d bytes (declared %d), %q", f.Filename, len(b), f.Size, f.ContentType)
		}
		seen[f.SHA256] = true
	}
	if len(seen) != 4 {
		t.Fatalf("%d distinct sha256 for 4 fixtures", len(seen))
	}
}
