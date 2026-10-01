package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrObjectMissing is returned when an object does not exist.
var ErrObjectMissing = errors.New("object missing")

// Presigner signs URLs for the browser. It uses its own client on the public
// endpoint (the browser's view of MinIO, or R2 later) with an explicit region,
// so signing never makes a network call.
type Presigner struct {
	c      *minio.Client
	bucket string
}

// NewPresigner builds a presigner for bucket on publicEndpoint ("host:port").
func NewPresigner(publicEndpoint, accessKey, secretKey, region string, useSSL bool, bucket string) (*Presigner, error) {
	if region == "" {
		region = "us-east-1"
	}
	c, err := minio.New(publicEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("presign client %s: %w", publicEndpoint, err)
	}
	return &Presigner{c: c, bucket: bucket}, nil
}

// PresignPut signs a PUT for key that is only valid with exactly this
// Content-Type and Content-Length. It returns the URL and the headers the
// client must send.
func (p *Presigner) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (*url.URL, http.Header, error) {
	h := http.Header{}
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	u, err := p.c.PresignHeader(ctx, http.MethodPut, p.bucket, key, ttl, nil, h)
	if err != nil {
		return nil, nil, fmt.Errorf("presign put: %w", err)
	}
	return u, http.Header{"Content-Type": {contentType}}, nil
}

// PresignGet signs a GET that downloads key as filename.
func (p *Presigner) PresignGet(ctx context.Context, key, filename string, ttl time.Duration) (*url.URL, error) {
	params := url.Values{}
	params.Set("response-content-disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	u, err := p.c.PresignedGetObject(ctx, p.bucket, key, ttl, params)
	if err != nil {
		return nil, fmt.Errorf("presign get: %w", err)
	}
	return u, nil
}

// ObjectStore reads and deletes document objects on the internal endpoint.
type ObjectStore struct {
	c      *minio.Client
	bucket string
}

// NewObjectStore wraps the internal MinIO client.
func NewObjectStore(c *minio.Client, bucket string) *ObjectStore {
	return &ObjectStore{c: c, bucket: bucket}
}

// Open returns the object's reader and size; ErrObjectMissing when absent.
func (s *ObjectStore) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	obj, err := s.c.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("get %s: %w", key, err)
	}
	st, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, 0, ErrObjectMissing
		}
		return nil, 0, fmt.Errorf("stat %s: %w", key, err)
	}
	return obj, st.Size, nil
}

// Delete removes the object; deleting a missing object is not an error.
func (s *ObjectStore) Delete(ctx context.Context, key string) error {
	if err := s.c.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete %s: %w", key, err)
	}
	return nil
}
