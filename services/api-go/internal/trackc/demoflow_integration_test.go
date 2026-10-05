//go:build integration

// TestDemoFlow proves the whole demo backend flow without Docker and without mocking the validation:
// the four canonical demo invoices (demo/samples/*.json) go through the production router
// (httpapi.NewRouter with WithTrackB and WithTrackC, assembled as cmd/api/main.go does) against a real
// Postgres 17 and the REAL validator-rs gRPC server (a child process of the test). Only these parts
// are stand-ins: the bearer token is the organisation id (no Zitadel), S3 is an in-test fake, NATS is
// the embedded nats-server, the rate limiter runs on miniredis, and ai-py's extraction step is a tiny
// consumer that turns invoice.submitted into invoice.extracted (these invoices are already canonical
// JSON, so extraction is the identity, exactly like ai-py's structured path).
//
// Run it (no Docker):
//
//	# 1. validator binary (scratch target dir outside /tmp)
//	cd services/validator-rs && CARGO_TARGET_DIR=$HOME/.cache/uae-validator-target cargo build --bin validator-rs
//	# 2. a Postgres 17 on 127.0.0.1:45490 with deploy/compose/postgres/init.sql applied (roles
//	#    compliance_owner / compliance_app and the compliance database), e.g. the zonky binaries
//	#    (io.zonky.test.postgres:embedded-postgres-binaries-linux-amd64, 17.x) started with pg_ctl
//	# 3. the test (validator-rs listens on 0.0.0.0:50051, hard-coded in its main.rs: the port must be free)
//	cd services/api-go && \
//	  VALIDATOR_BIN=$HOME/.cache/uae-validator-target/debug/validator-rs \
//	  TEST_OWNER_URL=postgres://compliance_owner:owner_dev_pw@127.0.0.1:45490/compliance \
//	  TEST_APP_URL=postgres://compliance_app:app_dev_pw@127.0.0.1:45490/compliance \
//	  go test -tags integration -run TestDemoFlow -count=1 -v ./internal/trackc/
//
// The test skips when VALIDATOR_BIN (or TEST_OWNER_URL / TEST_APP_URL) is unset.
package trackc_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/chaos"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
	"github.com/menem2024/uae-platform/services/api-go/internal/exportclient"
	"github.com/menem2024/uae-platform/services/api-go/internal/exports"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpapi"
	"github.com/menem2024/uae-platform/services/api-go/internal/ratelimit"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackb"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackc"
	"github.com/menem2024/uae-platform/services/api-go/internal/validator"
)

const validatorPort = 50051 // hard-coded in services/validator-rs/src/main.rs

// ---- stand-ins -------------------------------------------------------------------------------------

// orgVerifier stands in for Zitadel: the bearer token is the organisation id.
type orgVerifier struct{}

func (orgVerifier) Verify(_ context.Context, tok string) (auth.Principal, error) {
	if tok == "" {
		return auth.Principal{}, errors.New("empty token")
	}
	return auth.Principal{Subject: "demo-user", OrgID: tok}, nil
}

// fakeS3 is the least S3 that minio-go needs for PutObject, GetObject and StatObject of small objects.
type fakeS3 struct {
	mu   sync.Mutex
	objs map[string]fakeObj
}

