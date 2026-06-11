package db

import (
	"testing"

	"github.com/google/uuid"
)

func TestBatchRecentLogMessages(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "bob"})
	buf1, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	buf2, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#rust", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)

	for range 5 {
		InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf1, Sender: "alice", Kind: "privmsg", Content: "msg"})
	}
	for range 3 {
		InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf2, Sender: "bob", Kind: "privmsg", Content: "msg"})
	}

	got, err := BatchRecentLogMessages(ctx, logStore.DB, []uuid.UUID{buf1, buf2}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[buf1]) != 4 {
		t.Fatalf("#go: got %d messages, want 4", len(got[buf1]))
	}
	if len(got[buf2]) != 3 {
		t.Fatalf("#rust: got %d messages, want 3", len(got[buf2]))
	}
	// Messages must be in ascending order.
	msgs1 := got[buf1]
	for i := 1; i < len(msgs1); i++ {
		if msgs1[i].ID.String() < msgs1[i-1].ID.String() {
			t.Fatalf("#go messages not in ascending order at index %d", i)
		}
	}
}

func TestBatchRecentLogMessages_Empty(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()
	n, _ := ms.UpsertNetwork(ctx, Network{Name: "x", Host: "h", Port: 6667, Nick: "a"})
	logStore, _ := ms.LogStore(n.ID)
	got, err := BatchRecentLogMessages(ctx, logStore.DB, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("want nil for empty input, got %v", got)
	}
}

func TestBatchUnreadCandidates(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "bob"})
	buf1, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	buf2, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#rust", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)

	id1, _, _, _ := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf1, Sender: "alice", Kind: "privmsg", Content: "first"})
	InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf1, Sender: "alice", Kind: "privmsg", Content: "second"})
	InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf2, Sender: "alice", Kind: "privmsg", Content: "one"})
	InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf2, Sender: "alice", Kind: "join", Content: ""})

	// buf1 last-seen at id1 → only "second" unread; buf2 has no cutoff → all 2
	cutoffs := map[uuid.UUID]uuid.UUID{
		buf1: id1,
		buf2: uuid.Nil,
	}
	got, err := BatchUnreadCandidates(ctx, logStore.DB, cutoffs, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[buf1]) != 1 {
		t.Fatalf("#go: got %d candidates, want 1", len(got[buf1]))
	}
	if got[buf1][0].Content != "second" {
		t.Fatalf("#go first candidate content = %q, want %q", got[buf1][0].Content, "second")
	}
	if len(got[buf2]) != 2 {
		t.Fatalf("#rust: got %d candidates, want 2", len(got[buf2]))
	}
}

func TestBatchUnreadCandidates_PerBufferLimit(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "x", Host: "h", Port: 6667, Nick: "a"})
	buf1, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#a", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)

	for range 5 {
		InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf1, Sender: "alice", Kind: "privmsg", Content: "msg"})
	}
	got, err := BatchUnreadCandidates(ctx, logStore.DB, map[uuid.UUID]uuid.UUID{buf1: uuid.Nil}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[buf1]) != 3 {
		t.Fatalf("limit=3: got %d, want 3", len(got[buf1]))
	}
}

func TestMultiStoreBatchRecentMessages(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "bob"})
	buf1, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	buf2, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#rust", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)

	for range 3 {
		InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf1, Sender: "a", Kind: "privmsg", Content: "x"})
	}
	InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: buf2, Sender: "b", Kind: "privmsg", Content: "y"})

	got, err := ms.BatchRecentMessages(ctx, n.ID, []uuid.UUID{buf1, buf2}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[buf1]) != 3 {
		t.Fatalf("#go: want 3 StoredMessages, got %d", len(got[buf1]))
	}
	if len(got[buf2]) != 1 {
		t.Fatalf("#rust: want 1 StoredMessage, got %d", len(got[buf2]))
	}
	// NetworkID must be set correctly.
	for _, m := range got[buf1] {
		if m.NetworkID != n.ID {
			t.Fatalf("NetworkID = %s, want %s", m.NetworkID, n.ID)
		}
		if m.BufferID != buf1 {
			t.Fatalf("BufferID = %s, want %s", m.BufferID, buf1)
		}
	}
}
