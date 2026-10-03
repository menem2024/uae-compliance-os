//go:build unix

package chaos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestKillReady(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    Counts
		want bool
	}{
		{"nothing yet", Counts{}, false},
		{"terminal but no run in flight", Counts{Terminal: 400}, false},
		{"run in flight, too few terminal", Counts{Terminal: KillAfterTerminal - 1, RunningRuns: 3}, false},
		{"exactly the spec threshold", Counts{Terminal: KillAfterTerminal, RunningRuns: 1}, true},
	} {
		if got := tc.c.KillReady(); got != tc.want {
			t.Errorf("%s: KillReady() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPassChecksEveryConditionIncludingDuplicates(t *testing.T) {
	const n = 1000
	ok := Counts{Documents: n, Terminal: n, Extracted: n, AbandonedRuns: 1, Invoices: n, DistinctInvoiceDocs: n}
	if err := ok.Pass(n, 0); err != nil {
		t.Fatalf("clean result rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		c    Counts
		dlq  uint64
		want string
	}{
		{"a Document missing", with(ok, func(c *Counts) { c.Documents = n - 1 }), 0, "documents"},
		{"a Document not extracted", with(ok, func(c *Counts) { c.Extracted = n - 1 }), 0, "extracted"},
		{"an invoice dropped", with(ok, func(c *Counts) { c.Invoices, c.DistinctInvoiceDocs = n-1, n-1 }), 0, "invoices"},
		// The pair the brief warns about: the total is right only because one Document got two
		// invoices and another got none.
		{"dup/drop pair", with(ok, func(c *Counts) { c.DistinctInvoiceDocs = n - 1 }), 0, "distinct"},
		{"a dead letter", ok, 1, "dlq"},
		{"no superseded run", with(ok, func(c *Counts) { c.AbandonedRuns = 0 }), 0, "abandoned"},
	} {
		err := tc.c.Pass(n, tc.dlq)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Pass() = %v, want an error mentioning %q", tc.name, err, tc.want)
		}
	}
}

func with(c Counts, f func(*Counts)) Counts { f(&c); return c }

func TestChildEnvDropsModelKeysAndAppliesOverrides(t *testing.T) {
	base := []string{"PATH=/bin", "ANTHROPIC_API_KEY=sk-live", "LANGFUSE_SECRET_KEY=x", "LANGFUSE_PUBLIC_KEY=y",
		"AI_GATEWAY=anthropic", "HOME=/home/u"}
	got := ChildEnv(base, map[string]string{"AI_GATEWAY": "fake", "NATS_URL": "nats://127.0.0.1:1"})
	slices.Sort(got)
	want := []string{"AI_GATEWAY=fake", "HOME=/home/u", "NATS_URL=nats://127.0.0.1:1", "PATH=/bin"}
	if !slices.Equal(got, want) {
		t.Fatalf("ChildEnv = %v, want %v", got, want)
	}
}

func TestAIPyEnvIsTheFakeGatewayOnTheThrowawayStack(t *testing.T) {
	env := AIPyEnv(AIPyConfig{NatsURL: "nats://127.0.0.1:4222", S3Endpoint: "127.0.0.1:45292", S3AccessKey: "minio",
		S3SecretKey: "minio_dev_pw", ValkeyURL: "redis://127.0.0.1:6390/0", HealthPort: 18081, LatencyMS: 50})
	for k, v := range map[string]string{
		"AI_GATEWAY": "fake", "AI_FAKE_SCENARIO": "", "AI_FAKE_LATENCY_MS": "50", "NATS_URL": "nats://127.0.0.1:4222",
		"S3_ENDPOINT": "127.0.0.1:45292", "S3_ACCESS_KEY": "minio", "S3_SECRET_KEY": "minio_dev_pw",
		"S3_BUCKET": "documents", "S3_USE_SSL": "false", "VALKEY_URL": "redis://127.0.0.1:6390/0",
		"AI_HEALTH_PORT": "18081",
	} {
		if got, ok := env[k]; !ok || got != v {
			t.Errorf("%s = %q (set %v), want %q", k, got, ok, v)
		}
	}
	if _, ok := env["ANTHROPIC_API_KEY"]; ok {
		t.Error("the worker must never get a model key")
	}
}

// uv run spawns python as a child (checked on this host: python's ppid is uv's pid), so a SIGKILL of
// uv alone would leave the worker running. Kill must take down the whole process group.
func TestKillSIGKILLsTheWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	p, err := StartProcess(dir, os.Environ(), io.Discard, "sh", "-c", "sleep 300 & echo $! > child.pid; wait")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Kill() })
	var child int
	deadline := time.Now().Add(5 * time.Second)
	for child == 0 {
		if b, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(b), "\n") {
			child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		if time.Now().After(deadline) {
			t.Fatal("child never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if !p.KilledBySIGKILL() {
		t.Fatalf("process state %v, want killed by SIGKILL", p.State())
	}
	for alive(child) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d survived the group kill", child)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !p.Exited() {
		t.Fatal("Exited() = false after Kill")
	}
}

