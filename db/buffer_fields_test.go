package db

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

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

	_, _, buf, err := ms.EnsureBuffer(ctx, n.ID, "alice", BufferQuery)
	if err != nil {
		t.Fatal(err)
	}
	if buf.Kind != BufferQuery {
		t.Fatalf("kind = %q, want %q", buf.Kind, BufferQuery)
	}

	_, _, _, err = ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	err = UpdateLogBufferTopic(ctx, logStore.DB, "#go", "new topic")
	if err != nil {
		t.Fatal(err)
	}
	lastSeen := uuid.Must(uuid.NewV7())
	err = UpdateLogBufferLastSeen(ctx, logStore.DB, "#go", lastSeen)
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
			if !bytes.Equal(b.LastSeenID[:], lastSeen[:]) {
				t.Fatalf("last_seen_id = %s, want %s", b.LastSeenID, lastSeen)
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
	globalID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)
	want := uuid.Must(uuid.NewV7())
	if err := ms.MarkBufferLastSeen(ctx, globalID, want); err != nil {
		t.Fatal(err)
	}

	var got []byte
	if err := logStore.DB.QueryRow(`SELECT last_seen_id FROM buffers WHERE name = '#go'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want[:]) {
		t.Fatalf("got %x, want %x", got, want[:])
	}
}
