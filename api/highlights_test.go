package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
	"github.com/lepinkainen/lurker/irc"
)

func newHighlightsTestServer(t *testing.T) (*hub.Hub, http.Handler) {
	t.Helper()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	t.Cleanup(func() { irc.SetHighlightPatterns(nil) })

	evHub := hub.New()
	srv := &Server{Stores: stores, Hub: evHub}
	return evHub, srv.Handler()
}

func getHighlightPatterns(t *testing.T, h http.Handler) []string {
	t.Helper()
	rec := doNetworkRequest(h, http.MethodGet, "/api/settings/highlights", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp highlightsRequest
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp.Patterns
}

func TestHighlightsRoundTrip(t *testing.T) {
	_, h := newHighlightsTestServer(t)

	if got := getHighlightPatterns(t, h); len(got) != 0 {
		t.Fatalf("expected empty initial list, got %v", got)
	}

	rec := doNetworkRequest(h, http.MethodPut, "/api/settings/highlights", `{"patterns":["deploy","lurker"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", rec.Code, rec.Body.String())
	}

	got := getHighlightPatterns(t, h)
	if len(got) != 2 || got[0] != "deploy" || got[1] != "lurker" {
		t.Fatalf("round-trip mismatch: %v", got)
	}
}

func TestHighlightsPutReplaces(t *testing.T) {
	_, h := newHighlightsTestServer(t)

	doNetworkRequest(h, http.MethodPut, "/api/settings/highlights", `{"patterns":["old"]}`)
	rec := doNetworkRequest(h, http.MethodPut, "/api/settings/highlights", `{"patterns":["new"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := getHighlightPatterns(t, h)
	if len(got) != 1 || got[0] != "new" {
		t.Fatalf("expected replace semantics, got %v", got)
	}
}

func TestHighlightsValidation(t *testing.T) {
	_, h := newHighlightsTestServer(t)

	long := strings.Repeat("x", maxHighlightPatternLength+1)
	rec := doNetworkRequest(h, http.MethodPut, "/api/settings/highlights", `{"patterns":["`+long+`"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize pattern: status = %d, want 400", rec.Code)
	}

	rec = doNetworkRequest(h, http.MethodPut, "/api/settings/highlights", `{"patterns":["  ","Word","word"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := getHighlightPatterns(t, h)
	if len(got) != 1 || got[0] != "Word" {
		t.Fatalf("expected blanks dropped and case-insensitive dedupe, got %v", got)
	}
}

func TestUnreadCountsIncludeHighlights(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	t.Cleanup(func() { irc.SetHighlightPatterns(nil) })
	irc.SetHighlightPatterns([]string{"deploy"})

	n, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	bufID, _, _, err := stores.EnsureBuffer(ctx, n.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	// One nick mention, one highlight-pattern match, one plain message.
	for _, content := range []string{"hi bob", "deploy is done", "nothing special"} {
		if _, _, _, err := ircdb.InsertLogMessage(ctx, logStore.DB, ircdb.LogMessageInput{BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: content}); err != nil {
			t.Fatal(err)
		}
	}

	srv := &Server{Stores: stores}
	tally := srv.computeUnreadCounts(ctx, n.ID, bufID, uuid.Nil, "bob")
	if tally.Unread != 3 {
		t.Fatalf("unread = %d, want 3", tally.Unread)
	}
	if tally.Mentions != 2 {
		t.Fatalf("mentions = %d, want 2 (nick mention + highlight)", tally.Mentions)
	}
	if tally.MarkerID == uuid.Nil {
		t.Fatal("marker id not set, want id of first unread message")
	}
}

func TestHighlightsPutPublishesEventAndUpdatesMatcher(t *testing.T) {
	evHub, h := newHighlightsTestServer(t)
	events, _, unsub := evHub.Subscribe(1)
	defer unsub()

	rec := doNetworkRequest(h, http.MethodPut, "/api/settings/highlights", `{"patterns":["deploy"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", rec.Code, rec.Body.String())
	}

	select {
	case ev := <-events:
		he, ok := ev.(highlightsEvent)
		if !ok || he.Type != "highlights" || len(he.Patterns) != 1 || he.Patterns[0] != "deploy" {
			t.Fatalf("unexpected hub event: %#v", ev)
		}
	default:
		t.Fatal("expected highlights hub event")
	}

	sem := irc.ComputeMessageSemantics("privmsg", "alice", "deploy finished", "", "me")
	if !sem.Highlight || sem.HighlightPattern != "deploy" {
		t.Fatalf("live matcher not updated after PUT: %+v", sem)
	}
}
