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
