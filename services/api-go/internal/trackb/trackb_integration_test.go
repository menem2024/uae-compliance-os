//go:build integration

package trackb_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpapi"
	"github.com/menem2024/uae-platform/services/api-go/internal/ratelimit"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackb"
)

type orgVerifier struct{ orgs map[string]string }

func (v orgVerifier) Verify(_ context.Context, tok string) (auth.Principal, error) {
	return auth.Principal{Subject: "user-" + tok, OrgID: v.orgs[tok]}, nil
}

// A full Build against real Postgres and an embedded JetStream: an agent.run.started published on the
// real AGENTS stream reaches agent_runs and the Document's status through the real api-agent-events
// Durable (no fakes on this path), and the same App serves the rows over HTTP to the right Firm only.
func TestBuildWiresRealConsumersAndRoutes(t *testing.T) {
	env := dbtest.Setup(t)
	js := eventstest.StartJetStream(t)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	mc, err := storage.New("127.0.0.1:1", "ak", "sk", false)
	if err != nil {
		t.Fatal(err)
	}
	app, err := trackb.Build(context.Background(), trackb.Deps{
		Pool: env.App, NatsConn: js.Conn, JS: js.JS, Redis: rdb, InternalMinio: mc,
		StoragePublicEndpoint: "files.example.test:9000", StorageAccessKey: "ak", StorageSecretKey: "sk",
		StorageRegion: "auto", StorageBucket: storage.DocumentsBucket,
		UploadPerMinute: 20, WritePerMinute: 120, ReadPerMinute: 600, RetryInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	deadline := time.Now().Add(10 * time.Second)
	for app.Ready(ctx) != nil {
		if time.Now().After(deadline) {
			t.Fatalf("not ready: %v", app.Ready(ctx))
		}
		time.Sleep(20 * time.Millisecond)
	}

	cc := dbtest.ClientCompany(t, env, env.FirmA, "Oasis", "")
	doc, run := uuid.New(), uuid.New()
	if _, err := dbtest.ExecFirm(ctx, env, env.FirmA, `INSERT INTO documents (id, firm_id, client_company_id, sha256, object_key, filename,
		content_type, size_bytes, status) VALUES ($1::uuid, $2::uuid, $3, repeat('b', 64), 'firms/' || $2::text || '/docs/' || $1::text,
		'a.pdf', 'application/pdf', 10, 'uploaded')`, doc, env.FirmA, cc); err != nil {
		t.Fatal(err)
	}
	err = events.NewBus(js.JS).Publish(ctx, events.AgentRunStartedSubject, events.AgentRunStartedSubject+":"+run.String(),
		&compliancev1.AgentRunStarted{RunId: run.String(), FirmId: env.FirmA.String(), ClientCompanyId: cc.String(),
			Workflow: "document_ingestion@1", SubjectType: "document", SubjectId: doc.String(),
			StartedAt: timestamppb.Now(), DeliveryAttempt: 1})
	if err != nil {
		t.Fatal(err)
	}

	var runStatus, docStatus string
	for {
		runStatus, docStatus = "", ""
		_ = dbtest.QueryRowFirm(ctx, env, env.FirmA, `SELECT status FROM agent_runs WHERE id = $1`, run).Scan(&runStatus)
		_ = dbtest.QueryRowFirm(ctx, env, env.FirmA, `SELECT status FROM documents WHERE id = $1`, doc).Scan(&docStatus)
		if runStatus == "running" && docStatus == "processing" {
			break
		}
		if time.Now().After(deadline.Add(10 * time.Second)) {
			t.Fatalf("agent_runs/documents never updated: run=%q doc=%q", runStatus, docStatus)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Over HTTP, through the real router: Firm A sees the run, Firm B sees nothing of it.
	h := httpapi.NewRouter(orgVerifier{orgs: map[string]string{"a": env.OrgA, "b": env.OrgB}}, httpapi.PGStore{Pool: env.App},
		events.NewPublisher(js.JS), ratelimit.New(rdb, 60), func(context.Context) error { return nil }, httpapi.WithTrackB(app))
	list := func(tok string) []string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/agents/runs", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tok, rec.Code, rec.Body.String())
		}
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, it := range page.Items {
			ids = append(ids, it.ID)
		}
		return ids
	}
	if got := list("a"); len(got) != 1 || got[0] != run.String() {
		t.Fatalf("firm A runs = %v, want [%s]", got, run)
	}
	if got := list("b"); len(got) != 0 {
		t.Fatalf("firm B sees %v", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/firm", nil)
	req.Header.Set("Authorization", "Bearer a")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), env.FirmA.String()) {
		t.Fatalf("GET /v1/firm: %d %s", rec.Code, rec.Body.String())
	}

	app.Shutdown()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
