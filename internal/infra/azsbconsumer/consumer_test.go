package azsbconsumer

import (
	"context"
	"strings"
	"testing"
)

func TestMinDuration(t *testing.T) {
	t.Parallel()
	if minDuration(2, 5) != 2 {
		t.Fatalf("minDuration(2,5) = %v", minDuration(2, 5))
	}
	if minDuration(9, 3) != 3 {
		t.Fatalf("minDuration(9,3) = %v", minDuration(9, 3))
	}
}

func TestNew_NormalizesOptions(t *testing.T) {
	t.Parallel()
	cs := "Endpoint=sb://x.servicebus.windows.net/;SharedAccessKeyName=k;SharedAccessKey=eA=="
	c, err := New(cs, Options{
		MaxMessagesPerReceive: 0,
		MinReceiveBackoff:     0,
		MaxReceiveBackoff:     0,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if c.opts.MaxMessagesPerReceive != 1 {
		t.Fatalf("MaxMessagesPerReceive: got %d", c.opts.MaxMessagesPerReceive)
	}
	if c.opts.MinReceiveBackoff <= 0 {
		t.Fatalf("MinReceiveBackoff: %v", c.opts.MinReceiveBackoff)
	}
	if c.opts.MaxReceiveBackoff < c.opts.MinReceiveBackoff {
		t.Fatalf("MaxReceiveBackoff %v < MinReceiveBackoff %v", c.opts.MaxReceiveBackoff, c.opts.MinReceiveBackoff)
	}
}

func TestRegisterQueue(t *testing.T) {
	t.Parallel()
	c := &Consumer{handler: make(map[string]Handler)}
	h := func(ctx context.Context, msg Incoming) HandleResult {
		return HandleResult{Outcome: AckComplete}
	}
	if err := c.RegisterQueue("", h); err == nil {
		t.Fatal("expected error for empty queue name")
	}
	if err := c.RegisterQueue("q", nil); err == nil {
		t.Fatal("expected error for nil handler")
	}
	if err := c.RegisterQueue("q", h); err != nil {
		t.Fatal(err)
	}
	if err := c.RegisterQueue("q", h); err == nil {
		t.Fatal("expected error for duplicate queue")
	}
}

func TestConsumer_Run_NoQueuesRegistered(t *testing.T) {
	t.Parallel()
	cs := "Endpoint=sb://x.servicebus.windows.net/;SharedAccessKeyName=k;SharedAccessKey=eA=="
	c, err := New(cs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	err = c.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no queues registered") {
		t.Fatalf("Run: %v", err)
	}
}
