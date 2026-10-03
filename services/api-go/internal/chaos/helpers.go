//go:build unix

// Package chaos is the AC-E2 chaos harness (TestBatchChaos1000, build tag chaos): api-go in-process
// through Task 30's composition root, ai-py as a real OS process, and the HTTP upload driver, kill
// switch and counters the test needs. scripts/phase1-chaos.sh owns the throwaway Postgres and MinIO
// containers and is the only supported way to run the chaos test.
package chaos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"

	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpapi"
	"github.com/menem2024/uae-platform/services/api-go/internal/ratelimit"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackb"
)

// The AC-E2 numbers (spec, Task 29 brief).
const (
	Batch             = 1000
	KillAfterTerminal = 300 // SIGKILL once at least this many Documents are terminal and a run is in flight
	PassTimeout       = 10 * time.Minute
	UploadConcurrency = 20
	// SeedBase is past every seed of the committed eval datasets (ai/synthetic/datasets.py stays
	// below 2,400), so no chaos fixture is an eval document.
	SeedBase = 100_000
	// MaxFilesPerRequest is the upload contract's per-request limit (documents.MaxFiles).
	MaxFilesPerRequest = 100
	// AIDocumentsDurable is ai-py's document consumer (contract section 4).
	AIDocumentsDurable = "ai-documents"
)

// Counts is everything the kill and pass conditions read, for one Firm.
type Counts struct {
	Documents           int64 // every Document
	Terminal            int64 // past uploaded/processing: extracted, needs_review, not_invoice, failed, rejected
	Extracted           int64
	RunningRuns         int64
	AbandonedRuns       int64
	Invoices            int64 // invoices WHERE document_id IS NOT NULL
	DistinctInvoiceDocs int64 // COUNT(DISTINCT document_id) of the same rows
}

// KillReady is the spec's kill condition: at least one run in flight and at least
// KillAfterTerminal terminal Documents.
func (c Counts) KillReady() bool { return c.RunningRuns >= 1 && c.Terminal >= KillAfterTerminal }

// Pass returns nil when nothing was lost or duplicated for a batch of n, else the first unmet
// condition. dlq is the number of messages in the DLQ stream (every dlq.* subject).
func (c Counts) Pass(n int64, dlq uint64) error {
	switch {
	case c.Documents != n:
		return fmt.Errorf("documents = %d, want %d", c.Documents, n)
	case c.Extracted != n:
		return fmt.Errorf("extracted = %d, want %d", c.Extracted, n)
	case c.Invoices != n:
		return fmt.Errorf("invoices with a document_id = %d, want %d", c.Invoices, n)
	case c.DistinctInvoiceDocs != n:
		// Checked separately from the total: a duplicated invoice and a dropped one add up to n.
		return fmt.Errorf("distinct invoice document_ids = %d, want %d", c.DistinctInvoiceDocs, n)
	case dlq != 0:
		return fmt.Errorf("dlq holds %d messages, want 0", dlq)
	case c.AbandonedRuns < 1:
		return errors.New("no abandoned agent_run: no redelivered run superseded an orphaned one")
	}
	return nil
}

const countsSQL = `SELECT
  (SELECT count(*) FROM documents WHERE firm_id = $1),
  (SELECT count(*) FROM documents WHERE firm_id = $1 AND status NOT IN ('pending_upload','uploaded','processing')),
  (SELECT count(*) FROM documents WHERE firm_id = $1 AND status = 'extracted'),
  (SELECT count(*) FROM agent_runs WHERE firm_id = $1 AND status = 'running'),
  (SELECT count(*) FROM agent_runs WHERE firm_id = $1 AND status = 'abandoned'),
  (SELECT count(*) FROM invoices WHERE firm_id = $1 AND document_id IS NOT NULL),
  (SELECT count(DISTINCT document_id) FROM invoices WHERE firm_id = $1 AND document_id IS NOT NULL)`

// ReadCounts reads Counts for firm as the owner role (every table is FORCE ROW LEVEL SECURITY, so the
// read carries app.firm_id).
func ReadCounts(ctx context.Context, e dbtest.Env, firm uuid.UUID) (Counts, error) {
	var c Counts
	err := dbtest.QueryRowFirm(ctx, e, firm, countsSQL, firm).Scan(&c.Documents, &c.Terminal, &c.Extracted,
		&c.RunningRuns, &c.AbandonedRuns, &c.Invoices, &c.DistinctInvoiceDocs)
	return c, err
}

