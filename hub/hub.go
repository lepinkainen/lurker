// Package hub is a tiny in-process pub/sub fan-out used to push IRC
// events from the persistence path to any number of API consumers
// (primarily WebSocket clients).
package hub

import "sync"

// Hub fans out Publish calls to every live subscriber. Sends are
// non-blocking per subscriber — if a subscriber's buffer is full the
// event is dropped for that subscriber and its overflow channel is
// closed. A dropped event means the subscriber has fallen behind and
// can no longer be trusted to hold a consistent view; the contract is
// that the consumer watches its overflow channel, tears down its
// connection when it fires, and reconnects with a fresh state snapshot.
type Hub struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

// subscriber holds one consumer's event channel plus a one-shot overflow
// signal. overflow is closed exactly once (guarded by once) the first
// time a Publish has to drop an event for this subscriber.
type subscriber struct {
	ch       chan any
	overflow chan struct{}
	once     sync.Once
}

// New returns a new, empty Hub.
func New() *Hub { return &Hub{subs: make(map[*subscriber]struct{})} }

// Subscribe registers a new subscriber and returns a receive channel for
// events, an overflow channel, and a cleanup func. The buffer controls
// how many events can queue up before sends start dropping. The overflow
// channel is closed the first time an event is dropped for this
// subscriber; consumers should treat that as a signal to tear down and
// resync from a fresh snapshot.
func (h *Hub) Subscribe(buf int) (events <-chan any, overflow <-chan struct{}, unsubscribe func()) {
	sub := &subscriber{
		ch:       make(chan any, buf),
		overflow: make(chan struct{}),
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub.ch, sub.overflow, func() {
		h.mu.Lock()
		if _, ok := h.subs[sub]; ok {
			delete(h.subs, sub)
			close(sub.ch)
		}
		h.mu.Unlock()
	}
}

// Publish sends e to every subscriber. For any subscriber whose buffer is
// currently full, the event is dropped and its overflow channel is closed
// (exactly once). Never blocks.
func (h *Hub) Publish(e any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subs {
		select {
		case sub.ch <- e:
		default:
			sub.once.Do(func() { close(sub.overflow) })
		}
	}
}
