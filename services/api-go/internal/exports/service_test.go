package exports

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var safeName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func TestSafeFilename(t *testing.T) {
	for _, tc := range []struct{ number, kind, want string }{
		{"INV-2026.001", "invoice", "INV-2026.001-invoice.xml"},
		{"INV/2026 001", "invoice", "INV_2026_001-invoice.xml"},
		{"a\"b\r\nContent-Type: x", "credit_note", "a_b__Content-Type__x-credit_note.xml"},
		{"../../etc/passwd", "invoice", ".._.._etc_passwd-invoice.xml"},
		{"فاتورة-1", "invoice", "______-1-invoice.xml"}, // one "_" per rune
		{"", "invoice", "invoice-invoice.xml"},
		{strings.Repeat("a", 500), "invoice", strings.Repeat("a", 100) + "-invoice.xml"},
	} {
		got := safeFilename(tc.number, tc.kind)
		if got != tc.want {
			t.Errorf("safeFilename(%q, %q) = %q, want %q", tc.number, tc.kind, got, tc.want)
		}
		if !safeName.MatchString(got) {
			t.Errorf("safeFilename(%q) = %q has characters outside [A-Za-z0-9._-]", tc.number, got)
		}
	}
}

// fakeS3 is just enough of the S3 object API (path-style PUT and GET) for MinioStore.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING") {
			b = decodeAWSChunked(b)
		}
		f.objects[r.URL.Path] = b
		f.types[r.URL.Path] = r.Header.Get("Content-Type")
		w.Header().Set("ETag", `"abc"`)
	case http.MethodGet:
		b, ok := f.objects[r.URL.Path]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		_, _ = w.Write(b)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// decodeAWSChunked strips the aws-chunked signature framing minio-go uses over plain HTTP:
// "<hex size>;chunk-signature=<sig>\r\n<data>\r\n" repeated, ending with a zero-size chunk.
func decodeAWSChunked(b []byte) []byte {
	var out []byte
	for len(b) > 0 {
		nl := bytes.Index(b, []byte("\r\n"))
		if nl < 0 {
			break
		}
		size, err := strconv.ParseInt(strings.SplitN(string(b[:nl]), ";", 2)[0], 16, 64)
		if err != nil || size == 0 {
			break
		}
		b = b[nl+2:]
		out = append(out, b[:size]...)
		b = b[size+2:]
	}
	return out
}

func TestMinioStoreRoundTrip(t *testing.T) {
	f := &fakeS3{objects: map[string][]byte{}, types: map[string]string{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c, err := minio.New(strings.TrimPrefix(srv.URL, "http://"), &minio.Options{
		Creds: credentials.NewStaticV4("k", "s", ""), Region: "us-east-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	st := &MinioStore{Client: c, Bucket: "documents"}
	ctx := context.Background()
	if err := st.Put(ctx, "firms/f/exports/e.xml", []byte("<Invoice/>"), "application/xml"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := f.types["/documents/firms/f/exports/e.xml"]; got != "application/xml" {
		t.Fatalf("content type %q, objects %v", got, f.objects)
	}
	got, err := st.Get(ctx, "firms/f/exports/e.xml")
	if err != nil || string(got) != "<Invoice/>" {
		t.Fatalf("get = %q, %v", got, err)
	}
	if _, err := st.Get(ctx, "firms/f/exports/missing.xml"); err == nil {
		t.Fatal("missing object must be an error")
	}
	// An object above the 16 MiB bound is refused rather than buffered.
	f.objects["/documents/big.xml"] = make([]byte, MaxObjectBytes+1)
	if _, err := st.Get(ctx, "big.xml"); err == nil {
		t.Fatal("oversize object must be an error")
	}
	f.objects["/documents/max.xml"] = make([]byte, MaxObjectBytes)
	if b, err := st.Get(ctx, "max.xml"); err != nil || len(b) != MaxObjectBytes {
		t.Fatalf("16 MiB object: %d bytes, %v", len(b), err)
	}
}
