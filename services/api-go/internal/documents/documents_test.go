package documents_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/documents"
	"github.com/menem2024/uae-platform/services/api-go/internal/httpx"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
)

var pdf = []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")

func hexSHA(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestValidateAndClean(t *testing.T) {
	good := documents.FileIn{Filename: " inv\x00oice\t.pdf ", ContentType: "application/pdf", SizeBytes: 10, SHA256: hexSHA(pdf)}
	got, code := good.Validate()
	if code != "" || got.Filename != "invoice.pdf" {
		t.Fatalf("%q %q", got.Filename, code)
	}
	for _, c := range []struct {
		mut  func(*documents.FileIn)
		code string
	}{
		{func(f *documents.FileIn) { f.Filename = "\x01\x02" }, documents.CodeInvalidFilename},
		{func(f *documents.FileIn) { f.Filename = strings.Repeat("ب", 256) }, documents.CodeInvalidFilename},
		{func(f *documents.FileIn) { f.ContentType = "application/zip" }, documents.CodeInvalidContentType},
		{func(f *documents.FileIn) { f.SizeBytes = 0 }, documents.CodeInvalidSize},
		{func(f *documents.FileIn) { f.SizeBytes = documents.MaxSizeBytes + 1 }, documents.CodeInvalidSize},
		{func(f *documents.FileIn) { f.SHA256 = strings.ToUpper(f.SHA256) }, documents.CodeInvalidSHA256},
	} {
		f := good
		c.mut(&f)
		if _, code := f.Validate(); code != c.code {
			t.Errorf("want %s got %q", c.code, code)
		}
	}
}

func TestSniffAndKey(t *testing.T) {
	cases := []struct {
		ct   string
		head []byte
		ok   bool
	}{
		{"application/pdf", pdf, true},
		{"application/pdf", []byte("PK\x03\x04"), false},
		{"image/png", []byte("\x89PNG\r\n\x1a\nxxxx"), true},
		{"image/jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, true},
		{"image/webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), true},
		{"image/webp", []byte("RIFF\x00\x00\x00\x00WAVE"), false},
		{documents.ContentTypeXLSX, []byte("PK\x03\x04rest"), true},
		{documents.ContentTypeCSV, []byte("invoice_number,issue_date\nA-1,2026-01-01\n"), true},
		{documents.ContentTypeCSV, []byte("a,b\x00c"), false},
		{documents.ContentTypeCSV, pdf, false},
	}
	for _, c := range cases {
		if got := documents.SniffMatches(c.ct, c.head); got != c.ok {
			t.Errorf("%s %q: %v", c.ct, c.head, got)
		}
	}
	f, d := uuid.MustParse("11111111-1111-4111-8111-111111111111"), uuid.MustParse("22222222-2222-4222-8222-222222222222")
	if k := documents.ObjectKey(f, d); k != "firms/11111111-1111-4111-8111-111111111111/docs/22222222-2222-4222-8222-222222222222" {
		t.Fatal(k)
	}
	if n := documents.NumericFromFloat(0.9876); !n.Valid || n.Int.Int64() != 988 || n.Exp != -3 {
		t.Fatalf("%+v", n)
	}
}

// ------------------------------------------------------------------ fakes

type fakeStore struct {
	mu        sync.Mutex
	docs      map[uuid.UUID]sqlc.Document
	archived  bool
	published []string
}

func newStore() *fakeStore { return &fakeStore{docs: map[uuid.UUID]sqlc.Document{}} }

func (s *fakeStore) PreparePending(_ context.Context, firm, cc uuid.UUID, by string, files []documents.FileIn) ([]documents.Pending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cc == uuid.Nil {
		return nil, documents.ErrClientNotFound
	}
	if s.archived {
		return nil, documents.ErrClientArchived
	}
	var out []documents.Pending
	for _, f := range files {
		var existing *sqlc.Document
		for _, d := range s.docs {
			if d.FirmID == firm && d.ClientCompanyID == cc && d.Sha256 == f.SHA256 {
				existing = &d
			}
		}
		if existing != nil && existing.Status != "pending_upload" && existing.Status != "rejected" {
			out = append(out, documents.Pending{Doc: *existing, Deduplicated: true})
			continue
		}
		id := uuid.New()
		if existing != nil {
			id = existing.ID
		}
		d := sqlc.Document{ID: id, FirmID: firm, ClientCompanyID: cc, Sha256: f.SHA256, ObjectKey: documents.ObjectKey(firm, id),
			Filename: f.Filename, ContentType: f.ContentType, SizeBytes: f.SizeBytes, Status: "pending_upload",
			ReprocessNonce: uuid.NewString(), UploadedBy: by}
		s.docs[id] = d
		out = append(out, documents.Pending{Doc: d})
	}
	return out, nil
}

func (s *fakeStore) Get(_ context.Context, firm, id uuid.UUID) (sqlc.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[id]
	if !ok || d.FirmID != firm {
		return sqlc.Document{}, db.ErrNotFound
	}
	return d, nil
}

func (s *fakeStore) FinishUpload(_ context.Context, firm, id uuid.UUID, reason string) (sqlc.Document, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.docs[id]
	if d.Status != "pending_upload" {
		return d, false, nil
	}
	d.Status, d.StatusReason = "uploaded", ""
	if reason != "" {
		d.Status, d.StatusReason = "rejected", reason
	}
	s.docs[id] = d
	return d, true, nil
}

func (s *fakeStore) Candidates(context.Context, uuid.UUID) ([]*compliancev1.ClientCompanyRef, error) {
	return []*compliancev1.ClientCompanyRef{{ClientCompanyId: "c1", Name: "A", Trn: "100000000000001"}}, nil
}

func (s *fakeStore) MarkPublished(_ context.Context, _, id uuid.UUID, nonce string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = append(s.published, id.String()+":"+nonce)
	return nil
}

func (s *fakeStore) List(_ context.Context, firm uuid.UUID, f documents.ListFilter) ([]sqlc.Document, error) {
	var out []sqlc.Document
	for _, d := range s.docs {
		if d.FirmID == firm && (f.Status == "" || d.Status == f.Status) {
			out = append(out, d)
		}
	}
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *fakeStore) Invoices(context.Context, uuid.UUID, uuid.UUID) ([]documents.InvoiceRef, error) {
	c := 0.93
	return []documents.InvoiceRef{{ID: uuid.New(), Status: "extracted", SourceOrdinal: 0, ExtractionConfidence: &c}}, nil
}

type fakePresigner struct{ puts []string }

func (p *fakePresigner) PresignPut(_ context.Context, key, ct string, size int64, ttl time.Duration) (*url.URL, http.Header, error) {
	p.puts = append(p.puts, key)
	if ttl != documents.PutTTL || size <= 0 {
		return nil, nil, errors.New("bad presign args")
	}
	return &url.URL{Scheme: "http", Host: "minio.local:9000", Path: "/documents/" + key}, http.Header{"Content-Type": {ct}}, nil
}

func (p *fakePresigner) PresignGet(_ context.Context, key, _ string, _ time.Duration) (*url.URL, error) {
	return &url.URL{Scheme: "http", Host: "minio.local:9000", Path: "/documents/" + key}, nil
}

type fakeObjects struct {
	data    map[string][]byte
	deleted []string
	fail    bool
}

func (o *fakeObjects) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	if o.fail {
		return nil, 0, errors.New("connection refused")
	}
	b, ok := o.data[key]
	if !ok {
		return nil, 0, storage.ErrObjectMissing
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}

func (o *fakeObjects) Delete(_ context.Context, key string) error {
	o.deleted = append(o.deleted, key)
	return nil
}

type fakeBus struct {
	msgs []string
	last *compliancev1.DocumentUploaded
	err  error
}

func (b *fakeBus) Publish(_ context.Context, subject, msgID string, m proto.Message) error {
	if b.err != nil {
		return b.err
	}
	b.msgs = append(b.msgs, subject+"|"+msgID)
	b.last = m.(*compliancev1.DocumentUploaded)
	return nil
}

type rig struct {
	svc   *documents.Service
	store *fakeStore
	pre   *fakePresigner
	obj   *fakeObjects
	bus   *fakeBus
	firm  uuid.UUID
	cc    uuid.UUID
}

func newRig() *rig {
	r := &rig{store: newStore(), pre: &fakePresigner{}, obj: &fakeObjects{data: map[string][]byte{}}, bus: &fakeBus{},
		firm: uuid.New(), cc: uuid.New()}
	r.svc = &documents.Service{Store: r.store, Presigner: r.pre, Objects: r.obj, Bus: r.bus,
		Now: func() time.Time { return time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC) }}
	return r
}

func file(b []byte) documents.FileIn {
	return documents.FileIn{Filename: "a.pdf", ContentType: "application/pdf", SizeBytes: int64(len(b)), SHA256: hexSHA(b)}
}

// ------------------------------------------------------------------ service

func TestRequestUploadsSignsAndDeduplicates(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	bad := file(pdf)
	bad.SizeBytes = 0
	items, err := r.svc.RequestUploads(ctx, r.firm, r.cc, "user-1", []documents.FileIn{file(pdf), bad, file(pdf)})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Upload == nil || items[0].Deduplicated || items[0].Status != "pending_upload" {
		t.Fatalf("first: %+v", items[0])
	}
	if items[0].Upload.Method != "PUT" || items[0].Upload.Headers["Content-Type"] != "application/pdf" ||
		!items[0].Upload.ExpiresAt.Equal(time.Date(2026, 9, 30, 10, 15, 0, 0, time.UTC)) {
		t.Fatalf("upload: %+v", items[0].Upload)
	}
	if items[1].Error != documents.CodeInvalidSize || items[1].DocumentID != nil {
		t.Fatalf("invalid: %+v", items[1])
	}
	if !items[2].Deduplicated || items[2].Upload != nil || *items[2].DocumentID != *items[0].DocumentID {
		t.Fatalf("in-request duplicate: %+v", items[2])
	}
	if len(r.pre.puts) != 1 || len(r.store.docs) != 1 {
		t.Fatalf("puts=%v docs=%d", r.pre.puts, len(r.store.docs))
	}
	// Upload and complete, then ask again: deduplicated, no row, no presign, no message (AC-E3a).
	id := *items[0].DocumentID
	r.obj.data[r.store.docs[id].ObjectKey] = pdf
	if got := r.svc.Complete(ctx, r.firm, []string{id.String()}); got[0].Status != "uploaded" || got[0].Error != "" {
		t.Fatalf("complete: %+v", got)
	}
	again, err := r.svc.RequestUploads(ctx, r.firm, r.cc, "user-1", []documents.FileIn{file(pdf)})
	if err != nil || !again[0].Deduplicated || again[0].Upload != nil || again[0].Status != "uploaded" {
		t.Fatalf("again: %+v %v", again, err)
	}
	if len(r.pre.puts) != 1 || len(r.store.docs) != 1 || len(r.bus.msgs) != 1 {
		t.Fatalf("dedup side effects: puts=%d docs=%d msgs=%d", len(r.pre.puts), len(r.store.docs), len(r.bus.msgs))
	}
}

func TestCompletePublishesOnceWithCandidates(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	items, _ := r.svc.RequestUploads(ctx, r.firm, r.cc, "u", []documents.FileIn{file(pdf)})
	id := *items[0].DocumentID
	doc := r.store.docs[id]
	r.obj.data[doc.ObjectKey] = pdf
	for range 2 {
		got := r.svc.Complete(ctx, r.firm, []string{id.String()})
		if got[0].Status != "uploaded" || got[0].Error != "" {
			t.Fatalf("%+v", got)
		}
	}
	want := "document.uploaded|document.uploaded:" + id.String() + ":" + doc.ReprocessNonce
	if len(r.bus.msgs) != 1 || r.bus.msgs[0] != want {
		t.Fatalf("msgs %v", r.bus.msgs)
	}
	m := r.bus.last
	if m.GetObjectKey() != doc.ObjectKey || m.GetSha256() != doc.Sha256 || len(m.GetCandidates()) != 1 ||
		m.GetFirmId() != r.firm.String() || m.GetReprocessNonce() != doc.ReprocessNonce {
		t.Fatalf("payload %v", m)
	}
	if len(r.store.published) != 1 || r.store.published[0] != id.String()+":"+doc.ReprocessNonce {
		t.Fatalf("published %v", r.store.published)
	}
}

func TestCompleteRejectsAndDeletes(t *testing.T) {
	cases := map[string]struct {
		stored []byte
		decl   []byte
		ct     string
	}{
		documents.RejectObjectMissing:       {nil, pdf, "application/pdf"},
		documents.RejectSizeMismatch:        {append(append([]byte{}, pdf...), 'x'), pdf, "application/pdf"},
		documents.RejectSHA256Mismatch:      {bytes.Replace(pdf, []byte("obj"), []byte("OBJ"), 1), pdf, "application/pdf"},
		documents.RejectContentTypeMismatch: {pdf, pdf, "image/png"},
	}
	for reason, c := range cases {
		t.Run(reason, func(t *testing.T) {
			r := newRig()
			f := file(c.decl)
			f.ContentType = c.ct
			items, _ := r.svc.RequestUploads(context.Background(), r.firm, r.cc, "u", []documents.FileIn{f})
			doc := r.store.docs[*items[0].DocumentID]
			if c.stored != nil {
				r.obj.data[doc.ObjectKey] = c.stored
			}
			got := r.svc.Complete(context.Background(), r.firm, []string{doc.ID.String()})
			if got[0].Status != "rejected" || got[0].Error != reason {
				t.Fatalf("%+v", got)
			}
			if len(r.obj.deleted) != 1 || r.obj.deleted[0] != doc.ObjectKey || len(r.bus.msgs) != 0 {
				t.Fatalf("deleted=%v msgs=%v", r.obj.deleted, r.bus.msgs)
			}
		})
	}
}

func TestCompleteEdgeCases(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	items, _ := r.svc.RequestUploads(ctx, r.firm, r.cc, "u", []documents.FileIn{file(pdf)})
	id := *items[0].DocumentID
	r.obj.fail = true
	got := r.svc.Complete(ctx, r.firm, []string{id.String(), "nope", uuid.NewString()})
	if got[0].Error != documents.CodeStorageUnavailable || got[0].Status != "pending_upload" {
		t.Fatalf("storage outage must not reject: %+v", got[0])
	}
	if got[1].Error != documents.CodeNotFound || got[2].Error != documents.CodeNotFound {
		t.Fatalf("%+v", got)
	}
	// Another Firm cannot complete it.
	if other := r.svc.Complete(ctx, uuid.New(), []string{id.String()}); other[0].Error != documents.CodeNotFound {
		t.Fatalf("%+v", other)
	}
	// A failed publish leaves the Document uploaded and unpublished (the reconciler retries).
	r.obj.fail = false
	r.obj.data[r.store.docs[id].ObjectKey] = pdf
	r.bus.err = errors.New("nats down")
	if got := r.svc.Complete(ctx, r.firm, []string{id.String()}); got[0].Status != "uploaded" || got[0].Error != "" {
		t.Fatalf("%+v", got)
	}
	if len(r.store.published) != 0 {
		t.Fatal("marked published without an ack")
	}
}

// ------------------------------------------------------------------ HTTP

func serve(r *rig) http.Handler {
	mux := chi.NewRouter()
	firm := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(httpx.WithFirmID(req.Context(), r.firm)))
		})
	}
	documents.NewHandler(r.svc).Mount(mux, firm, firm, firm)
	return mux
}

