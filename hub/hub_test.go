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

func TestSubscribeReceivesPublishedEvent(t *testing.T) {
	h := New()
	ch, unsub := h.Subscribe(1)
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
	ch, unsub := h.Subscribe(1)
	unsub()

	_, ok := mustRecv(t, ch)
	if ok {
		t.Fatal("expected closed channel")
	}
}

func TestPublishDropsWhenSubscriberBufferFull(t *testing.T) {
	h := New()
	ch, unsub := h.Subscribe(1)
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
	ch, unsub := h.Subscribe(1)
	unsub()

	// Channel should be closed.
	_, ok := mustRecv(t, ch)
	if ok {
		t.Fatal("expected closed channel after first unsubscribe")
	}

	// Second unsubscribe must not panic.
	unsub()
}
