package hub

import (
	"testing"
	"time"
)

// mustRecv tries to receive from ch within a short timeout. Returns the
// value and true if received, or nil and false on timeout.
func mustRecv(t *testing.T, ch <-chan any) (any, bool) {
	t.Helper()
	select {
	case v, ok := <-ch:
		return v, ok
	case <-time.After(100 * time.Millisecond):
		return nil, false
	}
}

// isClosed reports whether the overflow signal channel is closed. A closed
// channel yields an immediate zero-value receive; an open one blocks.
func isClosed(t *testing.T, ch <-chan struct{}) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(100 * time.Millisecond):
		return false
	}
}

func TestSubscribeReceivesPublishedEvent(t *testing.T) {
	h := New()
	ch, _, unsub := h.Subscribe(1)
	defer unsub()

	h.Publish("hello")

	v, ok := mustRecv(t, ch)
	if !ok {
		t.Fatal("expected to receive event, got timeout")
	}
	if v != "hello" {
		t.Fatalf("expected 'hello', got %q", v)
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	h := New()
	ch, _, unsub := h.Subscribe(1)
	unsub()

	_, ok := mustRecv(t, ch)
	if ok {
		t.Fatal("expected closed channel")
	}
}

func TestPublishDropsWhenSubscriberBufferFull(t *testing.T) {
	h := New()
	ch, _, unsub := h.Subscribe(1)
	defer unsub()

	// Fill the buffer without reading.
	h.Publish("first")
	h.Publish("second")

	// First event should be present.
	v, ok := mustRecv(t, ch)
	if !ok {
		t.Fatal("expected to receive first event, got timeout")
	}
	if v != "first" {
		t.Fatalf("expected 'first', got %q", v)
	}

	// Second event should have been dropped; no more events.
	_, ok = mustRecv(t, ch)
	if ok {
		t.Fatal("expected no more events (second should have been dropped)")
	}
}

func TestPublishWithNoSubscribersDoesNotPanic(t *testing.T) {
	h := New()
	// must not panic
	h.Publish("nobody")
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	h := New()
	ch, _, unsub := h.Subscribe(1)
	unsub()

	// Channel should be closed.
	_, ok := mustRecv(t, ch)
	if ok {
		t.Fatal("expected closed channel after first unsubscribe")
	}

	// Second unsubscribe must not panic.
	unsub()
}

func TestOverflowClosesWhenBufferFull(t *testing.T) {
	h := New()
	_, overflow, unsub := h.Subscribe(1)
	defer unsub()

	// Publish more events than the buffer can hold without draining.
	h.Publish("first")  // buffered
	h.Publish("second") // dropped → overflow fires

	if !isClosed(t, overflow) {
		t.Fatal("expected overflow channel to be closed after a dropped event")
	}
}

func TestOverflowClosesOnceAcrossRepeatedDrops(t *testing.T) {
	h := New()
	_, overflow, unsub := h.Subscribe(1)
	defer unsub()

	// Many drops in a row must not panic on a double-close.
	h.Publish("fill")
	for range 5 {
		h.Publish("drop")
	}

	if !isClosed(t, overflow) {
		t.Fatal("expected overflow channel to be closed")
	}
}

func TestOverflowStaysOpenForDrainedSubscriber(t *testing.T) {
	h := New()
	ch, overflow, unsub := h.Subscribe(1)
	defer unsub()

	// Publish and drain each event so the buffer never overflows.
	for range 5 {
		h.Publish("ok")
		if _, ok := mustRecv(t, ch); !ok {
			t.Fatal("expected to receive event")
		}
	}

	if isClosed(t, overflow) {
		t.Fatal("expected overflow channel to stay open for a drained subscriber")
	}
}

type seqEvent struct {
	Stamp
	Name string
}

func TestDoStampsEventsInOrderAndReplays(t *testing.T) {
	h := New()
	ch, _, unsub := h.Subscribe(8)
	defer unsub()
	base := h.Seq()

	a := &seqEvent{Name: "a"}
	b := &seqEvent{Name: "b"}
	if err := h.Do(func(publish func(any)) error {
		publish(a)
		publish(b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.Publish("unsequenced")
	c := &seqEvent{Name: "c"}
	h.Publish(c)

	if a.Seq != base+1 || b.Seq != base+2 || c.Seq != base+3 {
		t.Fatalf("seqs = %d %d %d, want %d..%d", a.Seq, b.Seq, c.Seq, base+1, base+3)
	}
	for _, want := range []any{a, b, "unsequenced", c} {
		got, ok := mustRecv(t, ch)
		if !ok || got != want {
			t.Fatalf("subscriber got %v, want %v", got, want)
		}
	}

	evs, ok := h.Replay(base + 1)
	if !ok || len(evs) != 2 || evs[0] != Event(b) || evs[1] != Event(c) {
		t.Fatalf("replay(base+1) = %v ok=%v, want [b c]", evs, ok)
	}
	if evs, ok := h.Replay(base + 3); !ok || len(evs) != 0 {
		t.Fatalf("replay at head = %v ok=%v, want empty ok", evs, ok)
	}
	if _, ok := h.Replay(base + 99); ok {
		t.Fatal("replay from the future must fail")
	}
	// since before the ring's reach: the ring holds a..c, so base-1 is unknown.
	if _, ok := h.Replay(base - 1); ok {
		t.Fatal("replay from before the ring must fail")
	}
	// since == first ring seq - 1 is still fully served.
	if evs, ok := h.Replay(base); !ok || len(evs) != 3 {
		t.Fatalf("replay(base) = %d events ok=%v, want 3", len(evs), ok)
	}
}

// Codex review: seq crosses the wire as a JSON number and the web client
// compares cursors as doubles; anything past 2^53 makes adjacent events
// compare equal and the gate drop them.
func TestSeqStaysWithinJavaScriptSafeInteger(t *testing.T) {
	h := New()
	e := &seqEvent{Name: "e"}
	h.Publish(e)
	if e.Seq > MaxSafeSeq {
		t.Fatalf("seq %d exceeds Number.MAX_SAFE_INTEGER %d", e.Seq, uint64(MaxSafeSeq))
	}
	// Headroom: a year of a million events per second still fits.
	if e.Seq+(365*24*3600)<<20 > MaxSafeSeq {
		t.Fatalf("seq %d leaves less than a year of headroom under 2^53", e.Seq)
	}
}

func TestSnapshotSeqMatchesPublishedEvents(t *testing.T) {
	h := New()
	e := &seqEvent{Name: "e"}
	h.Publish(e)
	seq, err := h.Snapshot(func() error { return nil })
	if err != nil || seq != e.Seq {
		t.Fatalf("snapshot seq = %d err=%v, want %d", seq, err, e.Seq)
	}
}

func TestNilHubRunsDoAndSnapshot(t *testing.T) {
	var h *Hub
	ran := false
	if err := h.Do(func(publish func(any)) error { publish("x"); ran = true; return nil }); err != nil || !ran {
		t.Fatal("nil hub Do did not run fn")
	}
	if seq, err := h.Snapshot(func() error { return nil }); err != nil || seq != 0 {
		t.Fatal("nil hub Snapshot")
	}
	h.Publish("ignored")
}
