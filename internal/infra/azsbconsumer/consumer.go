// Package azsbconsumer provides a generic Azure Service Bus queue consumer: long-poll
// receive, per-queue handler dispatch, and peek-lock settlement (complete / abandon / dead-letter).
package azsbconsumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

// AckOutcome selects how the consumer settles a message with Service Bus.
type AckOutcome int

const (
	// AckComplete removes the message from the queue (success).
	AckComplete AckOutcome = iota
	// AckAbandon returns the message for redelivery (transient failure).
	AckAbandon
	// AckDeadLetter moves the message to the dead-letter sub-queue (permanent failure).
	AckDeadLetter
)

// Incoming is one dequeued message passed to a handler (body is raw; handlers may JSON-decode).
type Incoming struct {
	QueueName     string
	Body          []byte
	MessageID     string
	DeliveryCount uint32
}

// HandleResult tells the consumer how to settle the message after Handle returns.
type HandleResult struct {
	Outcome AckOutcome
	// DeadLetterReason and DeadLetterDescription are used when Outcome == AckDeadLetter.
	DeadLetterReason      string
	DeadLetterDescription string
	// Err is logged for non-complete outcomes (optional).
	Err error
}

// Handler processes a single Service Bus message under peek-lock.
type Handler func(ctx context.Context, msg Incoming) HandleResult

// Options tunes receive behavior for all registered queues.
type Options struct {
	// MaxMessagesPerReceive is passed to ReceiveMessages (default 1).
	MaxMessagesPerReceive int
	// MinReceiveBackoff is the initial delay after a ReceiveMessages error (default 200ms).
	MinReceiveBackoff time.Duration
	// MaxReceiveBackoff caps exponential backoff (default 30s).
	MaxReceiveBackoff time.Duration
}

// Consumer listens to multiple queues on one Service Bus namespace (listen-capable connection string).
type Consumer struct {
	client  *azservicebus.Client
	opts    Options
	mu      sync.Mutex
	handler map[string]Handler
}

// New builds a consumer from a Service Bus connection string (Shared Access Policy with Listen).
func New(connectionString string, opts Options) (*Consumer, error) {
	if strings.TrimSpace(connectionString) == "" {
		return nil, errors.New("azsbconsumer: empty connection string")
	}
	c, err := azservicebus.NewClientFromConnectionString(connectionString, nil)
	if err != nil {
		return nil, fmt.Errorf("azsbconsumer: service bus client: %w", err)
	}
	normalizeOptions(&opts)
	return &Consumer{
		client:  c,
		opts:    opts,
		handler: make(map[string]Handler),
	}, nil
}

func normalizeOptions(o *Options) {
	if o.MaxMessagesPerReceive < 1 {
		o.MaxMessagesPerReceive = 1
	}
	if o.MinReceiveBackoff <= 0 {
		o.MinReceiveBackoff = 200 * time.Millisecond
	}
	if o.MaxReceiveBackoff <= 0 {
		o.MaxReceiveBackoff = 30 * time.Second
	}
	if o.MaxReceiveBackoff < o.MinReceiveBackoff {
		o.MaxReceiveBackoff = o.MinReceiveBackoff
	}
}

// RegisterQueue binds a queue name to a handler. Call before Run. Not safe for concurrent RegisterQueue.
func (c *Consumer) RegisterQueue(queueName string, h Handler) error {
	if c == nil {
		return errors.New("azsbconsumer: nil consumer")
	}
	name := strings.TrimSpace(queueName)
	if name == "" {
		return errors.New("azsbconsumer: empty queue name")
	}
	if h == nil {
		return errors.New("azsbconsumer: nil handler")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.handler[name]; dup {
		return fmt.Errorf("azsbconsumer: duplicate queue %q", name)
	}
	c.handler[name] = h
	return nil
}

// Close closes the underlying Service Bus client.
func (c *Consumer) Close(ctx context.Context) error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close(ctx)
}

