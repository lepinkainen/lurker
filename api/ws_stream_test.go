package api

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket/wsjson"
)

// Hub overflow must tear the connection down, not just stop the writer.
// Before the fix the reader stayed blocked on the socket and the client saw
// a silent, event-less connection forever.
func TestStreamWriterOverflowCancelsCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	overflow := make(chan struct{})
	close(overflow)
	done := make(chan struct{})
	go runStreamWriter(ctx, nil, cancel, make(chan any), overflow, done)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("ctx not cancelled after hub overflow")
	}
	<-done
}

// The server heartbeat is what keeps an idle web client from declaring the
// socket dead at 60s; verify it actually arrives on a quiet connection.
func TestStreamSendsPingHeartbeat(t *testing.T) {
	old := wsPingInterval
	wsPingInterval = 20 * time.Millisecond
	t.Cleanup(func() { wsPingInterval = old })

	ts := newTestWSServer(t)
	c := ts.dial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var ev struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, c, &ev); err != nil {
		t.Fatalf("read: %v", err)
	}
	if ev.Type != "ping" {
		t.Fatalf("first event on idle stream = %q, want ping", ev.Type)
	}
}
