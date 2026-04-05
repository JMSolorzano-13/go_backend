package event

import (
	"log/slog"
	"sync"
)

// Bus is the global in-memory pub/sub router.
// Matches Python's EventBus in chalicelib/new/shared/domain/event/event_bus.py.
type Bus struct {
	mu         sync.Mutex
	handlers   map[EventType][]EventHandler
	eventCount map[EventType]int
	lastEvent  DomainEvent
	localInfra bool
}

func NewBus(localInfra bool) *Bus {
	return &Bus{
		handlers:   make(map[EventType][]EventHandler),
		eventCount: make(map[EventType]int),
		localInfra: localInfra,
	}
}

// Subscribe registers a handler for the given event type.
func (b *Bus) Subscribe(eventType EventType, handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish dispatches the event to all registered handlers.
// In local mode (LOCAL_INFRA) the dispatch runs in a goroutine (non-blocking).
// In production mode it runs synchronously, matching Python's sequential _publish.
func (b *Bus) Publish(eventType EventType, event DomainEvent) {
	b.mu.Lock()
	b.eventCount[eventType]++
	b.lastEvent = event
	handlers := make([]EventHandler, len(b.handlers[eventType]))
	copy(handlers, b.handlers[eventType])
	b.mu.Unlock()

	if len(handlers) == 0 {
		slog.Warn("event_bus: no handlers", "event_type", string(eventType))
		return
	}

	if b.localInfra {
		go b.dispatch(eventType, event, handlers)
	} else {
		b.dispatch(eventType, event, handlers)
	}
}

func (b *Bus) dispatch(eventType EventType, event DomainEvent, handlers []EventHandler) {
	for _, h := range handlers {
		slog.Warn("event_bus: dispatching", "event_type", string(eventType))
		if err := h.Handle(event); err != nil {
			slog.Error("event_bus: handler failed", "event_type", string(eventType), "error", err)
		}
	}
}

// EventCount returns the number of times the given event type has been published.
func (b *Bus) EventCount(eventType EventType) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.eventCount[eventType]
}
