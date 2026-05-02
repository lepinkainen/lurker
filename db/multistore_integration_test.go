package db

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestBufferRegistryStableAllocationAndRouting(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n1, err := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	n2, err := ms.UpsertNetwork(ctx, Network{Name: "OFTC", Host: "irc.oftc.net", Port: 6697, TLS: true, Nick: "b"})
	if err != nil {
		t.Fatal(err)
	}

	b1, _, _, err := ms.EnsureBuffer(ctx, n1.ID, "#go", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	b1again, _, _, err := ms.EnsureBuffer(ctx, n1.ID, "#go", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	b2, _, _, err := ms.EnsureBuffer(ctx, n2.ID, "#go", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}

	if b1 != b1again {
		t.Fatalf("buffer id changed: %s vs %s", b1, b1again)
	}
	if b1 == b2 {
		t.Fatal("expected globally unique buffer ids across networks")
	}

	networkID, name, kind, err := ms.LookupBuffer(ctx, b1)
	if err != nil {
		t.Fatal(err)
	}
	if networkID != n1.ID || name != "#go" || kind != BufferChannel {
		t.Fatalf("unexpected routing: network=%s name=%q kind=%q", networkID, name, kind)
	}
}

func TestHistoryRoutingByGlobalBufferID(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n1, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	n2, _ := ms.UpsertNetwork(ctx, Network{Name: "OFTC", Host: "irc.oftc.net", Port: 6697, TLS: true, Nick: "b"})

	global1, _, _, _ := ms.EnsureBuffer(ctx, n1.ID, "#go", BufferChannel)
	global2, _, _, _ := ms.EnsureBuffer(ctx, n2.ID, "#go", BufferChannel)
	log1, _ := ms.LogStore(n1.ID)
	log2, _ := ms.LogStore(n2.ID)
	_, _, _, _ = InsertLogMessage(ctx, log1.DB, LogMessageInput{BufferID: global1, Sender: "alice", Kind: "privmsg", Content: "hello libera"})
	_, _, _, _ = InsertLogMessage(ctx, log2.DB, LogMessageInput{BufferID: global2, Sender: "bob", Kind: "privmsg", Content: "hello oftc"})

	msgs1, err := ms.RecentMessages(ctx, global1, 50)
	if err != nil {
		t.Fatal(err)
	}
	msgs2, err := ms.RecentMessages(ctx, global2, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(msgs1) != 1 || msgs1[0].Content != "hello libera" {
		t.Fatalf("unexpected msgs1: %+v", msgs1)
	}
	if len(msgs2) != 1 || msgs2[0].Content != "hello oftc" {
		t.Fatalf("unexpected msgs2: %+v", msgs2)
	}
}

func TestWritesStayInPerNetworkDB(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n1, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	n2, _ := ms.UpsertNetwork(ctx, Network{Name: "OFTC", Host: "irc.oftc.net", Port: 6697, TLS: true, Nick: "b"})
	log1, _ := ms.LogStore(n1.ID)
	log2, _ := ms.LogStore(n2.ID)
	local1, _, _, _ := ms.EnsureBuffer(ctx, n1.ID, "#go", BufferChannel)
	_, _, _, _ = InsertLogMessage(ctx, log1.DB, LogMessageInput{BufferID: local1, Sender: "alice", Kind: "privmsg", Content: "only in libera"})

	var count1, count2 int
	if err := log1.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count1); err != nil {
		t.Fatal(err)
	}
	if err := log2.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count2); err != nil {
		t.Fatal(err)
	}
	if count1 != 1 || count2 != 0 {
		t.Fatalf("unexpected per-network counts: log1=%d log2=%d", count1, count2)
	}
}

func TestEnsureBufferRejectsRegistryLogIDMismatch(t *testing.T) {
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
	logStore, err := ms.LogStore(n.ID)
	if err != nil {
		t.Fatal(err)
	}

	registryID := uuid.Must(uuid.NewV7())
	logID := uuid.Must(uuid.NewV7())
	_, err = ms.Control.ExecContext(ctx,
		`INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, '#go', ?, ?)`,
		registryID[:], n.ID[:], BufferChannel, Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = logStore.DB.ExecContext(ctx,
		`INSERT INTO buffers(id, name, kind, created_at) VALUES (?, '#go', ?, ?)`,
		logID[:], BufferChannel, Now(),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, err = ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	if !errors.Is(err, ErrBufferIDMismatch) {
		t.Fatalf("err = %v, want ErrBufferIDMismatch", err)
	}
}
