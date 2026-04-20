package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	ircdb "github.com/lepinkainen/research/irc-service/db"
)

func TestSearchRoutesAcrossNetworksAndBuffers(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	n1, _ := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	n2, _ := stores.UpsertNetwork(ctx, ircdb.Network{Name: "OFTC", Host: "irc.oftc.net", Port: 6697, TLS: true, Nick: "b"})
	b1, _, _, _ := stores.UpsertBufferRegistry(ctx, n1.ID, "#go", ircdb.BufferChannel)
	_, _, _, _ = stores.UpsertBufferRegistry(ctx, n2.ID, "#go", ircdb.BufferChannel)
	ls1, _ := stores.LogStore(n1.ID)
	ls2, _ := stores.LogStore(n2.ID)
	lb1, _, _, _ := ircdb.UpsertLogBuffer(ctx, ls1.DB, n1.ID, "#go", ircdb.BufferChannel)
	lb2, _, _, _ := ircdb.UpsertLogBuffer(ctx, ls2.DB, n2.ID, "#go", ircdb.BufferChannel)
	_, _, _, _ = ircdb.InsertLogMessage(ctx, ls1.DB, ircdb.LogMessageInput{BufferID: lb1, Sender: "alice", Kind: "privmsg", Content: "shared needle libera"})
	_, _, _, _ = ircdb.InsertLogMessage(ctx, ls2.DB, ircdb.LogMessageInput{BufferID: lb2, Sender: "bob", Kind: "privmsg", Content: "shared needle oftc"})

	s := &Server{Stores: stores}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/search?q=needle", http.NoBody)
	s.search(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var all map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	results := all["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("len(results)=%d want 2", len(results))
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/search?q=needle&buffer="+itoa(b1), http.NoBody)
	s.search(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	results = all["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("len(buffer results)=%d want 1", len(results))
	}
}

func itoa(v int64) string {
	return fmt.Sprintf("%d", v)
}
