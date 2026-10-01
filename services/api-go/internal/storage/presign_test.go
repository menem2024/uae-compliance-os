package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
)

// Signing is offline: the endpoint below does not exist and is never dialled.
func TestPresignPutSignsTypeAndLength(t *testing.T) {
	p, err := storage.NewPresigner("files.example.test:9000", "minio", "minio_dev_pw", "us-east-1", false, "documents")
	if err != nil {
		t.Fatal(err)
	}
	u, hdr, err := p.PresignPut(context.Background(), "firms/f/docs/d", "application/pdf", 1234, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "files.example.test:9000" || u.Path != "/documents/firms/f/docs/d" {
		t.Fatalf("url %s", u)
	}
	if q.Get("X-Amz-Expires") != "900" || q.Get("X-Amz-SignedHeaders") != "content-length;content-type;host" {
		t.Fatalf("query %v", q)
	}
	if hdr.Get("Content-Type") != "application/pdf" || len(hdr) != 1 {
		t.Fatalf("headers %v", hdr)
	}
}

func TestPresignGetSetsDisposition(t *testing.T) {
	p, err := storage.NewPresigner("files.example.test:9000", "minio", "minio_dev_pw", "", false, "documents")
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.PresignGet(context.Background(), "firms/f/docs/d", "فاتورة 1.pdf", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("X-Amz-Expires") != "300" || !strings.HasPrefix(u.Query().Get("response-content-disposition"), "attachment; filename*=UTF-8''") {
		t.Fatalf("url %s", u)
	}
}
