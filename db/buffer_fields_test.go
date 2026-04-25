package db

import "testing"

func TestQueryBufferCreationAndBufferFields(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, err := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	logStore, _ := ms.LogStore(n.ID)

	_, _, _, err = ms.UpsertBufferRegistry(ctx, n.ID, "alice", BufferQuery)
	if err != nil {
		t.Fatal(err)
	}
	_, _, buf, err := UpsertLogBuffer(ctx, logStore.DB, n.ID, "alice", BufferQuery)
	if err != nil {
		t.Fatal(err)
	}
	if buf.Kind != BufferQuery {
		t.Fatalf("kind = %q, want %q", buf.Kind, BufferQuery)
	}

	_, _, _, err = ms.UpsertBufferRegistry(ctx, n.ID, "#go", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = UpsertLogBuffer(ctx, logStore.DB, n.ID, "#go", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	err = UpdateLogBufferTopic(ctx, logStore.DB, "#go", "new topic")
	if err != nil {
		t.Fatal(err)
	}
	err = UpdateLogBufferLastSeen(ctx, logStore.DB, "#go", 42)
	if err != nil {
		t.Fatal(err)
	}

	bufs, err := ms.ListAllBuffers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, b := range bufs {
		if b.Name == "#go" {
			found = true
			if b.Topic != "new topic" {
				t.Fatalf("topic = %q", b.Topic)
			}
			if b.LastSeenID != 42 {
				t.Fatalf("last_seen_id = %d", b.LastSeenID)
			}
		}
	}
	if !found {
		t.Fatal("did not find #go buffer")
	}
}

func TestMarkBufferLastSeenPersists(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	globalID, _, _, _ := ms.UpsertBufferRegistry(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)
	_, _, _, _ = UpsertLogBuffer(ctx, logStore.DB, n.ID, "#go", BufferChannel)
	if err := ms.MarkBufferLastSeen(ctx, globalID, 99); err != nil {
		t.Fatal(err)
	}

	var got int64
	if err := logStore.DB.QueryRow(`SELECT COALESCE(last_seen_id,0) FROM buffers WHERE name = '#go'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 99 {
		t.Fatalf("got %d, want 99", got)
	}
}
