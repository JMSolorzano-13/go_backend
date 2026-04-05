package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/siigofiscal/go_backend/internal/config"
)

type Client struct {
	s3     *s3.Client
	presig *s3.PresignClient
}

func NewClient(cfg *config.Config) (*Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.RegionName),
	}

	if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.AWSEndpointURL != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.AWSEndpointURL)
			o.UsePathStyle = true
		})
	}

	svc := s3.NewFromConfig(awsCfg, s3Opts...)
	return &Client{
		s3:     svc,
		presig: s3.NewPresignClient(svc),
	}, nil
}

func (c *Client) Upload(ctx context.Context, bucket, key string, body []byte) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		return fmt.Errorf("s3 upload %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (c *Client) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 download %s/%s: %w", bucket, key, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (c *Client) PresignedURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	req, err := c.presig.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("s3 presign %s/%s: %w", bucket, key, err)
	}
	return req.URL, nil
}

func (c *Client) PresignedPutURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	req, err := c.presig.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("s3 presign put %s/%s: %w", bucket, key, err)
	}
	return req.URL, nil
}

func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 delete %s/%s: %w", bucket, key, err)
	}
	return nil
}

// CertRoute returns the S3 key for a FIEL file. Matches Python's _get_route.
func CertRoute(workspaceID, companyID int64, ext string) string {
	return fmt.Sprintf("ws_%d/c_%d.%s", workspaceID, companyID, ext)
}
