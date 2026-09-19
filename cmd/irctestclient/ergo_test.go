//go:build ergo

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/girc"
)

// Exercise the same registered, persistent sender and HTTP handler used by
// the UI runner, observing messages only after Ergo delivers them to a peer.
func TestErgoMessageSender(t *testing.T) {
	addr := os.Getenv("ERGO_ADDR")
	if addr == "" {
		addr = "127.0.0.1:16667"
	}
	suffix := time.Now().UnixNano()
	channel := fmt.Sprintf("#sender-%d", suffix)
	senderNick := fmt.Sprintf("bob%d", suffix)
	receiver, _, err := connectSender(t.Context(), addr, channel, fmt.Sprintf("observer%d", suffix))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(receiver.Close)
	messages := make(chan girc.Event, 8)
	receiver.Handlers.Add(girc.PRIVMSG, func(_ *girc.Client, e girc.Event) { messages <- e })
	sender, _, err := connectSender(t.Context(), addr, channel, senderNick)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sender.Close)
	handler := messageHandler(func(message string) error { sender.Cmd.Message(channel, message); return nil })
	for _, content := range []string{"first message", "second message"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(content)))
		if response.Code != http.StatusAccepted {
			t.Fatalf("send status = %d", response.Code)
		}
		select {
		case event := <-messages:
			if event.Source == nil || event.Source.Name != senderNick || event.Last() != content || event.Params[0] != channel {
				t.Fatalf("unexpected IRC event: %+v", event)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Ergo did not deliver %q", content)
		}
	}
}