// Run starts one receive loop per registered queue and blocks until ctx is cancelled.
// After ctx ends, all receivers are closed and the WaitGroup completes.
func (c *Consumer) Run(ctx context.Context) error {
	if c == nil {
		return errors.New("azsbconsumer: nil consumer")
	}
	c.mu.Lock()
	routes := maps.Clone(c.handler)
	c.mu.Unlock()
	if len(routes) == 0 {
		return errors.New("azsbconsumer: no queues registered")
	}

	var wg sync.WaitGroup
	errOnce := make(chan error, 1)
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for q, h := range routes {
		wg.Add(1)
		go func(queue string, handler Handler) {
			defer wg.Done()
			if err := c.runQueue(childCtx, queue, handler); err != nil && !errors.Is(err, context.Canceled) {
				select {
				case errOnce <- fmt.Errorf("azsbconsumer: queue %q: %w", queue, err):
				default:
				}
				cancel()
			}
		}(q, h)
	}

	wg.Wait()
	select {
	case err := <-errOnce:
		return err
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (c *Consumer) runQueue(ctx context.Context, queue string, handler Handler) error {
	recv, err := c.client.NewReceiverForQueue(queue, &azservicebus.ReceiverOptions{
		ReceiveMode: azservicebus.ReceiveModePeekLock,
	})
	if err != nil {
		return fmt.Errorf("NewReceiverForQueue: %w", err)
	}
	defer func() { _ = recv.Close(context.Background()) }()

	backoff := c.opts.MinReceiveBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		msgs, err := recv.ReceiveMessages(ctx, c.opts.MaxMessagesPerReceive, nil)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			slog.Error("azsbconsumer: ReceiveMessages", "queue", queue, "err", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = minDuration(backoff*2, c.opts.MaxReceiveBackoff)
			continue
		}
		backoff = c.opts.MinReceiveBackoff

		for _, rm := range msgs {
			dispatch(ctx, recv, queue, rm, handler)
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func dispatch(ctx context.Context, recv *azservicebus.Receiver, queue string, rm *azservicebus.ReceivedMessage, h Handler) {
	msgID := ""
	if rm != nil {
		msgID = rm.MessageID
	}
	logArgs := []any{"queue", queue, "message_id", msgID}

	var res HandleResult
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("azsbconsumer: handler panic", append(logArgs, "recover", r)...)
				res = HandleResult{Outcome: AckAbandon, Err: fmt.Errorf("panic: %v", r)}
			}
		}()
		if rm == nil {
			res = HandleResult{Outcome: AckDeadLetter, DeadLetterReason: "NilMessage", DeadLetterDescription: "received nil message pointer"}
			return
		}
		in := Incoming{
			QueueName:     queue,
			Body:          rm.Body,
			MessageID:     rm.MessageID,
			DeliveryCount: rm.DeliveryCount,
		}
		res = h(ctx, in)
	}()

	settle(ctx, recv, rm, res, logArgs)
}

func settle(ctx context.Context, recv *azservicebus.Receiver, rm *azservicebus.ReceivedMessage, res HandleResult, logArgs []any) {
	if rm == nil {
		return
	}
	var err error
	switch res.Outcome {
	case AckComplete:
		err = recv.CompleteMessage(ctx, rm, nil)
		if err != nil {
			slog.Error("azsbconsumer: CompleteMessage", append(logArgs, "err", err)...)
		}
	case AckAbandon:
		if res.Err != nil {
			slog.Warn("azsbconsumer: abandoning message", append(logArgs, "err", res.Err)...)
		}
		err = recv.AbandonMessage(ctx, rm, nil)
		if err != nil {
			slog.Error("azsbconsumer: AbandonMessage", append(logArgs, "err", err)...)
		}
	case AckDeadLetter:
		if res.Err != nil {
			slog.Warn("azsbconsumer: dead-lettering message", append(logArgs, "err", res.Err)...)
		}
		reason := res.DeadLetterReason
		if strings.TrimSpace(reason) == "" {
			reason = "ProcessingFailed"
		}
		desc := res.DeadLetterDescription
		if strings.TrimSpace(desc) == "" && res.Err != nil {
			desc = res.Err.Error()
		}
		r, d := reason, desc
		opts := &azservicebus.DeadLetterOptions{Reason: &r, ErrorDescription: &d}
		err = recv.DeadLetterMessage(ctx, rm, opts)
		if err != nil {
			slog.Error("azsbconsumer: DeadLetterMessage", append(logArgs, "err", err)...)
		}
	default:
		slog.Error("azsbconsumer: unknown outcome, abandoning", append(logArgs, "outcome", res.Outcome)...)
		_ = recv.AbandonMessage(ctx, rm, nil)
	}
}
