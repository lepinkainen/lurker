package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

func newHistoryTestServer(t *testing.T) (*ircdb.MultiStore, *Server, ircdb.Network, uuid.UUID) {
	t.Helper()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })

	ctx := t.Context()
	n, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	bufID, _, _, err := stores.EnsureBuffer(ctx, n.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	return stores, &Server{Stores: stores}, n, bufID
}

func insertHistoryMessages(t *testing.T, stores *ircdb.MultiStore, networkID, bufID uuid.UUID, count int) []uuid.UUID {
	t.Helper()
	ls, err := stores.LogStore(networkID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 0, count)
	for range count {
		id, _, _, err := ircdb.InsertLogMessage(t.Context(), ls.DB, ircdb.LogMessageInput{
			BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "hi",
		})
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestTallyUnreadSkipsSelfAndAnchorsMarker(t *testing.T) {
	id1 := uuid.Must(uuid.NewV7())
	id2 := uuid.Must(uuid.NewV7())
	id3 := uuid.Must(uuid.NewV7())
	id4 := uuid.Must(uuid.NewV7())
	cands := []ircdb.UnreadCandidate{
		{ID: id1, Kind: "privmsg", Sender: "Bob", Content: "my own message"},
		{ID: id2, Kind: "join", Sender: "alice", Content: ""},
		{ID: id3, Kind: "privmsg", Sender: "alice", Content: "hi bob"},
		{ID: id4, Kind: "privmsg", Sender: "alice", Content: "plain"},
	}
	got := tallyUnread(cands, "bob", nil)
	if got.Unread != 2 {
		t.Fatalf("unread = %d, want 2 (self message and join excluded)", got.Unread)
	}
	if got.Mentions != 1 {
		t.Fatalf("mentions = %d, want 1", got.Mentions)
	}
	// Marker anchors at the first message that counts: not the self message,
	// not the join, but the first message from someone else.
	if got.MarkerID != id3 {
		t.Fatalf("marker = %v, want %v", got.MarkerID, id3)
	}
}

func TestTallyUnreadEmptyAndUnknownNick(t *testing.T) {
	if got := tallyUnread(nil, "bob", nil); got != (unreadTally{}) {
		t.Fatalf("empty tally = %+v, want zero", got)
	}
	// Unknown nick: self-detection degrades to counting everything.
	id := uuid.Must(uuid.NewV7())
	got := tallyUnread([]ircdb.UnreadCandidate{{ID: id, Kind: "privmsg", Sender: "Bob", Content: "x"}}, "", nil)
	if got.Unread != 1 || got.MarkerID != id {
		t.Fatalf("tally = %+v, want unread=1 marker=%v", got, id)
	}
}

func TestTallyUnreadMutedSuppressesUnreadButKeepsMentions(t *testing.T) {
	id1 := uuid.Must(uuid.NewV7())
	id2 := uuid.Must(uuid.NewV7())
	cands := []ircdb.UnreadCandidate{
		{ID: id1, Kind: "privmsg", Sender: "weatherbot", Content: "sunny today"},
		{ID: id2, Kind: "privmsg", Sender: "weatherbot", Content: "hey bob, sunny today"},
	}
	muted := func(sender string) bool { return sender == "weatherbot" }

	got := tallyUnread(cands, "bob", muted)
	if got.Unread != 0 {
		t.Fatalf("unread = %d, want 0 (both messages from a muted sender)", got.Unread)
	}
	if got.Mentions != 1 {
		t.Fatalf("mentions = %d, want 1 (mention survives mute)", got.Mentions)
	}
	if got.MarkerID != uuid.Nil {
		t.Fatalf("marker = %v, want Nil (muted sender never anchors the marker)", got.MarkerID)
	}
}

func TestMarkerTS(t *testing.T) {
	if got := markerTS(uuid.Nil); got != "" {
		t.Fatalf("markerTS(Nil) = %q, want empty", got)
	}
	if got := markerTS(uuid.Must(uuid.NewRandom())); got != "" {
		t.Fatalf("markerTS(v4) = %q, want empty", got)
	}
	if got := markerTS(uuid.Must(uuid.NewV7())); got == "" {
		t.Fatal("markerTS(v7) empty, want RFC3339 timestamp")
	}
}

func TestHistoryEndpointReturnsMessagesForBuffer(t *testing.T) {
	stores, s, n, bufID := newHistoryTestServer(t)
	ids := insertHistoryMessages(t, stores, n.ID, bufID, 3)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/buffers/"+bufID.String()+"/history", http.NoBody)
	r.SetPathValue("id", bufID.String())
	s.history(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		BufferID uuid.UUID    `json:"buffer_id"`
		Messages []messageDTO `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.BufferID != bufID {
		t.Fatalf("buffer_id = %s, want %s", resp.BufferID, bufID)
	}
	if len(resp.Messages) != 3 {
		t.Fatalf("messages len = %d want 3", len(resp.Messages))
	}
	gotIDs := map[uuid.UUID]bool{}
	for _, m := range resp.Messages {
		gotIDs[m.ID] = true
	}
	for _, id := range ids {
		if !gotIDs[id] {
			t.Fatalf("missing inserted id %s in response", id)
		}
	}
}

func TestHistoryEndpointRejectsBadBufferID(t *testing.T) {
	_, s, _, _ := newHistoryTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/buffers/not-a-uuid/history", http.NoBody)
	r.SetPathValue("id", "not-a-uuid")
	s.history(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestLoadHistoryPaginatesWithBeforeCursor(t *testing.T) {
	stores, s, n, bufID := newHistoryTestServer(t)
	ids := insertHistoryMessages(t, stores, n.ID, bufID, 5)
	// ids are time-ordered ascending. Use the third id as the `before` cursor;
	// the response should contain only ids strictly older than it.
	cursor := ids[2]

	msgs, err := s.loadHistory(t.Context(), bufID, cursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one older message before cursor")
	}
	for _, m := range msgs {
		if m.ID.String() >= cursor.String() {
			t.Fatalf("message %s not strictly before cursor %s", m.ID, cursor)
		}
	}
}

func TestLoadHistoryRecentWhenBeforeIsNil(t *testing.T) {
	stores, s, n, bufID := newHistoryTestServer(t)
	insertHistoryMessages(t, stores, n.ID, bufID, 2)

	msgs, err := s.loadHistory(t.Context(), bufID, uuid.Nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("recent messages len = %d want 2", len(msgs))
	}
}

func TestChannelMembersWireShape(t *testing.T) {
	members := []irc.ChannelUser{
		{Nick: "alice", Prefix: "@", Self: true},
		{Nick: "bob"},
	}
	if len(members) != 2 {
		t.Fatalf("len = %d want 2", len(members))
	}
	if members[0].Nick != "alice" || members[0].Prefix != "@" || !members[0].Self {
		t.Fatalf("alice = %+v", members[0])
	}
	if members[1].Nick != "bob" || members[1].Prefix != "" || members[1].Self || members[1].Away {
		t.Fatalf("bob = %+v", members[1])
	}
}

func TestAttachPreviewsNoopWhenInputEmpty(t *testing.T) {
	_, s, _, _ := newHistoryTestServer(t)
	// Should not panic on nil or empty slice even though Stores.Previews exists.
	s.attachPreviews(t.Context(), nil)
	s.attachPreviews(t.Context(), []messageDTO{})
}

func TestAttachPreviewsLeavesMessagesUntouchedWithoutLinks(t *testing.T) {
	stores, s, n, bufID := newHistoryTestServer(t)
	ids := insertHistoryMessages(t, stores, n.ID, bufID, 1)

	dto := messageDTO{MessageCore: irc.MessageCore{ID: ids[0], NetworkID: n.ID, BufferID: bufID}}
	msgs := []messageDTO{dto}
	s.attachPreviews(t.Context(), msgs)
	if len(msgs[0].Previews) != 0 {
		t.Fatalf("previews populated unexpectedly: %+v", msgs[0].Previews)
	}
}

func TestAttachPreviewsResolvesCachedDisplayablePreview(t *testing.T) {
	stores, s, n, bufID := newHistoryTestServer(t)
	ids := insertHistoryMessages(t, stores, n.ID, bufID, 1)
	url := "https://example.test/p.png"

	if err := stores.Previews.Put(t.Context(), ircdb.URLPreview{URL: url, Kind: ircdb.PreviewKindImage, Mime: "image/png"}); err != nil {
		t.Fatal(err)
	}
	ls, _ := stores.LogStore(n.ID)
	if err := ircdb.InsertMessagePreviewLinks(t.Context(), ls.DB, []ircdb.MessagePreviewLink{
		{MessageID: ids[0], URL: url, Position: 0},
	}); err != nil {
		t.Fatal(err)
	}

	msgs := []messageDTO{{MessageCore: irc.MessageCore{ID: ids[0], NetworkID: n.ID, BufferID: bufID}}}
	s.attachPreviews(t.Context(), msgs)
	if len(msgs[0].Previews) != 1 {
		t.Fatalf("previews len = %d want 1: %+v", len(msgs[0].Previews), msgs[0].Previews)
	}
	if msgs[0].Previews[0].Kind != ircdb.PreviewKindImage {
		t.Fatalf("preview kind = %q want image", msgs[0].Previews[0].Kind)
	}
}

func TestAttachPreviewsSkipsNonDisplayableKinds(t *testing.T) {
	stores, s, n, bufID := newHistoryTestServer(t)
	ids := insertHistoryMessages(t, stores, n.ID, bufID, 1)
	url := "https://example.test/error"

	if err := stores.Previews.Put(t.Context(), ircdb.URLPreview{URL: url, Kind: ircdb.PreviewKindError, Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	ls, _ := stores.LogStore(n.ID)
	if err := ircdb.InsertMessagePreviewLinks(t.Context(), ls.DB, []ircdb.MessagePreviewLink{
		{MessageID: ids[0], URL: url, Position: 0},
	}); err != nil {
		t.Fatal(err)
	}

	msgs := []messageDTO{{MessageCore: irc.MessageCore{ID: ids[0], NetworkID: n.ID, BufferID: bufID}}}
	s.attachPreviews(t.Context(), msgs)
	if len(msgs[0].Previews) != 0 {
		t.Fatalf("expected error-kind to be filtered: %+v", msgs[0].Previews)
	}
}
