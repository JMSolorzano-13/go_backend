package sqs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// Client wraps the AWS SDK v2 SQS client with LocalStack-compatible configuration.
type Client struct {
	svc *sqs.Client
}

// NewClient creates an SQS client.
// When endpointURL is non-empty (LOCAL_INFRA), requests are routed to LocalStack.
func NewClient(region, endpointURL, accessKey, secretKey string) (*Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}

	if accessKey != "" && secretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("sqs: load aws config: %w", err)
	}

	var svcOpts []func(*sqs.Options)
	if endpointURL != "" {
		svcOpts = append(svcOpts, func(o *sqs.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
		})
	}

	return &Client{svc: sqs.NewFromConfig(cfg, svcOpts...)}, nil
}

// SendMessage publishes a message body to the given queue URL.
// Extra options (FIFO group/dedup IDs, delay) are passed via the SQS input.
func (c *Client) SendMessage(ctx context.Context, input *sqs.SendMessageInput) error {
	_, err := c.svc.SendMessage(ctx, input)
	return err
}
