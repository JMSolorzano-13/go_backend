// Package azservicebus implements port.MessagePublisher for Azure Storage Queues
// (blob account Queue service; Azurite-compatible). For CLOUD_PROVIDER=azure against
// Terraform Service Bus queues, use internal/infra/azsbpub with AZURE_SERVICEBUS_CONNECTION_STRING.
package azservicebus

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"

	"github.com/siigofiscal/go_backend/internal/domain/port"
)

// QueuePublisher sends messages to Azure Storage queues using the same queue name
// as the last path segment of QueueURL (works for LocalStack-style URLs and
// Azurite URLs such as http://127.0.0.1:10001/devstoreaccount1/queue_export).
type QueuePublisher struct {
	svc *azqueue.ServiceClient
}

var _ port.MessagePublisher = (*QueuePublisher)(nil)

// NewQueuePublisher builds a publisher from a storage connection string (must include QueueEndpoint).
func NewQueuePublisher(connectionString string) (*QueuePublisher, error) {
	if connectionString == "" {
		return nil, errors.New("azservicebus: empty connection string")
	}
	svc, err := azqueue.NewServiceClientFromConnectionString(connectionString, nil)
	if err != nil {
		return nil, fmt.Errorf("azservicebus: service client: %w", err)
	}
	return &QueuePublisher{svc: svc}, nil
}

// SendMessage implements port.MessagePublisher.
func (p *QueuePublisher) SendMessage(ctx context.Context, in *port.SendMessageInput) error {
	if in == nil {
		return errors.New("azservicebus: nil SendMessageInput")
	}
	name := queueNameFromURL(in.QueueURL)
	if name == "" {
		return fmt.Errorf("azservicebus: could not parse queue name from %q", in.QueueURL)
	}
	qc := p.svc.NewQueueClient(name)
	var opts *azqueue.EnqueueMessageOptions
	if in.DelaySeconds > 0 {
		opts = &azqueue.EnqueueMessageOptions{VisibilityTimeout: to.Ptr(in.DelaySeconds)}
	}
	_, err := qc.EnqueueMessage(ctx, in.Body, opts)
	if err != nil {
		return fmt.Errorf("azservicebus enqueue %s: %w", name, err)
	}
	return nil
}

// queueNameFromURL extracts the last path segment from a SQS-style URL
// (e.g. http://localhost:4566/000000000000/queue_export → queue_export)
// and translates underscores to hyphens because Azure Queue Storage only
// allows lowercase letters, digits, and hyphens in queue names.
func queueNameFromURL(queueURL string) string {
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
