// Package eventbus is a tiny in-process publish/subscribe used to push device
// updates to the UI over Server-Sent Events, so the browser does not need to
// poll.
package eventbus

import (
	"sync"
	"time"
)

// Event is one message delivered to subscribers.
type Event struct {
	Type string         `json:"type"`
	At   time.Time      `json:"at"`
	Data map[string]any `json:"data,omitempty"`
}

// Bus is a fan-out of events to a set of subscribers. Publishing never blocks
// on a slow subscriber: its buffer simply drops the oldest pending message.
type Bus struct {
	mu     sync.Mutex
	subs   map[int]chan Event
	nextID int
}

// New returns an empty bus.
func New() *Bus {
	return &Bus{subs: map[int]chan Event{}}
}

// Subscribe returns a channel of events and a cancel function that must be
// called to release it.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan Event, 64)
	b.subs[id] = ch
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

// Publish delivers an event to every subscriber.
func (b *Bus) Publish(ev Event) {
	if b == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// Slow subscriber: drop this message instead of blocking.
		}
	}
}

// PublishData is a convenience for publishing a typed event with a payload.
func (b *Bus) PublishData(typ string, data map[string]any) {
	b.Publish(Event{Type: typ, Data: data})
}
