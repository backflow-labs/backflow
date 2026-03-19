package notify

import (
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"
)

const defaultBufferSize = 100

// EventBus provides async fan-out event delivery to registered subscribers.
type EventBus struct {
	ch          chan Event
	subscribers []Notifier
	mu          sync.RWMutex
	done        chan struct{}
	closed      atomic.Bool
	closeOnce   sync.Once
}

// NewEventBus creates and starts an EventBus with a buffered channel.
func NewEventBus() *EventBus {
	b := &EventBus{
		ch:   make(chan Event, defaultBufferSize),
		done: make(chan struct{}),
	}
	go b.deliver()
	return b
}

// Subscribe registers a notifier to receive events.
func (b *EventBus) Subscribe(n Notifier) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = append(b.subscribers, n)
}

// Emit publishes an event to the bus. If the buffer is full or the bus is
// closed, the event is dropped with a warning. Emit never blocks the caller.
func (b *EventBus) Emit(event Event) {
	if b.closed.Load() {
		log.Warn().
			Str("event", string(event.Type)).
			Str("task_id", event.TaskID).
			Msg("event bus closed, dropping event")
		return
	}
	select {
	case b.ch <- event:
	default:
		log.Warn().
			Str("event", string(event.Type)).
			Str("task_id", event.TaskID).
			Msg("event bus buffer full, dropping event")
	}
}

// Close stops the bus and waits for all pending events to be delivered.
func (b *EventBus) Close() {
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		close(b.ch)
		<-b.done
	})
}

func (b *EventBus) deliver() {
	defer close(b.done)
	for event := range b.ch {
		b.mu.RLock()
		subs := make([]Notifier, len(b.subscribers))
		copy(subs, b.subscribers)
		b.mu.RUnlock()

		for _, sub := range subs {
			if err := sub.Notify(event); err != nil {
				log.Warn().
					Err(err).
					Str("event", string(event.Type)).
					Str("task_id", event.TaskID).
					Msg("subscriber notification failed")
			}
		}
	}
}
