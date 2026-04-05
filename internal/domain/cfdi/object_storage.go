package cfdi

import (
	"context"
	"time"
)

// ExportObjectStorage uploads export blobs and issues time-limited read URLs.
// Aligns with port.FileStorage (Upload + PresignGet); implemented by S3 and Azure blob adapters.
type ExportObjectStorage interface {
	Upload(ctx context.Context, bucket, key string, body []byte) error
	PresignGet(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
}