type fakeObj struct {
	data []byte
	ct   string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("location") {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.Contains(key, "/") { // a bucket: it always exists
		w.WriteHeader(http.StatusOK)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") { // minio-go signs plain-HTTP uploads in chunks
			var err error
			if b, err = decodeAWSChunked(b); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		f.objs[key] = fakeObj{data: b, ct: r.Header.Get("Content-Type")}
		sum := sha256.Sum256(b)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet, http.MethodHead:
		o, ok := f.objs[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`)
			return
		}
		sum := sha256.Sum256(o.data)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)
		w.Header().Set("Content-Type", o.ct)
		http.ServeContent(w, r, "", time.Unix(1_700_000_000, 0), bytes.NewReader(o.data))
	default:
		http.Error(w, "unsupported", http.StatusMethodNotAllowed)
	}
}

// decodeAWSChunked unwraps an aws-chunked body: "<hex size>;chunk-signature=<sig>\r\n<data>\r\n" repeated,
// ended by a zero-size chunk. Signatures are not verified (the fake is not an authentication test).
func decodeAWSChunked(b []byte) ([]byte, error) {
	var out []byte
	for {
		head, rest, ok := bytes.Cut(b, []byte("\r\n"))
		if !ok {
			return nil, errors.New("aws-chunked: truncated chunk header")
		}
		sizeHex, _, _ := strings.Cut(string(head), ";")
		var n int
		if _, err := fmt.Sscanf(sizeHex, "%x", &n); err != nil {
			return nil, fmt.Errorf("aws-chunked: chunk size %q: %w", sizeHex, err)
		}
		if n == 0 {
			return out, nil
		}
		if len(rest) < n+2 {
			return nil, errors.New("aws-chunked: truncated chunk")
		}
		out = append(out, rest[:n]...)
		b = rest[n+2:]
	}
}

// startValidator starts the real validator-rs (own process group, killed at cleanup) and waits until
// its gRPC port accepts connections.
func startValidator(t *testing.T, bin string) string {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", validatorPort)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port %d is busy (validator-rs listens on a hard-coded 0.0.0.0:%d): %v", validatorPort, validatorPort, err)
	}
	_ = l.Close()
	logf, err := os.Create(filepath.Join(t.TempDir(), "validator-rs.log"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := chaos.StartProcess("", chaos.ChildEnv(os.Environ(), map[string]string{"RUST_LOG": "warn"}), logf, bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Kill(); err != nil {
			t.Error(err)
		}
		_ = logf.Close()
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return "http://" + addr
		}
		if p.Exited() {
			b, _ := os.ReadFile(logf.Name())
			t.Fatalf("validator-rs exited early (%v): %s", p.State(), b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("validator-rs not listening on %s within 30s", addr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startExtractionStandIn does what ai-py does for an already canonical invoice: it consumes
// invoice.submitted and publishes invoice.extracted with the same Invoice.
func startExtractionStandIn(t *testing.T, js jetstream.JetStream) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cons, err := js.CreateOrUpdateConsumer(ctx, events.InvoicesStream, jetstream.ConsumerConfig{
		Durable: "ai-extraction", FilterSubject: events.SubmittedSubject, AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := cons.Consume(func(m jetstream.Msg) {
		var ev compliancev1.InvoiceSubmitted
		if err := proto.Unmarshal(m.Data(), &ev); err != nil {
			t.Errorf("stand-in: decode invoice.submitted: %v", err)
			_ = m.Term()
			return
		}
		out, err := proto.Marshal(&compliancev1.InvoiceExtracted{InvoiceId: ev.GetInvoiceId(), FirmId: ev.GetFirmId(),
			Invoice: ev.GetInvoice(), Confidence: 1})
		if err != nil {
			t.Errorf("stand-in: encode: %v", err)
			return
		}
		pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pcancel()
		if _, err := js.Publish(pctx, events.ExtractedSubject, out); err != nil {
			_ = m.Nak()
			return
		}
		_ = m.Ack()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cc.Stop)
}

// ---- the stack -------------------------------------------------------------------------------------

type stack struct {
	env  dbtest.Env
	url  string
	http *http.Client
}

func startStack(t *testing.T, validatorAddr string) *stack {
	t.Helper()
	env := dbtest.Setup(t)
	js := eventstest.StartJetStream(t)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	s3 := httptest.NewServer(&fakeS3{objs: map[string]fakeObj{}})
	t.Cleanup(s3.Close)
	s3Host := strings.TrimPrefix(s3.URL, "http://")
	const s3Key, s3Secret = "demo", "demo-secret"
	mc, err := storage.New(s3Host, s3Key, s3Secret, false)
	if err != nil {
		t.Fatal(err)
	}

	vc, err := validator.New(validatorAddr)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := exportclient.New(validatorAddr)
	if err != nil {
		t.Fatal(err)
	}
	tcCfg, err := trackc.LoadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	const perMinute = 100_000
	store := httpapi.PGStore{Pool: env.App}
	tc, err := trackc.New(trackc.Deps{
		Pool: env.App, Validator: vc, Exporter: ec,
		Store:        &exports.MinioStore{Client: mc, Bucket: tcCfg.ExportsBucket},
		Firms:        store,
		ReadLimiter:  trackc.NamedLimiter("c-read", ratelimit.New(rdb, perMinute)),
		WriteLimiter: trackc.NamedLimiter("c-write", ratelimit.New(rdb, perMinute)),
		Config:       tcCfg,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	consumer := events.NewValidationConsumer(js.JS, httpapi.HandleExtracted(tc.Validation), 200*time.Millisecond)
	wg.Go(func() { consumer.Run(ctx) })

	app, err := trackb.Build(ctx, trackb.Deps{
		Pool: env.App, NatsConn: js.Conn, JS: js.JS, Redis: rdb, InternalMinio: mc,
		StoragePublicEndpoint: s3Host, StorageAccessKey: s3Key, StorageSecretKey: s3Secret,
		StorageRegion: "us-east-1", StorageBucket: storage.DocumentsBucket,
		UploadPerMinute: perMinute, WritePerMinute: perMinute, ReadPerMinute: perMinute,
		RetryInterval: 200 * time.Millisecond,
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	appDone := make(chan error, 1)
	go func() { appDone <- app.Run(ctx) }()
	deadline := time.Now().Add(15 * time.Second)
	for app.Ready(ctx) != nil || consumer.Ready(ctx) != nil {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("not ready: track b: %v, validation consumer: %v", app.Ready(ctx), consumer.Ready(ctx))
		}
		time.Sleep(20 * time.Millisecond)
	}
	startExtractionStandIn(t, js.JS)

	h := httpapi.NewRouter(orgVerifier{}, store, events.NewPublisher(js.JS), ratelimit.New(rdb, perMinute),
		app.Ready, httpapi.WithTrackB(app), httpapi.WithTrackC(tc))
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		app.Shutdown()
		srv.Close()
		cancel()
		wg.Wait()
		select {
		case err := <-appDone:
			if err != nil {
				t.Errorf("trackb Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("trackb Run did not return after cancel")
		}
	})
	return &stack{env: env, url: srv.URL, http: &http.Client{Timeout: 30 * time.Second}}
}

// ---- the HTTP driver -------------------------------------------------------------------------------

type issue struct {
	RuleID         string `json:"rule_id"`
	Severity       string `json:"severity"`
	Path           string `json:"path"`
	SuggestedValue string `json:"suggested_value"`
	Fixable        bool   `json:"fixable"`
}

type detail struct {
	ID             uuid.UUID       `json:"id"`
	Status         string          `json:"status"`
	PayloadVersion int32           `json:"payload_version"`
	Payload        json.RawMessage `json:"payload"`
	LatestRun      *struct {
		ID           uuid.UUID `json:"id"`
		ErrorCount   int       `json:"error_count"`
		WarningCount int       `json:"warning_count"`
		Issues       []issue   `json:"issues"`
	} `json:"latest_run"`
}

func (d detail) errs() []issue {
	var out []issue
	if d.LatestRun != nil {
		for _, i := range d.LatestRun.Issues {
			if i.Severity == "error" {
				out = append(out, i)
			}
		}
	}
	return out
}

func (d detail) find(rule string) (issue, bool) {
	if d.LatestRun != nil {
		for _, i := range d.LatestRun.Issues {
			if i.RuleID == rule {
				return i, true
			}
		}
	}
	return issue{}, false
}

// table renders the latest run's issues compactly: rule@path[severity] and the suggested value.
func (d detail) table() string {
	if d.LatestRun == nil {
		return d.Status + " (no run)"
	}
	var parts []string
	for _, i := range d.LatestRun.Issues {
		p := fmt.Sprintf("%s@%s[%s]", i.RuleID, i.Path, i.Severity[:1])
		if i.SuggestedValue != "" {
			p += "=>" + i.SuggestedValue
		}
		parts = append(parts, p)
	}
	return fmt.Sprintf("%s v%d errors=%d warnings=%d: %s", d.Status, d.PayloadVersion, d.LatestRun.ErrorCount,
		d.LatestRun.WarningCount, strings.Join(parts, " "))
}

func (s *stack) do(t *testing.T, org, method, path string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, s.url+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+org)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read: %v", method, path, err)
	}
	return resp.StatusCode, resp.Header, raw
}

// call performs a request and requires status want; it decodes a JSON response into out (when non-nil).
func (s *stack) call(t *testing.T, org, method, path string, in, out any, want int) []byte {
	t.Helper()
	var body []byte
	switch v := in.(type) {
	case nil:
	case []byte:
		body = v
	default:
		var err error
		if body, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	status, _, raw := s.do(t, org, method, path, body)
	if status != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, status, want, bytes.TrimSpace(raw))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return raw
}

func (s *stack) detail(t *testing.T, org string, id uuid.UUID) detail {
	t.Helper()
	var d detail
	s.call(t, org, http.MethodGet, "/v1/invoices/"+id.String()+"/validation", nil, &d, http.StatusOK)
	return d
}

// waitStatus polls GET .../validation until the invoice has one of the wanted statuses.
func (s *stack) waitStatus(t *testing.T, org string, id uuid.UUID, want ...string) detail {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		d := s.detail(t, org, id)
		if slices.Contains(want, d.Status) && d.LatestRun != nil {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("invoice %s: status %q (%s) after 30s, want one of %v", id, d.Status, d.table(), want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *stack) submit(t *testing.T, org, sample string) uuid.UUID {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "demo", "samples", sample))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	s.call(t, org, http.MethodPost, "/v1/invoices", raw, &out, http.StatusAccepted)
	if out.ID == uuid.Nil {
		t.Fatalf("%s: no invoice id", sample)
	}
	return out.ID
}

// correct applies path => value corrections to the invoice's current payload version (old values read
// from the stored payload with the same field-path grammar the server uses) and returns the new
// payload version and the revalidation state the API reports.
func (s *stack) correct(t *testing.T, org string, id uuid.UUID, fixes [][2]string) (int32, string) {
	t.Helper()
	d := s.detail(t, org, id)
	inv := &compliancev1.Invoice{}
	if err := protojson.Unmarshal(d.Payload, inv); err != nil {
		t.Fatalf("stored payload: %v", err)
	}
	changes := make([]fieldpath.FieldChange, 0, len(fixes))
	for _, f := range fixes {
		old, err := fieldpath.Get(inv, f[0])
		if err != nil {
			t.Fatalf("path %q: %v", f[0], err)
		}
		changes = append(changes, fieldpath.FieldChange{Path: f[0], OldValue: old, NewValue: f[1]})
	}
	var out struct {
		PayloadVersion int32  `json:"payload_version"`
		Revalidation   string `json:"revalidation"`
	}
	s.call(t, org, http.MethodPost, "/v1/invoices/"+id.String()+"/validation/corrections",
		map[string]any{"payload_version": d.PayloadVersion, "changes": changes, "reason": "demo flow integration test"},
		&out, http.StatusOK)
	if out.PayloadVersion != d.PayloadVersion+1 {
		t.Fatalf("payload_version %d after correction, want %d", out.PayloadVersion, d.PayloadVersion+1)
	}
	return out.PayloadVersion, out.Revalidation
}

type exportView struct {
	ID        uuid.UUID `json:"id"`
	InvoiceID uuid.UUID `json:"invoice_id"`
	SHA256    string    `json:"sha256"`
	SizeBytes int       `json:"size_bytes"`
	Format    string    `json:"format"`
}

// approveAndExport approves the current payload version, creates the export and verifies the XML.
func (s *stack) approveAndExport(t *testing.T, org string, id uuid.UUID) exportView {
	t.Helper()
	d := s.detail(t, org, id)
	if d.Status != "validated" {
		t.Fatalf("status %q before approval, want validated", d.Status)
	}
	var ap struct {
		Status         string `json:"status"`
		PayloadVersion int32  `json:"payload_version"`
	}
	s.call(t, org, http.MethodPost, "/v1/invoices/"+id.String()+"/validation/approve",
		map[string]any{"payload_version": d.PayloadVersion}, &ap, http.StatusOK)
	if ap.Status != "ready" || ap.PayloadVersion != d.PayloadVersion {
		t.Fatalf("approve = %+v, want ready at v%d", ap, d.PayloadVersion)
	}
	if got := s.detail(t, org, id).Status; got != "ready" {
		t.Fatalf("status after approval = %q, want ready", got)
	}

	var ex exportView
	s.call(t, org, http.MethodPost, "/v1/exports", map[string]any{"invoice_id": id}, &ex, http.StatusCreated)
	if ex.ID == uuid.Nil || ex.InvoiceID != id || ex.SHA256 == "" {
		t.Fatalf("export = %+v", ex)
	}
	var got exportView
	s.call(t, org, http.MethodGet, "/v1/exports/"+ex.ID.String(), nil, &got, http.StatusOK)
	if got.SHA256 != ex.SHA256 {
		t.Fatalf("GET export sha256 %s != created %s", got.SHA256, ex.SHA256)
	}

	status, hdr, xmlBytes := s.do(t, org, http.MethodGet, "/v1/exports/"+ex.ID.String()+"/xml", nil)
	if status != http.StatusOK {
		t.Fatalf("GET export xml: %d %s", status, xmlBytes)
	}
	if ct := hdr.Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("export Content-Type = %q", ct)
	}
	sum := sha256.Sum256(xmlBytes)
	if hex.EncodeToString(sum[:]) != ex.SHA256 {
		t.Errorf("sha256(xml) = %x, export row says %s", sum, ex.SHA256)
	}
	if ex.SizeBytes != len(xmlBytes) {
		t.Errorf("export size_bytes = %d, xml is %d bytes", ex.SizeBytes, len(xmlBytes))
	}
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	elements, root := 0, ""
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("export XML is not well-formed: %v", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			if root == "" {
				root = se.Name.Local
			}
			elements++
		}
	}
	if root != "Invoice" || elements < 20 {
		t.Errorf("export XML root %q with %d elements, want an Invoice document", root, elements)
	}
	t.Logf("export %s: %d bytes, root <%s>, %d elements, sha256 %s", ex.ID, len(xmlBytes), root, elements, ex.SHA256[:16])
	return ex
}

// auditActions returns the invoice's audit trail as action names, oldest first. Validation runs are not
// audit events (they are validation_runs rows); the trail holds the human and system decisions.
func (s *stack) auditActions(t *testing.T, org string, id uuid.UUID) []string {
	t.Helper()
	var out struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	s.call(t, org, http.MethodGet, "/v1/invoices/"+id.String()+"/validation/audit", nil, &out, http.StatusOK)
	// The endpoint lists the newest event first; return them oldest first.
	actions := make([]string, len(out.Items))
	for i, it := range out.Items {
		actions[len(out.Items)-1-i] = it.Action
	}
	return actions
}

// ---- the test --------------------------------------------------------------------------------------

func ruleIDs(d detail) []string {
	var ids []string
	if d.LatestRun != nil {
		for _, i := range d.LatestRun.Issues {
			ids = append(ids, i.RuleID)
		}
	}
	return ids
}

func requireRules(t *testing.T, d detail, rules ...string) {
	t.Helper()
	for _, r := range rules {
		if _, ok := d.find(r); !ok {
			t.Errorf("run has no %s issue; got %v", r, ruleIDs(d))
		}
	}
}

func TestDemoFlow(t *testing.T) {
	bin := os.Getenv("VALIDATOR_BIN")
	if bin == "" {
		t.Skip("VALIDATOR_BIN is not set: build services/validator-rs (cargo build --bin validator-rs) and point VALIDATOR_BIN at the binary; see the file header")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("VALIDATOR_BIN: %v", err)
	}
	s := startStack(t, startValidator(t, bin))
	orgA, orgB := s.env.OrgA, s.env.OrgB
	type row struct{ sample, before, after string }
	var table []row
	t.Cleanup(func() {
		t.Log("engine results per sample (rule@path[e|w]=>suggested):")
		for _, r := range table {
			t.Logf("  %-20s initial: %s", r.sample, r.before)
			t.Logf("  %-20s final:   %s", "", r.after)
		}
	})

	t.Run("01-valid", func(t *testing.T) {
		id := s.submit(t, orgA, "01-valid.json")
		d := s.waitStatus(t, orgA, id, "validated", "has_issues")
		if d.Status != "validated" || d.LatestRun.ErrorCount != 0 || len(d.errs()) != 0 {
			t.Fatalf("01-valid: %s", d.table())
		}
		before := d.table()
		ex := s.approveAndExport(t, orgA, id)
		table = append(table, row{"01-valid", before, s.detail(t, orgA, id).table() + " | export " + ex.SHA256[:12]})

		got := s.auditActions(t, orgA, id)
		t.Logf("01 audit actions (oldest first): %v", got)
		if want := []string{"invoice.approved", "invoice.exported"}; !slices.Equal(got, want) {
			t.Errorf("01 audit actions = %v, want %v", got, want)
		}

		// Cross-tenant: the second Firm sees nothing of the first Firm's invoice or export.
		inv := id.String()
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/v1/invoices/" + inv, ""},
			{http.MethodGet, "/v1/invoices/" + inv + "/validation", ""},
			{http.MethodGet, "/v1/invoices/" + inv + "/validation/audit", ""},
			{http.MethodPost, "/v1/invoices/" + inv + "/validation", ""},
			{http.MethodPost, "/v1/invoices/" + inv + "/validation/approve", `{"payload_version":1}`},
			{http.MethodPost, "/v1/invoices/" + inv + "/validation/corrections",
				`{"payload_version":1,"changes":[{"path":"note","old_value":"Tax invoice","new_value":"x"}]}`},
			{http.MethodPost, "/v1/exports", `{"invoice_id":"` + inv + `"}`},
			{http.MethodGet, "/v1/exports/" + ex.ID.String(), ""},
			{http.MethodGet, "/v1/exports/" + ex.ID.String() + "/xml", ""},
		} {
			var body []byte
			if c.body != "" {
				body = []byte(c.body)
			}
			if status, _, raw := s.do(t, orgB, c.method, c.path, body); status != http.StatusNotFound {
				t.Errorf("cross-tenant %s %s = %d %s, want 404", c.method, c.path, status, bytes.TrimSpace(raw))
			}
		}
		// ...and it is untouched for the owner.
		if got := s.detail(t, orgA, id); got.Status != "ready" {
			t.Errorf("after cross-tenant attempts the invoice is %q, want ready", got.Status)
		}
	})

	t.Run("02-totals-mismatch", func(t *testing.T) {
		id := s.submit(t, orgA, "02-totals-mismatch.json")
		d := s.waitStatus(t, orgA, id, "validated", "has_issues")
		if d.Status != "has_issues" {
			t.Fatalf("02: %s", d.table())
		}
		before := d.table()
		is, ok := d.find("ibr-co-16")
		if !ok || is.Severity != "error" || is.SuggestedValue == "" {
			t.Fatalf("02: want an ibr-co-16 error with a suggested_value: %s", d.table())
		}
		if is.Path == "" {
			t.Fatalf("02: ibr-co-16 has no path: %s", d.table())
		}
		pv, reval := s.correct(t, orgA, id, [][2]string{{is.Path, is.SuggestedValue}})
		t.Logf("02: corrected %s => %s (payload v%d, revalidation %s)", is.Path, is.SuggestedValue, pv, reval)
		if reval != "done" {
			t.Fatalf("02: revalidation %q after the correction, want done", reval)
		}
		// Re-validate explicitly too (POST .../validation).
		var run struct {
			Status string `json:"status"`
			Errors int    `json:"errors"`
		}
		s.call(t, orgA, http.MethodPost, "/v1/invoices/"+id.String()+"/validation", nil, &run, http.StatusOK)
		d = s.waitStatus(t, orgA, id, "validated", "has_issues")
		if d.Status != "validated" || run.Status != "validated" || len(d.errs()) != 0 {
			t.Fatalf("02 after correction: run=%+v %s", run, d.table())
		}
		ex := s.approveAndExport(t, orgA, id)
		table = append(table, row{"02-totals-mismatch", before, d.table() + " | export " + ex.SHA256[:12]})
		got := s.auditActions(t, orgA, id)
		t.Logf("02 audit actions (oldest first): %v", got)
		want := []string{"invoice.fields_changed", "invoice.validation_requested", "invoice.approved", "invoice.exported"}
		if !slices.Equal(got, want) {
			t.Errorf("02 audit actions = %v, want %v", got, want)
		}
	})

	t.Run("03-bad-trn", func(t *testing.T) {
		id := s.submit(t, orgA, "03-bad-trn.json")
		d := s.waitStatus(t, orgA, id, "validated", "has_issues")
		if d.Status != "has_issues" {
			t.Fatalf("03: %s", d.table())
		}
		before := d.table()
		is, ok := d.find("ibr-132-ae")
		if !ok || is.Severity != "error" || is.Path != "seller_trn" {
			t.Fatalf("03: want an ibr-132-ae error at seller_trn: %s", d.table())
		}
		s.correct(t, orgA, id, [][2]string{{"seller_trn", "198765432102003"}})
		d = s.waitStatus(t, orgA, id, "validated", "has_issues")
		if d.Status != "validated" {
			t.Fatalf("03 after correction: %s", d.table())
		}
		table = append(table, row{"03-bad-trn", before, d.table()})
		if got := s.auditActions(t, orgA, id); !slices.Equal(got, []string{"invoice.fields_changed"}) {
			t.Errorf("03 audit actions = %v, want [invoice.fields_changed]", got)
		}
	})

	t.Run("04-missing-fields", func(t *testing.T) {
		id := s.submit(t, orgA, "04-missing-fields.json")
		d := s.waitStatus(t, orgA, id, "validated", "has_issues")
		if d.Status != "has_issues" {
			t.Fatalf("04: %s", d.table())
		}
		before := d.table()
		requireRules(t, d, "ibr-003", "ibr-007", "ibr-010")
		for _, c := range []struct{ rule, path string }{{"ibr-003", "issue_date"}, {"ibr-007", "buyer.name"}} {
			if is, ok := d.find(c.rule); ok && is.Path != c.path {
				t.Errorf("04: %s at %q, want %q", c.rule, is.Path, c.path)
			}
		}
		// Values from the official example (rulesets/pint-ae-1.0.4/corpus/examples/standard-tax-invoice.json).
		s.correct(t, orgA, id, [][2]string{
			{"issue_date", "2025-02-06"},
			{"buyer.name", "Noor Electronics LLC"},
			{"buyer.postal_address.line1", "Street Name"},
			{"buyer.postal_address.city", "Abu Dhabi"},
			{"buyer.postal_address.country_subdivision", "AUH"},
			{"buyer.postal_address.country_code", "AE"},
		})
		d = s.waitStatus(t, orgA, id, "validated", "has_issues")
		if d.Status != "validated" {
			t.Fatalf("04 after corrections: %s", d.table())
		}
		if _, still := d.find("ibr-141-ae"); still {
			t.Errorf("04: ibr-141-ae still reported once issue_date is set: %s", d.table())
		}
		table = append(table, row{"04-missing-fields", before, d.table()})
		if got := s.auditActions(t, orgA, id); !slices.Equal(got, []string{"invoice.fields_changed"}) {
			t.Errorf("04 audit actions = %v, want [invoice.fields_changed]", got)
		}
	})
}