// alive reports whether pid exists and is not a zombie.
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	_, rest, _ := strings.Cut(string(b), ") ")
	return !strings.HasPrefix(rest, "Z")
}

// fakeAPI is just enough of /v1/documents/{uploads,complete} plus a presigned-PUT target to check the
// driver's chunking, its concurrency bound and that it refuses a deduplicated batch.
type fakeAPI struct {
	mu        sync.Mutex
	puts      map[string][]byte
	completed []string
	chunks    []int
	inFlight  atomic.Int32
	maxFlight atomic.Int32
	dedupAll  bool
	srv       *httptest.Server
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{puts: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/documents/uploads", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		var in struct {
			ClientCompanyID string `json:"client_company_id"`
			Files           []struct {
				Filename    string `json:"filename"`
				ContentType string `json:"content_type"`
				SizeBytes   int64  `json:"size_bytes"`
				SHA256      string `json:"sha256"`
			} `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ClientCompanyID == "" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.chunks = append(f.chunks, len(in.Files))
		f.mu.Unlock()
		items := make([]map[string]any, len(in.Files))
		for i, file := range in.Files {
			id := uuid.NewString()
			if f.dedupAll {
				items[i] = map[string]any{"document_id": id, "status": "uploaded", "deduplicated": true, "upload": nil}
				continue
			}
			items[i] = map[string]any{"document_id": id, "status": "pending_upload", "deduplicated": false,
				"upload": map[string]any{"url": f.srv.URL + "/s3/" + id + "?sha=" + file.SHA256 + "&X-Amz-Signature=x",
					"method": "PUT", "headers": map[string]string{"Content-Type": file.ContentType}}}
		}
		writeJSON(w, map[string]any{"items": items})
	})
	mux.HandleFunc("PUT /s3/{id}", func(w http.ResponseWriter, r *http.Request) {
		n := f.inFlight.Add(1)
		defer f.inFlight.Add(-1)
		for {
			m := f.maxFlight.Load()
			if n <= m || f.maxFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		body, _ := io.ReadAll(r.Body)
		sum := sha256.Sum256(body)
		if r.Header.Get("Content-Type") != "application/pdf" || r.ContentLength != int64(len(body)) ||
			hex.EncodeToString(sum[:]) != r.URL.Query().Get("sha") {
			http.Error(w, "mismatch", http.StatusForbidden)
			return
		}
		f.mu.Lock()
		f.puts[r.PathValue("id")] = body
		f.mu.Unlock()
	})
	mux.HandleFunc("POST /v1/documents/complete", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			DocumentIDs []string `json:"document_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		items := make([]map[string]any, len(in.DocumentIDs))
		f.mu.Lock()
		for i, id := range in.DocumentIDs {
			status := "uploaded"
			if _, ok := f.puts[id]; !ok {
				status = "pending_upload"
			}
			items[i] = map[string]any{"document_id": id, "status": status}
			f.completed = append(f.completed, id)
		}
		f.mu.Unlock()
		writeJSON(w, map[string]any{"items": items})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func pdfFixtures(t *testing.T, n int) []Fixture {
	t.Helper()
	dir := t.TempDir()
	out := make([]Fixture, n)
	for i := range n {
		body := []byte("%PDF-1.4\n% chaos fixture " + strconv.Itoa(i) + "\n%%EOF\n")
		path := filepath.Join(dir, "f"+strconv.Itoa(i)+".pdf")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		out[i] = Fixture{Path: path, Filename: filepath.Base(path), ContentType: "application/pdf",
			Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
	}
	return out
}

func TestUploadBatchChunksBoundsConcurrencyAndCompletesEveryDocument(t *testing.T) {
	f := newFakeAPI(t)
	c := &Client{Base: f.srv.URL, Token: "tok", HTTP: f.srv.Client()}
	fx := pdfFixtures(t, 230)
	ids, err := c.UploadBatch(context.Background(), uuid.New(), fx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != len(fx) || len(f.puts) != len(fx) || len(f.completed) != len(fx) {
		t.Fatalf("ids=%d puts=%d completed=%d, want %d each", len(ids), len(f.puts), len(f.completed), len(fx))
	}
	if !slices.Equal(f.chunks, []int{100, 100, 30}) {
		t.Fatalf("upload request sizes %v, want [100 100 30] (MaxFiles per request)", f.chunks)
	}
	if m := f.maxFlight.Load(); m > 7 || m < 2 {
		t.Fatalf("max concurrent PUTs = %d, want 2..7", m)
	}
}

func TestUploadBatchRefusesADeduplicatedFixture(t *testing.T) {
	f := newFakeAPI(t)
	f.dedupAll = true
	c := &Client{Base: f.srv.URL, Token: "tok", HTTP: f.srv.Client()}
	_, err := c.UploadBatch(context.Background(), uuid.New(), pdfFixtures(t, 3), 2)
	if err == nil || !strings.Contains(err.Error(), "deduplicated") {
		t.Fatalf("err = %v, want a deduplicated error (the batch must be 1,000 distinct Documents)", err)
	}
}
