// Package events is a minimal in-process pub/sub hub used to notify the admin
// UI over SSE when something happens in the registry (currently: pushes).
package events

import "sync"

type Event struct {
	Type  string `json:"type"` // push | delete | retention | gc
	Name  string `json:"name"`
	Tag   string `json:"tag,omitempty"`
	Actor string `json:"actor,omitempty"`
}

type Hub struct {
	mu      sync.Mutex
	clients map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan Event]struct{})}
}

func (h *Hub) Subscribe() chan Event {
	ch := make(chan Event, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// Publish notifies all subscribers. Slow or full subscribers are skipped
// rather than blocked on — this runs on the push request's hot path.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- e:
		default:
		}
	}
}
