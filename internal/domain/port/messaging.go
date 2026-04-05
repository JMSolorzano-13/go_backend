package port

import "context"

// SendMessageInput is a provider-agnostic outbound queue message.
//
// Call-site map: built in internal/infra/sqs/handler.go from domain events
// (today mapped to *sqs.SendMessageInput). sat_request_generator uses the
// AWS SDK directly and is out of scope for this port.
//
// Infra mapping: QueueURL + Body → SQS SendMessage; FIFO uses Group/Dedup IDs;
// DelaySeconds → SQS DelaySeconds (capped in handler).
type SendMessageInput struct {
	QueueURL               string
	Body                   string
	DelaySeconds           int32
	MessageGroupID         string
	MessageDeduplicationID string
}

// MessagePublisher abstracts a message queue (AWS SQS, Azure Service Bus).
//
// Infra mapping: sqs.Publisher (AWS) or azservicebus.QueuePublisher (Azure Queue Storage).
type MessagePublisher interface {
	SendMessage(ctx context.Context, in *SendMessageInput) error
}
