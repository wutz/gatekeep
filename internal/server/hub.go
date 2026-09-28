package server

import (
	"encoding/json"
	"sync"
)

// Hub is a tiny pub/sub used for SSE pushes and Wait wakeups.
type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

// NewHub creates a hub.
func NewHub() *Hub { return &Hub{subs: map[chan []byte]struct{}{}} }

// Subscribe returns a channel of JSON events.
func (h *Hub) Subscribe() chan []byte {
	c := make(chan []byte, 16)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	return c
}

// Unsubscribe removes c.
func (h *Hub) Unsubscribe(c chan []byte) {
	h.mu.Lock()
	delete(h.subs, c)
	h.mu.Unlock()
}

// Publish broadcasts an event without blocking on slow subscribers.
func (h *Hub) Publish(kind, id string) {
	b, _ := json.Marshal(map[string]string{"type": kind, "id": id})
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- b:
		default:
		}
	}
}
