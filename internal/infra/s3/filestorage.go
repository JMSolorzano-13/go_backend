package s3

import (
	"context"
	"time"

	"github.com/siigofiscal/go_backend/internal/domain/port"
)

// FileStorageAdapter implements port.FileStorage using the AWS S3 client.
type FileStorageAdapter struct {
	*Client
}

var _ port.FileStorage = FileStorageAdapter{}

// PresignGet implements port.FileStorage.
func (a FileStorageAdapter) PresignGet(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	return a.PresignedURL(ctx, bucket, key, expiry)
}

// PresignPut implements port.FileStorage.
func (a FileStorageAdapter) PresignPut(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	return a.PresignedPutURL(ctx, bucket, key, expiry)
}
