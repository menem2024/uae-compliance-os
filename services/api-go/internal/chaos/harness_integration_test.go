//go:build integration && unix

package chaos

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
)

// fakeS3 is the smallest S3 that minio-go's bucket-location, stat, get and delete calls and a
// presigned PUT accept. CI's integration job has Postgres but no MinIO, and this test is about the
// harness's HTTP driver and Task 30's composition root, not about MinIO (storage's own integration
// test covers MinIO).
type fakeS3 struct {
	mu         sync.Mutex
	objects    map[string]s3Object
	signedPuts int
	srv        *httptest.Server
}

type s3Object struct {
	body        []byte
	contentType string
	at          time.Time
}

func newFakeS3(t *testing.T) *fakeS3 {
	t.Helper()
	s := &fakeS3{objects: map[string]s3Object{}}
	s.srv = httptest.NewServer(s)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fakeS3) endpoint() string { u, _ := url.Parse(s.srv.URL); return u.Host }

func (s *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if key == "" {
		if _, ok := r.URL.Query()["location"]; ok {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+
				`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		// Only a presigned PUT (the browser's path) is accepted.
		if r.URL.Query().Get("X-Amz-Signature") == "" {
			http.Error(w, "unsigned", http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || int64(len(body)) != r.ContentLength {
			http.Error(w, "short body", http.StatusBadRequest)
			return
		}
		s.objects[key] = s3Object{body: body, contentType: r.Header.Get("Content-Type"), at: time.Now().UTC()}
		s.signedPuts++
		w.Header().Set("ETag", `"`+uuid.NewString()+`"`)
	case http.MethodGet, http.MethodHead:
		obj, ok := s.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code>`+
					`<Message>The specified key does not exist.</Message></Error>`)
			}
			return
		}
		w.Header().Set("Content-Type", obj.contentType)
		w.Header().Set("ETag", `"etag"`)
		http.ServeContent(w, r, key, obj.at, bytes.NewReader(obj.body))
	case http.MethodDelete:
		delete(s.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

// The harness's upload driver against Task 30's real composition root (trackb.Build +
// httpapi.NewRouter behind an httptest.Server, real Postgres, embedded JetStream): every fixture
// becomes one uploaded Document through the real JSON contract and one document.uploaded message,
// and a re-upload of a fixture is refused as deduplicated (the 1,000 must be distinct).
func TestUploadDriverThroughTrackBCompositionRoot(t *testing.T) {
	env := dbtest.Setup(t)
	js := eventstest.StartJetStream(t)
	s3 := newFakeS3(t)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	api := StartAPI(t, APIConfig{DB: env, JS: js, Redis: rdb, S3Endpoint: s3.endpoint(), S3AccessKey: "minio",
		S3SecretKey: "minio_dev_pw"})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := api.Client(env.OrgA)
	cc, err := c.CreateClientCompany(ctx, "Chaos Trading LLC")
	if err != nil {
		t.Fatal(err)
	}
	fx := pdfFixtures(t, 150)
	ids, err := c.UploadBatch(ctx, cc, fx, UploadConcurrency)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if len(seen) != len(fx) {
		t.Fatalf("%d distinct Document ids for %d fixtures", len(seen), len(fx))
	}

	got, err := ReadCounts(ctx, env, env.FirmA)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Counts{Documents: 150}); got != want {
		t.Fatalf("counts after upload = %+v, want %+v", got, want)
	}
	var uploaded int64
	if err := dbtest.QueryRowFirm(ctx, env, env.FirmA,
		`SELECT count(*) FROM documents WHERE firm_id = $1 AND status = 'uploaded' AND published_at IS NOT NULL`,
		env.FirmA).Scan(&uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded != 150 {
		t.Fatalf("%d Documents uploaded and published, want 150", uploaded)
	}
	if n := eventstest.StreamMsgs(t, js.JS, events.DocumentsStream); n != 150 {
		t.Fatalf("DOCUMENTS holds %d messages, want 150 document.uploaded", n)
	}
	if s3.signedPuts != 150 {
		t.Fatalf("%d presigned PUTs, want 150", s3.signedPuts)
	}
	if other, err := ReadCounts(ctx, env, env.FirmB); err != nil || other != (Counts{}) {
		t.Fatalf("Firm B counts = %+v (%v), want zero", other, err)
	}

	if _, err := c.UploadBatch(ctx, cc, fx[:1], 1); err == nil || !strings.Contains(err.Error(), "deduplicated") {
		t.Fatalf("re-upload err = %v, want deduplicated", err)
	}
}

// ReadCounts against the real schema: the terminal set, running/abandoned runs, and the invoices
// count that is separate from COUNT(DISTINCT document_id), all scoped to one Firm.
func TestReadCountsAgainstTheSchema(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	seedDoc := func(firm, cc uuid.UUID, status string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := dbtest.ExecFirm(ctx, env, firm, `INSERT INTO documents (id, firm_id, client_company_id, sha256,
			object_key, filename, content_type, size_bytes, status) VALUES ($1::uuid, $2::uuid, $3,
			encode(sha256($1::text::bytea), 'hex'), 'firms/' || $2::text || '/docs/' || $1::text, 'a.pdf',
			'application/pdf', 10, $4)`, id, firm, cc, status); err != nil {
			t.Fatal(err)
		}
		return id
	}
	seedRun := func(firm uuid.UUID, status string) {
		t.Helper()
		if _, err := dbtest.ExecFirm(ctx, env, firm, `INSERT INTO agent_runs (id, firm_id, subject_type, subject_id,
			status) VALUES ($1, $2, 'document', 'x', $3)`, uuid.New(), firm, status); err != nil {
			t.Fatal(err)
		}
	}
	seedInvoice := func(firm, cc, doc uuid.UUID, ordinal int) {
		t.Helper()
		if _, err := dbtest.ExecFirm(ctx, env, firm, `INSERT INTO invoices (firm_id, status, payload, client_company_id,
			document_id, source_ordinal) VALUES ($1, 'extracted', '{}', $2, $3, $4)`, firm, cc, doc, ordinal); err != nil {
			t.Fatal(err)
		}
	}

	ccA := dbtest.ClientCompany(t, env, env.FirmA, "A", "")
	d1, d2 := seedDoc(env.FirmA, ccA, "extracted"), seedDoc(env.FirmA, ccA, "extracted")
	seedDoc(env.FirmA, ccA, "processing")
	seedDoc(env.FirmA, ccA, "needs_review")
	seedDoc(env.FirmA, ccA, "uploaded")
	seedRun(env.FirmA, "running")
	seedRun(env.FirmA, "abandoned")
	seedRun(env.FirmA, "succeeded")
	seedInvoice(env.FirmA, ccA, d1, 0)
	seedInvoice(env.FirmA, ccA, d1, 1)
	seedInvoice(env.FirmA, ccA, d2, 0)
	ccB := dbtest.ClientCompany(t, env, env.FirmB, "B", "")
	seedInvoice(env.FirmB, ccB, seedDoc(env.FirmB, ccB, "extracted"), 0)
	seedRun(env.FirmB, "running")

	got, err := ReadCounts(ctx, env, env.FirmA)
	if err != nil {
		t.Fatal(err)
	}
	want := Counts{Documents: 5, Terminal: 3, Extracted: 2, RunningRuns: 1, AbandonedRuns: 1, Invoices: 3,
		DistinctInvoiceDocs: 2}
	if got != want {
		t.Fatalf("ReadCounts = %+v, want %+v", got, want)
	}
}
