package sqs

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/siigofiscal/go_backend/internal/domain/port"
)

// Publisher implements port.MessagePublisher on top of the AWS SQS client.
type Publisher struct {
	Client *Client
}

var _ port.MessagePublisher = Publisher{}

// SendMessage implements port.MessagePublisher.
func (p Publisher) SendMessage(ctx context.Context, in *port.SendMessageInput) error {
	if p.Client == nil {
		return errors.New("sqs: nil Client")
	}
	if in == nil {
		return errors.New("sqs: nil SendMessageInput")
	}
	input := &sqs.SendMessageInput{
		QueueUrl:     aws.String(in.QueueURL),
		MessageBody:  aws.String(in.Body),
		DelaySeconds: in.DelaySeconds,
	}
	if in.MessageGroupID != "" {
		input.MessageGroupId = aws.String(in.MessageGroupID)
	}
	if in.MessageDeduplicationID != "" {
		input.MessageDeduplicationId = aws.String(in.MessageDeduplicationID)
	}
	_, err := p.Client.svc.SendMessage(ctx, input)
	if err != nil {
		return fmt.Errorf("sqs send: %w", err)
	}
	return nil
}
