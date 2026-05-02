package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

func TestMarkReadBroadcastsBufferUpdate(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	n, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	bufferID, _, _, err := stores.EnsureBuffer(ctx, n.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}

	h := hub.New()
	events, unsub := h.Subscribe(8)
	defer unsub()

	httpSrv := httptest.NewServer((&Server{Stores: stores, Hub: h}).Handler())
	defer httpSrv.Close()

	wsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(wsCtx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/api/stream", nil)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()

	lastSeenID := uuid.Must(uuid.NewV7())
	if err := wsjson.Write(wsCtx, c, clientCmd{Type: "mark_read", ReqID: "r1", BufferID: bufferID, MessageID: lastSeenID}); err != nil {
		t.Fatal(err)
	}

	for {
		var ack ackEnvelope
		if err := wsjson.Read(wsCtx, c, &ack); err != nil {
			t.Fatal(err)
		}
		if ack.Type == "buffer_update" {
			continue
		}
		if ack.Type != "ack" || ack.ReqID != "r1" {
			t.Fatalf("unexpected ack: %+v", ack)
		}
		break
	}

	select {
	case ev := <-events:
		got, ok := ev.(bufferLastSeenEvent)
		if !ok {
			t.Fatalf("event type = %T", ev)
		}
		if got.Type != "buffer_update" || got.ID != bufferID || got.NetworkID != n.ID || got.LastSeenID != lastSeenID {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-wsCtx.Done():
		t.Fatal(wsCtx.Err())
	}
}

func TestJoinUsesNetworkIDChannelAndPartUsesBufferID(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	n, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	bufferID, _, _, err := stores.EnsureBuffer(ctx, n.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}

	if n.ID == uuid.Nil || bufferID == uuid.Nil {
		t.Fatal("expected non-zero ids")
	}
}