func do(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestHandlerCodes(t *testing.T) {
	r := newRig()
	h := serve(r)
	for _, c := range []struct {
		method, path, body string
		code               int
		err                string
	}{
		{"POST", "/v1/documents/uploads", `{"client_company_id":"x","files":[]}`, 400, "invalid_client_company_id"},
		{"POST", "/v1/documents/uploads", `{"client_company_id":"` + r.cc.String() + `","files":[]}`, 400, "invalid_files"},
		{"POST", "/v1/documents/uploads", `{"client_company_id":"` + uuid.Nil.String() + `","files":[{"filename":"a","content_type":"text/csv","size_bytes":1,"sha256":"` + hexSHA(pdf) + `"}]}`, 404, "client_company_not_found"},
		{"POST", "/v1/documents/uploads", `{"bogus":1}`, 400, "invalid_json"},
		{"POST", "/v1/documents/complete", `{"document_ids":[]}`, 400, "invalid_document_ids"},
		{"GET", "/v1/documents?status=weird", "", 400, "invalid_status"},
		{"GET", "/v1/documents?client_company_id=zz", "", 400, "invalid_client_company_id"},
		{"GET", "/v1/documents?cursor=not-a-cursor", "", 400, "invalid_cursor"},
		{"GET", "/v1/documents/not-a-uuid", "", 404, "not_found"},
		{"GET", "/v1/documents/" + uuid.NewString() + "/download", "", 404, "not_found"},
	} {
		code, out := do(t, h, c.method, c.path, c.body)
		if code != c.code || out["error"] != c.err {
			t.Errorf("%s %s: %d %v", c.method, c.path, code, out)
		}
	}
	r.store.archived = true
	code, out := do(t, h, "POST", "/v1/documents/uploads", `{"client_company_id":"`+r.cc.String()+`","files":[{"filename":"a.pdf","content_type":"application/pdf","size_bytes":5,"sha256":"`+hexSHA(pdf)+`"}]}`)
	if code != 409 || out["error"] != "client_company_archived" {
		t.Fatalf("%d %v", code, out)
	}
}

func TestHandlerHappyPath(t *testing.T) {
	r := newRig()
	h := serve(r)
	body := `{"client_company_id":"` + r.cc.String() + `","files":[{"filename":"a.pdf","content_type":"application/pdf","size_bytes":` +
		"29" + `,"sha256":"` + hexSHA(pdf) + `"},{"filename":"","content_type":"application/pdf","size_bytes":1,"sha256":"` + hexSHA(pdf) + `"}]}`
	code, out := do(t, h, "POST", "/v1/documents/uploads", body)
	items := out["items"].([]any)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	if code != 200 || first["upload"] == nil || second["error"] != "invalid_filename" || second["document_id"] != nil {
		t.Fatalf("%d %v", code, out)
	}
	id := first["document_id"].(string)
	r.obj.data[r.store.docs[uuid.MustParse(id)].ObjectKey] = pdf
	code, out = do(t, h, "POST", "/v1/documents/complete", `{"document_ids":["`+id+`"]}`)
	if code != 200 || out["items"].([]any)[0].(map[string]any)["status"] != "uploaded" {
		t.Fatalf("%d %v", code, out)
	}
	code, out = do(t, h, "GET", "/v1/documents/"+id, "")
	if code != 200 || out["status"] != "uploaded" || len(out["invoices"].([]any)) != 1 || out["review_reasons"] == nil {
		t.Fatalf("%d %v", code, out)
	}
	code, out = do(t, h, "GET", "/v1/documents/"+id+"/download", "")
	if code != 200 || !strings.Contains(out["url"].(string), "/docs/"+id) {
		t.Fatalf("%d %v", code, out)
	}
	code, out = do(t, h, "GET", "/v1/documents?limit=1", "")
	if code != 200 || len(out["items"].([]any)) != 1 || out["next_cursor"] != nil {
		t.Fatalf("%d %v", code, out)
	}
	// A pending Document has no verified bytes to download.
	_, out = do(t, h, "POST", "/v1/documents/uploads", `{"client_company_id":"`+r.cc.String()+`","files":[{"filename":"b.pdf","content_type":"application/pdf","size_bytes":3,"sha256":"`+strings.Repeat("a", 64)+`"}]}`)
	pending := out["items"].([]any)[0].(map[string]any)["document_id"].(string)
	if code, out := do(t, h, "GET", "/v1/documents/"+pending+"/download", ""); code != 409 || out["error"] != "not_downloadable" {
		t.Fatalf("%d %v", code, out)
	}
}
