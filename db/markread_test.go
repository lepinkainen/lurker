package db

import "testing"

func TestMultiStoreSearchAndMarkRead(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	globalID, _, _, _ := ms.UpsertBufferRegistry(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)
	localID, _, _, _ := UpsertLogBuffer(ctx, logStore.DB, n.ID, "#go", BufferChannel)
	id, _, _, _ := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: localID, Sender: "alice", Kind: "privmsg", Content: "searchable needle"})

	results, err := ms.Search(ctx, "needle", 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].BufferID != globalID {
		t.Fatalf("unexpected search results: %+v", results)
	}

	if err := ms.MarkBufferLastSeen(ctx, globalID, id); err != nil {
		t.Fatal(err)
	}
	var got int64
	if err := logStore.DB.QueryRow(`SELECT COALESCE(last_seen_id,0) FROM buffers WHERE id = ?`, localID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("last_seen_id=%d want %d", got, id)
	}
}
