//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
)

// TestObjectStoreAndPresignAgainstMinIO runs against a throwaway MinIO:
//
//	docker run -d --rm --name p1-t9-minio -p 4559:9000 \
//	  -e MINIO_ROOT_USER=minio -e MINIO_ROOT_PASSWORD=minio_dev_pw \
//	  bitnamilegacy/minio:2025.7.23-debian-12-r5
//
// (the image pinned in deploy/compose/compose.yaml:74; minio/minio refuses
// anonymous pulls on this host), env TEST_S3_ENDPOINT=localhost:4559. Skips
// when that env var is unset. Remove the container afterwards.
func TestObjectStoreAndPresignAgainstMinIO(t *testing.T) {
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT not set; skipping MinIO integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4("minio", "minio_dev_pw", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	bucket := "p1-t9-" + uuid.NewString()
	waitForMinIO(t, ctx, client)
	if err := storage.EnsureBucket(ctx, client, bucket); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = client.RemoveObject(cleanupCtx, bucket, "key1", minio.RemoveObjectOptions{})
		_ = client.RemoveBucket(cleanupCtx, bucket)
	})

	store := storage.NewObjectStore(client, bucket)

	content := []byte("abc")
	if _, err := client.PutObject(ctx, bucket, "key1", bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
		t.Fatal(err)
	}

	rc, size, err := store.Open(ctx, "key1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if size != 3 || !bytes.Equal(got, content) {
		t.Fatalf("got %q size %d, want %q size 3", got, size, content)
	}

	if _, _, err := store.Open(ctx, "missing-key"); !errors.Is(err, storage.ErrObjectMissing) {
		t.Fatalf("open missing key: got %v, want ErrObjectMissing", err)
	}

	if err := store.Delete(ctx, "key1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, "key1"); err != nil {
		t.Fatalf("second delete: got %v, want nil", err)
	}

	presigner, err := storage.NewPresigner(endpoint, "minio", "minio_dev_pw", "us-east-1", false, bucket)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("hello")
	u, hdr, err := presigner.PresignPut(ctx, "presign-key", "text/plain", int64(len(body)), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = client.RemoveObject(cleanupCtx, bucket, "presign-key", minio.RemoveObjectOptions{})
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", hdr.Get("Content-Type"))
	req.ContentLength = int64(len(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signed put: got status %d, want 200", resp.StatusCode)
	}

	badReq, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	badReq.Header.Set("Content-Type", "application/json")
	badReq.ContentLength = int64(len(body))
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = badResp.Body.Close()
	if badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("signed put with wrong content-type: got status %d, want 403", badResp.StatusCode)
	}
}

func waitForMinIO(t *testing.T, ctx context.Context, c *minio.Client) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := c.ListBuckets(ctx); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal(fmt.Errorf("minio not reachable: %w", lastErr))
}
