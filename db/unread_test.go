package db

import (
	"testing"

	"github.com/google/uuid"
)

func TestUnreadCandidates(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "bob"})
	bufID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)

	// Five messages: privmsg (mention), privmsg, join (sys), privmsg, action.
	id1, _, _, _ := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "hi bob"})
	_, _, _, _ = InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "second"})
	_, _, _, _ = InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: bufID, Sender: "carol", Kind: "join", Content: ""})
	_, _, _, _ = InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "third"})
	_, _, _, _ = InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: bufID, Sender: "alice", Kind: "action", Content: "waves at bob"})

	// All candidates from beginning
	all, err := UnreadCandidates(ctx, logStore.DB, bufID, uuid.Nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("got %d candidates, want 5", len(all))
	}

	// After marking the first as last-seen, should get 4 (excluding id1).
	after, err := UnreadCandidates(ctx, logStore.DB, bufID, id1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 4 {
		t.Fatalf("got %d candidates after lastSeen, want 4", len(after))
	}
	if after[0].Content != "second" {
		t.Fatalf("first after lastSeen = %q, want %q", after[0].Content, "second")
	}

	// Limit truncates from the head (ASC order).
	limited, err := UnreadCandidates(ctx, logStore.DB, bufID, uuid.Nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Fatalf("limit=2 returned %d", len(limited))
	}
}
