package azblob

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"

	"github.com/siigofiscal/go_backend/internal/domain/port"
)

// Client implements port.FileStorage against Azure Blob (including Azurite).
type Client struct {
	connString string
	svc        *azblob.Client
}

var _ port.FileStorage = (*Client)(nil)

// NewFromConnectionString builds a blob client. connString must include blob endpoint (Azurite or Azure).
func NewFromConnectionString(connString string) (*Client, error) {
	if connString == "" {
		return nil, fmt.Errorf("azblob: empty connection string")
	}
	svc, err := azblob.NewClientFromConnectionString(connString, nil)
	if err != nil {
		return nil, fmt.Errorf("azblob: new client: %w", err)
	}
	return &Client{connString: connString, svc: svc}, nil
}

func (c *Client) Upload(ctx context.Context, bucket, key string, body []byte) error {
	_, err := c.svc.UploadBuffer(ctx, bucket, key, body, nil)
	if err != nil {
		return fmt.Errorf("azblob upload %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (c *Client) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	out, err := c.svc.DownloadStream(ctx, bucket, key, nil)
	if err != nil {
		return nil, fmt.Errorf("azblob download %s/%s: %w", bucket, key, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	_, err := c.svc.DeleteBlob(ctx, bucket, key, nil)
	if err != nil {
		return fmt.Errorf("azblob delete %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (c *Client) PresignGet(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	_ = ctx
	bc, err := blob.NewClientFromConnectionString(c.connString, bucket, key, nil)
	if err != nil {
		return "", fmt.Errorf("azblob presign get: %w", err)
	}
	return bc.GetSASURL(sas.BlobPermissions{Read: true}, time.Now().Add(expiry), nil)
}

func (c *Client) PresignPut(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	_ = ctx
	bc, err := blob.NewClientFromConnectionString(c.connString, bucket, key, nil)
	if err != nil {
		return "", fmt.Errorf("azblob presign put: %w", err)
	}
	perms := sas.BlobPermissions{Create: true, Write: true}
	return bc.GetSASURL(perms, time.Now().Add(expiry), nil)
}
