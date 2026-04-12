package sqs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/siigofiscal/go_backend/internal/domain/port"
)

const (
	maxSQSMessageBytes = 256 * 1024 // 256 KB
	// maxDelaySQS caps DelaySeconds for AWS SQS (hard limit: 900s = 15 min).
	// Azure Service Bus ScheduledEnqueueTime has no such limit; the publisher
	// converts DelaySeconds to an absolute timestamp, so larger values work fine.
	maxDelaySQS = 15 * time.Minute
)

// Handler implements event.EventHandler by serializing the event and
// publishing it to the configured SQS queue.
// Mirrors Python's SQSHandler in chalicelib/new/shared/infra/sqs_handler.py.
type Handler struct {
	QueueURL string
	pub      port.MessagePublisher
}

func NewHandler(queueURL string, pub port.MessagePublisher) *Handler {
	return &Handler{QueueURL: queueURL, pub: pub}
}

// Handle serializes the event to JSON and sends it to SQS.
// The parameter is interface{} to avoid an import cycle with the event package.
func (h *Handler) Handle(ev interface{}) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("sqs_handler: marshal: %w", err)
	}

	if len(body) > maxSQSMessageBytes {
		slog.Error("sqs_handler: message too large", "queue", h.QueueURL, "bytes", len(body))
		return nil // match Python behaviour: log and skip, no error propagated
	}

	in := &port.SendMessageInput{
		QueueURL: h.QueueURL,
		Body:     string(body),
	}

	// FIFO queues require MessageGroupId + MessageDeduplicationId.
	if isFIFO(h.QueueURL) {
		id := uuid.NewString()
		in.MessageGroupID = id
		in.MessageDeduplicationID = id
	}

	// Handle execute_at → DelaySeconds.
	// Azure Service Bus supports arbitrary ScheduledEnqueueTime; only cap for AWS SQS (15 min).
	if delayed, ok := ev.(interface{ GetExecuteAt() *time.Time }); ok {
		if execAt := delayed.GetExecuteAt(); execAt != nil {
			delay := time.Until(*execAt)
			if delay > 0 {
				in.DelaySeconds = int32(delay.Seconds())
			}
		}
	}

	slog.Warn("sqs_handler: sending", "queue", h.QueueURL)
	if err := h.pub.SendMessage(context.Background(), in); err != nil {
		return fmt.Errorf("sqs_handler: send to %s: %w", h.QueueURL, err)
	}
	return nil
}

func isFIFO(url string) bool {
	parts := strings.Split(url, ".")
	return len(parts) > 0 && parts[len(parts)-1] == "fifo"
}