// PollUntil calls fn every interval until it reports done, fn errors, ctx ends or timeout elapses;
// it returns the last Counts fn produced.
func PollUntil(ctx context.Context, timeout, interval time.Duration,
	fn func(context.Context) (Counts, bool, error)) (Counts, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		c, done, err := fn(ctx)
		if err != nil || done {
			return c, err
		}
		select {
		case <-ctx.Done():
			return c, ctx.Err()
		case <-tick.C:
		}
	}
}

// ChaosConfig is the environment scripts/phase1-chaos.sh hands the chaos test.
type ChaosConfig struct {
	AIPyDir                              string // services/ai-py (CHAOS_AIPY_DIR)
	S3Endpoint, S3AccessKey, S3SecretKey string // the throwaway MinIO (CHAOS_S3_*)
	Scenario                             string // AI_FAKE_SCENARIO for the worker; "" = ai-py's packaged default
	LogDir                               string // worker logs and .pgid files (CHAOS_LOG_DIR)
	LatencyMS                            int    // AI_FAKE_LATENCY_MS (CHAOS_AI_FAKE_LATENCY_MS, default 50)
}

// RequireChaosEnv reads ChaosConfig and fails (never skips) when the stack is missing.
func RequireChaosEnv(t testing.TB) ChaosConfig {
	t.Helper()
	for _, k := range []string{"TEST_OWNER_URL", "TEST_APP_URL", "CHAOS_S3_ENDPOINT"} {
		if os.Getenv(k) == "" {
			t.Fatalf("%s is not set: run the chaos test through scripts/phase1-chaos.sh", k)
		}
	}
	c := ChaosConfig{
		AIPyDir: os.Getenv("CHAOS_AIPY_DIR"), S3Endpoint: os.Getenv("CHAOS_S3_ENDPOINT"),
		S3AccessKey: envOr("CHAOS_S3_ACCESS_KEY", "minio"), S3SecretKey: envOr("CHAOS_S3_SECRET_KEY", "minio_dev_pw"),
		Scenario: os.Getenv("CHAOS_AI_FAKE_SCENARIO"), LogDir: os.Getenv("CHAOS_LOG_DIR"), LatencyMS: 50,
	}
	if c.AIPyDir == "" {
		// go test runs in services/api-go/internal/chaos.
		dir, err := filepath.Abs(filepath.Join("..", "..", "..", "ai-py"))
		if err != nil {
			t.Fatal(err)
		}
		c.AIPyDir = dir
	}
	if c.LogDir == "" {
		c.LogDir = t.TempDir()
	}
	if raw := os.Getenv("CHAOS_AI_FAKE_LATENCY_MS"); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err != nil || ms < 0 {
			t.Fatalf("CHAOS_AI_FAKE_LATENCY_MS=%q", raw)
		}
		c.LatencyMS = ms
	}
	return c
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// FreePort returns a loopback TCP port that was free a moment ago.
func FreePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// AIPyConfig is what the worker needs to reach the throwaway stack.
type AIPyConfig struct {
	NatsURL, S3Endpoint, S3AccessKey, S3SecretKey, ValkeyURL, Scenario string
	HealthPort, LatencyMS                                              int
}

// AIPyEnv is the worker's environment (ai.settings.Settings names). AI_GATEWAY is always fake: no
// test makes a network LLM call, and ChildEnv strips ANTHROPIC_API_KEY from the inherited env too.
func AIPyEnv(c AIPyConfig) map[string]string {
	return map[string]string{
		"AI_GATEWAY":         "fake",
		"AI_FAKE_SCENARIO":   c.Scenario,
		"AI_FAKE_LATENCY_MS": strconv.Itoa(c.LatencyMS), // keeps runs in flight long enough to kill one
		"AI_CACHE":           "none",                    // every Document goes through the gateway
		"VALKEY_URL":         c.ValkeyURL,
		"AI_HEALTH_PORT":     strconv.Itoa(c.HealthPort),
		"NATS_URL":           c.NatsURL,
		"S3_ENDPOINT":        c.S3Endpoint,
		"S3_ACCESS_KEY":      c.S3AccessKey,
		"S3_SECRET_KEY":      c.S3SecretKey,
		"S3_BUCKET":          storage.DocumentsBucket,
		"S3_USE_SSL":         "false",
		"S3_REGION":          "us-east-1",
	}
}

