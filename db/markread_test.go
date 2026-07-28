package db

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMultiStoreSearchAndMarkRead(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	globalID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)
	id, _, _, _ := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: globalID, Sender: "alice", Kind: "privmsg", Content: "searchable needle"})

	results, err := ms.Search(ctx, "needle", uuid.Nil, uuid.Nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].BufferID != globalID {
		t.Fatalf("unexpected search results: %+v", results)
	}

	effective, err := ms.MarkBufferLastSeen(ctx, globalID, id)
	if err != nil {
		t.Fatal(err)
	}
	if effective != id {
		t.Fatalf("effective = %v, want %v", effective, id)
	}
	var got []byte
	if err := logStore.DB.QueryRow(`SELECT last_seen_id FROM buffers WHERE id = ?`, globalID[:]).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, id[:]) {
		t.Fatalf("last_seen_id=%x want %x", got, id[:])
	}
}

func TestMarkBufferLastSeenRejectsUnknownMessage(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	globalID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)
	real, _, _, _ := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: globalID, Sender: "alice", Kind: "privmsg", Content: "hi"})

	// Fabricated id: must be rejected, last_seen untouched.
	if _, err := ms.MarkBufferLastSeen(ctx, globalID, uuid.Must(uuid.NewV7())); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("err = %v, want ErrMessageNotFound", err)
	}
	var got []byte
	if err := logStore.DB.QueryRow(`SELECT last_seen_id FROM buffers WHERE id = ?`, globalID[:]).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("last_seen_id = %x, want NULL after rejected mark", got)
	}

	// A message in a *different* buffer of the same network must also be rejected.
	otherID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#other", BufferChannel)
	foreign, _, _, _ := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: otherID, Sender: "bob", Kind: "privmsg", Content: "elsewhere"})
	if _, err := ms.MarkBufferLastSeen(ctx, globalID, foreign); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("cross-buffer err = %v, want ErrMessageNotFound", err)
	}

	// Sanity: the real message still works.
	if _, err := ms.MarkBufferLastSeen(ctx, globalID, real); err != nil {
		t.Fatal(err)
	}
}

func TestMarkBufferLastSeenIsMonotonic(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	globalID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)
	older, _, _, _ := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: globalID, Sender: "alice", Kind: "privmsg", Content: "one"})
	newer, _, _, _ := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: globalID, Sender: "alice", Kind: "privmsg", Content: "two"})

	if _, err := ms.MarkBufferLastSeen(ctx, globalID, newer); err != nil {
		t.Fatal(err)
	}
	// Stale ack (racing client with a shorter history window): no error,
	// no regression — the effective position is the newer id.
	effective, err := ms.MarkBufferLastSeen(ctx, globalID, older)
	if err != nil {
		t.Fatal(err)
	}
	if effective != newer {
		t.Fatalf("effective = %v, want %v (max-wins)", effective, newer)
	}
	var got []byte
	if err := logStore.DB.QueryRow(`SELECT last_seen_id FROM buffers WHERE id = ?`, globalID[:]).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newer[:]) {
		t.Fatalf("last_seen_id = %x, want %x (must not regress)", got, newer[:])
	}
}
