package port

import (
	"context"
	"time"
)

// FileStorage abstracts object storage (AWS S3, Azure Blob, Azurite).
//
// Call-site map (handlers → current infra/s3.Client):
//   - coi, company, cfdi, scraper, attachment: Upload / PresignGet / PresignPut
//   - company: Download
//   - attachment: Delete
//
// Infra mapping: Upload→PutObject, Download→GetObject, Delete→DeleteObject,
// PresignGet→PresignGetObject, PresignPut→PresignPutObject.
//
// Narrower domain use: cfdi.ExportObjectStorage (Upload + PresignedURL only);
// Phase 1E may fold that into FileStorage once method names align.
type FileStorage interface {
	Upload(ctx context.Context, bucket, key string, body []byte) error
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	Delete(ctx context.Context, bucket, key string) error
	PresignGet(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
	PresignPut(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
}