// ChildEnv is base without model and tracing credentials and without any key in overrides, plus
// overrides.
func ChildEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, over := overrides[k]; over || k == "ANTHROPIC_API_KEY" || strings.HasPrefix(k, "LANGFUSE_") {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

// Proc is a child process in its own process group.
type Proc struct {
	cmd      *exec.Cmd
	done     chan struct{}
	logPath  string
	pgidPath string
}

// StartProcess starts name in dir as the leader of a new process group, so Kill reaches every process
// it spawns (uv run starts python as a child, not by exec).
func StartProcess(dir string, env []string, out io.Writer, name string, args ...string) (*Proc, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	p := &Proc{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// Kill sends SIGKILL to the whole process group (an unclean death: no handler runs, nothing is
// acked or flushed) and waits for the leader to be reaped. Killing an exited Proc is a no-op.
func (p *Proc) Kill() error {
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill process group %d: %w", p.cmd.Process.Pid, err)
	}
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("process %d not reaped 10s after SIGKILL", p.cmd.Process.Pid)
	}
	if p.pgidPath != "" {
		_ = os.Remove(p.pgidPath)
	}
	return nil
}

// Exited reports whether the leader has exited and been reaped.
func (p *Proc) Exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// State is the leader's exit state, nil while it runs.
func (p *Proc) State() *os.ProcessState {
	if !p.Exited() {
		return nil
	}
	return p.cmd.ProcessState
}

// KilledBySIGKILL reports whether the leader died of SIGKILL (not SIGTERM, not a clean exit).
func (p *Proc) KilledBySIGKILL() bool {
	st := p.State()
	if st == nil {
		return false
	}
	ws, ok := st.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL
}

// LogTail returns the last 8 KiB of the process log ("" without one).
func (p *Proc) LogTail() string {
	if p.logPath == "" {
		return ""
	}
	b, err := os.ReadFile(p.logPath)
	if err != nil {
		return ""
	}
	return "--- " + p.logPath + " (tail) ---\n" + string(b[max(0, len(b)-8<<10):])
}

// StartAIPy starts `uv run python -m ai` in cfg.AIPyDir with env on top of this process's env. The
// log goes to <LogDir>/<name>.log and the process group id to <LogDir>/<name>.pgid, so
// scripts/phase1-chaos.sh can still kill the worker if go test dies before its cleanup runs.
func StartAIPy(t testing.TB, cfg ChaosConfig, env map[string]string, name string) *Proc {
	t.Helper()
	logf, err := os.Create(filepath.Join(cfg.LogDir, name+".log"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := StartProcess(cfg.AIPyDir, ChildEnv(os.Environ(), env), logf,
		"uv", "run", "--frozen", "python", "-m", "ai")
	if err != nil {
		_ = logf.Close()
		t.Fatal(err)
	}
	p.logPath = logf.Name()
	p.pgidPath = filepath.Join(cfg.LogDir, name+".pgid")
	if err := os.WriteFile(p.pgidPath, []byte(strconv.Itoa(p.cmd.Process.Pid)+"\n"), 0o600); err != nil {
		t.Error(err)
	}
	t.Cleanup(func() {
		if err := p.Kill(); err != nil {
			t.Error(err)
		}
		_ = logf.Close()
	})
	return p
}

// WaitForConsumer waits until durable exists on stream (the worker has bound its consumer), failing
// early if proc exits first.
func WaitForConsumer(js jetstream.JetStream, stream, durable string, proc *Proc, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := js.Consumer(ctx, stream, durable)
		cancel()
		switch {
		case err == nil:
			return nil
		case proc.Exited():
			return fmt.Errorf("worker exited (%v) before consumer %s/%s existed", proc.State(), stream, durable)
		case time.Now().After(deadline):
			return fmt.Errorf("consumer %s/%s not created within %v: %w", stream, durable, timeout, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Fixture is one file to upload.
type Fixture struct {
	Path, Filename, ContentType, SHA256 string
	Size                                int64
}

// genScript renders n SyntheticInvoice PDFs with ai.synthetic.generator.generate and
// ai.synthetic.render_pdf.render_pdf, seeds seed_base..seed_base+n-1, alternating English and Arabic,
// without injected defects. It is a one-off fixture step: the harness never generates documents in Go.
const genScript = `import sys
from pathlib import Path
from ai.synthetic.generator import generate
from ai.synthetic.render_pdf import render_pdf
n, out, base = int(sys.argv[1]), Path(sys.argv[2]), int(sys.argv[3])
out.mkdir(parents=True, exist_ok=True)
for i in range(n):
    seed = base + i
    inv = generate(seed, "ar" if i % 2 else "en", defect_rate=0.0)
    (out / f"chaos-{seed:07d}.pdf").write_bytes(render_pdf(inv))
`

// GenerateBatch shells out once to uv in aiPyDir to render n PDFs into dir, then hashes them. It
// fails unless there are exactly n files with n distinct sha256 (a collision would be deduplicated
// by the upload path and the batch would not be n Documents).
func GenerateBatch(ctx context.Context, aiPyDir string, n, seedBase int, dir string) ([]Fixture, error) {
	cmd := exec.CommandContext(ctx, "uv", "run", "--frozen", "python", "-c", genScript,
		strconv.Itoa(n), dir, strconv.Itoa(seedBase))
	cmd.Dir = aiPyDir
	cmd.Env = ChildEnv(os.Environ(), nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("generate fixtures: %w\n%s", err, out)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fx := make([]Fixture, 0, n)
	seen := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pdf") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		if prev, dup := seen[h]; dup {
			return nil, fmt.Errorf("fixtures %s and %s have the same sha256", prev, e.Name())
		}
		seen[h] = e.Name()
		fx = append(fx, Fixture{Path: path, Filename: e.Name(), ContentType: "application/pdf", SHA256: h,
			Size: int64(len(b))})
	}
	if len(fx) != n {
		return nil, fmt.Errorf("generated %d fixtures, want %d", len(fx), n)
	}
	return fx, nil
}

// Client speaks the public /v1 JSON contract as the browser does (Task 15): it never calls a Go
// package of api-go directly.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

type fileIn struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

// Upload is a presigned PUT from POST /v1/documents/uploads.
type Upload struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
}

type uploadItem struct {
	DocumentID   *uuid.UUID `json:"document_id"`
	Status       string     `json:"status"`
	Deduplicated bool       `json:"deduplicated"`
	Upload       *Upload    `json:"upload"`
	Error        string     `json:"error"`
}

type completeItem struct {
	DocumentID string `json:"document_id"`
	Status     string `json:"status"`
	Error      string `json:"error"`
}

func (c *Client) do(ctx context.Context, method, path string, in, out any, want int) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("%s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode != want {
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, bytes.TrimSpace(raw[:min(len(raw), 512)]))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: decode: %w", method, path, err)
	}
	return nil
}

// CreateClientCompany creates an active ClientCompany (POST /v1/client-companies) and returns its id.
func (c *Client) CreateClientCompany(ctx context.Context, name string) (uuid.UUID, error) {
	var out struct {
		ID uuid.UUID `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/client-companies", map[string]string{"name": name}, &out, http.StatusCreated)
	return out.ID, err
}

// UploadBatch uploads every fixture to client company cc: POST /v1/documents/uploads in requests of
// MaxFilesPerRequest, the presigned PUT of each file with at most concurrency in flight, then POST
// /v1/documents/complete for the request's Documents. It fails on any per-item error, on a
// deduplicated fixture and on any Document that complete does not move to uploaded. It returns the
// Document ids in fixture order.
func (c *Client) UploadBatch(ctx context.Context, cc uuid.UUID, fx []Fixture, concurrency int) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(fx))
	for chunk := range slices.Chunk(fx, MaxFilesPerRequest) {
		got, err := c.uploadChunk(ctx, cc, chunk, concurrency)
		if err != nil {
			return nil, err
		}
		ids = append(ids, got...)
	}
	return ids, nil
}

func (c *Client) uploadChunk(ctx context.Context, cc uuid.UUID, fx []Fixture, concurrency int) ([]uuid.UUID, error) {
	files := make([]fileIn, len(fx))
	for i, f := range fx {
		files[i] = fileIn{Filename: f.Filename, ContentType: f.ContentType, SizeBytes: f.Size, SHA256: f.SHA256}
	}
	var res struct {
		Items []uploadItem `json:"items"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/documents/uploads",
		map[string]any{"client_company_id": cc, "files": files}, &res, http.StatusOK); err != nil {
		return nil, err
	}
	if len(res.Items) != len(fx) {
		return nil, fmt.Errorf("uploads: %d items for %d files", len(res.Items), len(fx))
	}
	ids := make([]uuid.UUID, len(fx))
	for i, it := range res.Items {
		switch {
		case it.Error != "":
			return nil, fmt.Errorf("uploads: %s: %s", fx[i].Filename, it.Error)
		case it.Deduplicated:
			return nil, fmt.Errorf("uploads: %s was deduplicated: fixtures must be distinct", fx[i].Filename)
		case it.DocumentID == nil || it.Upload == nil:
			return nil, fmt.Errorf("uploads: %s: no document id or no presigned upload", fx[i].Filename)
		}
		ids[i] = *it.DocumentID
	}

	errs := make([]error, len(fx))
	sem := make(chan struct{}, max(1, concurrency))
	var wg sync.WaitGroup
	for i := range fx {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			errs[i] = c.put(ctx, *res.Items[i].Upload, fx[i])
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	raw := make([]string, len(ids))
	for i, id := range ids {
		raw[i] = id.String()
	}
	var done struct {
		Items []completeItem `json:"items"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/documents/complete", map[string]any{"document_ids": raw}, &done,
		http.StatusOK); err != nil {
		return nil, err
	}
	if len(done.Items) != len(ids) {
		return nil, fmt.Errorf("complete: %d items for %d documents", len(done.Items), len(ids))
	}
	for i, it := range done.Items {
		if it.Status != "uploaded" || it.Error != "" || it.DocumentID != raw[i] {
			return nil, fmt.Errorf("complete: %s (%s): status %q error %q", it.DocumentID, fx[i].Filename, it.Status,
				it.Error)
		}
	}
	return ids, nil
}

// put performs one presigned upload exactly as signed: the method, the returned headers and the
// declared Content-Length.
func (c *Client) put(ctx context.Context, u Upload, f Fixture) error {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return err
	}
	if int64(len(b)) != f.Size {
		return fmt.Errorf("put %s: file is %d bytes, declared %d", f.Filename, len(b), f.Size)
	}
	req, err := http.NewRequestWithContext(ctx, u.Method, u.URL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	for k, v := range u.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("put %s: %w", f.Filename, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("put %s: %d %s", f.Filename, resp.StatusCode, bytes.TrimSpace(msg))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// orgVerifier stands in for Zitadel (not part of the chaos stack): the bearer token is the
// organisation id. Everything after authentication is the production router.
type orgVerifier struct{}

func (orgVerifier) Verify(_ context.Context, tok string) (auth.Principal, error) {
	if tok == "" {
		return auth.Principal{}, errors.New("empty token")
	}
	return auth.Principal{Subject: "chaos-harness", OrgID: tok}, nil
}

// APIConfig is the stack the in-process api-go runs on.
type APIConfig struct {
	DB                                   dbtest.Env
	JS                                   eventstest.JetStream
	Redis                                *redis.Client
	S3Endpoint, S3AccessKey, S3SecretKey string
}

// API is api-go running in-process: Task 30's trackb.Build and Run, behind httpapi.NewRouter on an
// httptest.Server.
type API struct {
	URL  string
	App  *trackb.App
	HTTP *http.Client
}

// Production rate limits (Global Constraints); 1,000 Documents are 10 upload and 10 complete
// requests, well inside them.
const (
	uploadsPerMinute = 20
	writesPerMinute  = 120
	readsPerMinute   = 600
)

// StartAPI builds and runs Track B and serves the full router until the test ends.
func StartAPI(t testing.TB, c APIConfig) *API {
	t.Helper()
	mc, err := storage.New(c.S3Endpoint, c.S3AccessKey, c.S3SecretKey, false)
	if err != nil {
		t.Fatal(err)
	}
	app, err := trackb.Build(context.Background(), trackb.Deps{
		Pool: c.DB.App, NatsConn: c.JS.Conn, JS: c.JS.JS, Redis: c.Redis, InternalMinio: mc,
		StoragePublicEndpoint: c.S3Endpoint, StorageAccessKey: c.S3AccessKey, StorageSecretKey: c.S3SecretKey,
		StorageRegion: "us-east-1", StorageBucket: storage.DocumentsBucket,
		UploadPerMinute: uploadsPerMinute, WritePerMinute: writesPerMinute, ReadPerMinute: readsPerMinute,
		RetryInterval: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	deadline := time.Now().Add(15 * time.Second)
	for app.Ready(ctx) != nil {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("Track B not ready: %v", app.Ready(ctx))
		}
		time.Sleep(20 * time.Millisecond)
	}
	h := httpapi.NewRouter(orgVerifier{}, httpapi.PGStore{Pool: c.DB.App}, events.NewPublisher(c.JS.JS),
		ratelimit.New(c.Redis, 60), app.Ready, httpapi.WithTrackB(app))
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		app.Shutdown()
		srv.Close()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("trackb Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("trackb Run did not return after cancel")
		}
	})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = UploadConcurrency + 4
	return &API{URL: srv.URL, App: app, HTTP: &http.Client{Transport: transport, Timeout: 2 * time.Minute}}
}

// Client returns a driver authenticated as a member of organisation org.
func (a *API) Client(org string) *Client { return &Client{Base: a.URL, Token: org, HTTP: a.HTTP} }
