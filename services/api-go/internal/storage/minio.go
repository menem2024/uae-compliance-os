// Package storage wraps the MinIO (S3) client used for documents.
package storage

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// DocumentsBucket holds uploaded invoice documents.
const DocumentsBucket = "documents"

// New builds a MinIO client (no network I/O).
func New(endpoint, accessKey, secretKey string, useSSL bool) (*minio.Client, error) {
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client %s: %w", endpoint, err)
	}
	return c, nil
}

// EnsureBucket creates bucket if it does not exist.
func EnsureBucket(ctx context.Context, c *minio.Client, bucket string) error {
	ok, err := c.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("bucket exists %s: %w", bucket, err)
	}
	if ok {
		return nil
	}
	if err := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		if code := minio.ToErrorResponse(err).Code; code == "BucketAlreadyOwnedByYou" || code == "BucketAlreadyExists" {
			return nil
		}
		return fmt.Errorf("make bucket %s: %w", bucket, err)
	}
	return nil
}

// CheckBucket fails unless bucket exists; used by /readyz.
func CheckBucket(ctx context.Context, c *minio.Client, bucket string) error {
	ok, err := c.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("bucket exists %s: %w", bucket, err)
	}
	if !ok {
		return fmt.Errorf("bucket %s missing", bucket)
	}
	return nil
}
