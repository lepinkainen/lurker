// Package hub is a tiny in-process pub/sub fan-out used to push IRC
// events from the persistence path to any number of API consumers
// (primarily WebSocket clients).
package hub

import "sync"

// Hub fans out Publish calls to every live subscriber. Sends are
// non-blocking per subscriber — if a subscriber's buffer is full the
// event is dropped for that subscriber only. Callers should treat
// dropped events as a signal that the subscriber is slow and reconnect
// with a fresh state snapshot.
type Hub struct {
	mu   sync.RWMutex
	subs map[chan any]struct{}
}

// New returns a new, empty Hub.
func New() *Hub { return &Hub{subs: make(map[chan any]struct{})} }

// Subscribe registers a new subscriber and returns a receive channel
// plus a cleanup func. The buffer controls how many events can queue up
// before sends start dropping.
func (h *Hub) Subscribe(buf int) (<-chan any, func()) {
	ch := make(chan any, buf)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Publish sends e to every subscriber, dropping the event for any
// subscriber whose buffer is currently full. Never blocks.
func (h *Hub) Publish(e any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
