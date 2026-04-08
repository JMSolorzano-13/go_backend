// Package azsbpub implements port.MessagePublisher for Azure Service Bus queues.
// Terraform (terraform_azure/modules/messaging) provisions Service Bus entities;
// this must be used for CLOUD_PROVIDER=azure in production — not Storage Queues (azqueue).
package azsbpub

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"

	"github.com/siigofiscal/go_backend/internal/domain/port"
)

// Publisher sends JSON payloads to Service Bus queues. Queue names match SQS_* env values:
// either a plain name (e.g. queue-create-query) or a URL whose last path segment is the name.
type Publisher struct {
	client *azservicebus.Client
}

var _ port.MessagePublisher = (*Publisher)(nil)

// NewPublisher builds a publisher from a Service Bus namespace connection string
// (Shared Access Policy with Send permission).
func NewPublisher(connectionString string) (*Publisher, error) {
	if strings.TrimSpace(connectionString) == "" {
		return nil, errors.New("azsbpub: empty connection string")
	}
	c, err := azservicebus.NewClientFromConnectionString(connectionString, nil)
	if err != nil {
		return nil, fmt.Errorf("azsbpub: service bus client: %w", err)
	}
	return &Publisher{client: c}, nil
}

// Close releases the underlying client.
func (p *Publisher) Close(ctx context.Context) error {
	if p == nil || p.client == nil {
		return nil
	}
	return p.client.Close(ctx)
}

// SendMessage implements port.MessagePublisher.
func (p *Publisher) SendMessage(ctx context.Context, in *port.SendMessageInput) error {
	if in == nil {
		return errors.New("azsbpub: nil SendMessageInput")
	}
	name := queueNameFromSQSURL(in.QueueURL)
	if name == "" {
		return fmt.Errorf("azsbpub: could not parse queue name from %q", in.QueueURL)
	}

	sender, err := p.client.NewSender(name, nil)
	if err != nil {
		return fmt.Errorf("azsbpub: NewSender %q: %w", name, err)
	}
	defer func() { _ = sender.Close(ctx) }()

	msg := &azservicebus.Message{Body: []byte(in.Body)}
	if in.DelaySeconds > 0 {
		t := time.Now().UTC().Add(time.Duration(in.DelaySeconds) * time.Second)
		msg.ScheduledEnqueueTime = &t
	}

	if err := sender.SendMessage(ctx, msg, nil); err != nil {
		return fmt.Errorf("azsbpub: SendMessage %q: %w", name, err)
	}
	return nil
}

// queueNameFromSQSURL mirrors internal/infra/azservicebus/publisher.go: last path segment
// or raw token, with underscores mapped to hyphens for Azure queue naming.
func queueNameFromSQSURL(queueURL string) string {
	u, err := url.Parse(strings.TrimSpace(queueURL))
	if err != nil || u.Path == "" {
		return azureQueueName(strings.Trim(strings.TrimSpace(queueURL), "/"))
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) == 0 {
		return ""
	}
	return azureQueueName(segs[len(segs)-1])
}

func azureQueueName(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}
