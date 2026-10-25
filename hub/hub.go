// Package hub is the in-process event stream between the persistence path
// and API consumers (primarily WebSocket clients). It fans events out to any
// number of subscribers and, for events that carry a Stamp, assigns each a
// position in one total order so clients can resume exactly where they left
// off (the same shape as Discord's gateway sequence or a Matrix sync token).
package hub

import (
	"sync"
	"time"
)

// Stamp is embedded by every event type that takes part in the ordered
// stream. Publish fills Seq; it is the client's resume cursor.
type Stamp struct {
	Seq uint64 `json:"seq"`
}

// SetSeq records the event's position in the stream.
func (s *Stamp) SetSeq(n uint64) { s.Seq = n }

// SeqNo returns the event's position in the stream (0 = not yet published).
func (s *Stamp) SeqNo() uint64 { return s.Seq }

// Event is a sequenced stream event: any pointer to a struct embedding Stamp.
type Event interface {
	SetSeq(uint64)
	SeqNo() uint64
}

// RingSize bounds how many sequenced events are kept for Replay. A client
// whose cursor fell further behind takes a full snapshot instead.
const RingSize = 4096

// Hub fans out Publish calls to every live subscriber. Sends are
// non-blocking per subscriber — if a subscriber's buffer is full the
// event is dropped for that subscriber and its overflow channel is
// closed. A dropped event means the subscriber has fallen behind and
// can no longer be trusted to hold a consistent view; the contract is
// that the consumer watches its overflow channel, tears down its
// connection when it fires, and reconnects (resuming from its cursor).
//
// Stream order: every Publish, Do and Snapshot runs under one lock, so the
// sequence numbers it hands out are also the order subscribers see events
// in, and a Snapshot sees exactly the writes of the events numbered at or
// below the sequence it returns. Writers that publish an event describing a
// DB write must do the write inside Do so no snapshot can observe the write
// without the event or the event without the write.
//
// ponytail: one global lock for every write + publish in the process. A
// single-user bouncer commits a few rows per second; revisit with per-network
// streams if SQLite write latency under the lock ever shows up.
type Hub struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}

	streamMu sync.Mutex
	seq      uint64
	ring     []Event
}

// subscriber holds one consumer's event channel plus a one-shot overflow
// signal. overflow is closed exactly once (guarded by once) the first
// time a Publish has to drop an event for this subscriber.
type subscriber struct {
	ch       chan any
	overflow chan struct{}
	once     sync.Once
}

// MaxSafeSeq is JavaScript's Number.MAX_SAFE_INTEGER (2^53-1). Sequence
// numbers cross the wire as JSON numbers and the web client compares them
// as doubles, so they must never exceed it (seqTooLargeForJS guards tests).
const MaxSafeSeq = 1<<53 - 1

// New returns a new, empty Hub. The sequence starts at a wall-clock-derived
// value (seconds << 20: a million events per second of headroom, safe for
// JavaScript until about the year 2240) so a cursor saved against a previous
// process is never valid against this one: it is either ahead of our
// sequence or behind the ring, and both resolve to a full snapshot in
// Replay.
// ponytail: relies on the clock not stepping backwards across restarts; a
// persisted epoch id would close that gap if it ever matters.
func New() *Hub {
	return &Hub{
		subs: make(map[*subscriber]struct{}),
		seq:  uint64(max(time.Now().Unix(), 0)) << 20, // non-negative: gosec G115
	}
}

// Subscribe registers a new subscriber and returns a receive channel for
// events, an overflow channel, and a cleanup func. The buffer controls
// how many events can queue up before sends start dropping. The overflow
// channel is closed the first time an event is dropped for this
// subscriber; consumers should treat that as a signal to tear down and
// resync.
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

// Publish sends e to every subscriber, stamping it with the next sequence
// number when it is an Event. Never blocks. Nil-safe (no-op on a nil Hub).
func (h *Hub) Publish(e any) {
	_ = h.Do(func(publish func(any)) error {
		publish(e)
		return nil
	})
}

// Do runs fn under the stream lock. fn does its writes and calls publish for
// each event describing them; publish stamps and fans out immediately, so
// the writes and their events are one atomic step relative to every other
// Do, Publish and Snapshot. fn must not call back into the Hub (the lock is
// not reentrant). Nil-safe: on a nil Hub fn runs with a no-op publisher.
func (h *Hub) Do(fn func(publish func(any)) error) error {
	if h == nil {
		return fn(func(any) {})
	}
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	return fn(h.publishLocked)
}

// Snapshot runs fn under the stream lock and returns the sequence number the
// resulting read reflects: every event at or below it is already in what fn
// read, and none above it is. Nil-safe (fn runs, seq 0).
func (h *Hub) Snapshot(fn func() error) (uint64, error) {
	if h == nil {
		return 0, fn()
	}
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	return h.seq, fn()
}

// Seq returns the sequence number of the last published Event.
func (h *Hub) Seq() uint64 {
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	return h.seq
}

// Replay returns the sequenced events after since, in order, when the ring
// still holds every one of them. ok is false when since is unknown (ahead of
// the stream, or from another process) or older than the ring reaches; the
// caller then falls back to a full snapshot.
func (h *Hub) Replay(since uint64) (events []Event, ok bool) {
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	switch {
	case since > h.seq:
		return nil, false
	case since == h.seq:
		return nil, true
	case len(h.ring) == 0 || since < h.ring[0].SeqNo()-1:
		return nil, false
	}
	for i, ev := range h.ring {
		if ev.SeqNo() > since {
			return append([]Event(nil), h.ring[i:]...), true
		}
	}
	return nil, true
}

func (h *Hub) publishLocked(e any) {
	if ev, ok := e.(Event); ok {
		h.seq++
		ev.SetSeq(h.seq)
		h.ring = append(h.ring, ev)
		if len(h.ring) > RingSize {
			h.ring = h.ring[len(h.ring)-RingSize:]
		}
	}
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
