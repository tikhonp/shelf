// Package hub is shelf's in-process publish and subscribe. Modules publish
// what changed, such as a download's progress, and the event stream and the
// bot subscribe. Nothing is stored: a client that reconnects reloads what it
// shows, so an event that never arrived costs nothing.
package hub

import "sync"

// Buffer is how many events a subscriber may fall behind before it starts
// missing them.
const Buffer = 64

// Event is one published value.
type Event struct {
	Topic string
	Data  any
}

// Hub fans events out to subscribers.
type Hub struct {
	mu   sync.RWMutex
	subs map[*sub]struct{}
}

type sub struct {
	topics map[string]bool // nil: every topic
	ch     chan Event
}

// New returns an empty hub.
func New() *Hub { return &Hub{subs: map[*sub]struct{}{}} }

// Publish sends v to every subscriber of topic. It never blocks: a
// subscriber whose buffer is full misses the event, so a slow stream can't
// hold up a download.
func (h *Hub) Publish(topic string, v any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs {
		if s.topics != nil && !s.topics[topic] {
			continue
		}
		select {
		case s.ch <- Event{Topic: topic, Data: v}:
		default:
		}
	}
}

// Subscribe returns a channel of the events of topics, or of every topic
// when none is given, and a function that ends the subscription and closes
// the channel.
func (h *Hub) Subscribe(topics ...string) (<-chan Event, func()) {
	s := &sub{ch: make(chan Event, Buffer)}
	if len(topics) > 0 {
		s.topics = map[string]bool{}
		for _, t := range topics {
			s.topics[t] = true
		}
	}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, s)
			h.mu.Unlock()
			close(s.ch)
		})
	}
}
