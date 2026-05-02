package db

import (
	"bytes"
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

	if err := ms.MarkBufferLastSeen(ctx, globalID, id); err != nil {
		t.Fatal(err)
	}
	var got []byte
	if err := logStore.DB.QueryRow(`SELECT last_seen_id FROM buffers WHERE id = ?`, globalID[:]).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, id[:]) {
		t.Fatalf("last_seen_id=%x want %x", got, id[:])
	}
}
