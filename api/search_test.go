package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ircdb "github.com/lepinkainen/lurker/db"
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
	b1, _, _, _ := stores.EnsureBuffer(ctx, n1.ID, "#go", ircdb.BufferChannel)
	b2, _, _, _ := stores.EnsureBuffer(ctx, n2.ID, "#go", ircdb.BufferChannel)
	ls1, _ := stores.LogStore(n1.ID)
	ls2, _ := stores.LogStore(n2.ID)
	_, _, _, _ = ircdb.InsertLogMessage(ctx, ls1, ircdb.LogMessageInput{BufferID: b1, Sender: "alice", Kind: "privmsg", Content: "shared needle libera"})
	_, _, _, _ = ircdb.InsertLogMessage(ctx, ls2, ircdb.LogMessageInput{BufferID: b2, Sender: "bob", Kind: "privmsg", Content: "shared needle oftc"})

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
	r = httptest.NewRequest(http.MethodGet, "/api/search?q=needle&buffer="+b1.String(), http.NoBody)
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

func TestHistoryAroundReturnsContextWindow(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()
	n, _ := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	b, _, _, _ := stores.EnsureBuffer(ctx, n.ID, "#go", ircdb.BufferChannel)
	ls, _ := stores.LogStore(n.ID)
	var mid string
	for i := range 5 {
		id, _, _, _ := ircdb.InsertLogMessage(ctx, ls, ircdb.LogMessageInput{BufferID: b, Sender: "alice", Kind: "privmsg", Content: "m"})
		if i == 2 {
			mid = id.String()
		}
	}
	s := &Server{Stores: stores}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/buffers/"+b.String()+"/history?around="+mid+"&limit=2", http.NoBody)
	r.SetPathValue("id", b.String())
	s.history(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Messages) != 3 || resp.Messages[1].ID != mid {
		t.Fatalf("around window: %+v", resp.Messages)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/buffers/"+b.String()+"/history?around=nope", http.NoBody)
	r.SetPathValue("id", b.String())
	s.history(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad around: status=%d", w.Code)
	}
}
