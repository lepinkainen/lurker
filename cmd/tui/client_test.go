package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// coder/websocket defaults to a 32 KiB read limit, which a history page or a
// large channel's member_list exceeds. Verify the client reads well past it.
func TestConnectWSReadsLargeFrame(t *testing.T) {
	big := strings.Repeat("x", 512<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		_ = wsjson.Write(r.Context(), c, map[string]string{"type": "message", "content": big})
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := newAPIClient(srv.URL).connectWS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()

	ev, ok := <-startWSReader(ctx, conn)
	if !ok {
		t.Fatal("reader closed: frame over 32 KiB rejected")
	}
	if len(ev.Content) != len(big) {
		t.Fatalf("content len = %d, want %d", len(ev.Content), len(big))
	}
}
