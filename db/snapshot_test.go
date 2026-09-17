package db

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// SnapshotNetwork must return windows and unread candidates from one
// consistent view: every unread candidate newer than a buffer's window max
// would be a message the window should have contained.
func TestSnapshotNetworkWindowAndUnreadAgree(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ms.Close() })
	n, err := ms.UpsertNetwork(ctx, Network{Name: "n", Host: "h", Port: 6667, Nick: "me"})
	if err != nil {
		t.Fatal(err)
	}
	bufID, _, _, err := ms.EnsureBuffer(ctx, n.ID, "#a", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := ms.LogStore(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for range 5 {
		id, _, _, err := InsertLogMessage(ctx, logStore, LogMessageInput{BufferID: bufID, Kind: "privmsg", Sender: "bob", Content: "x"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	snap, err := ms.SnapshotNetwork(ctx, n.ID, map[uuid.UUID]uuid.UUID{bufID: ids[1]}, 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(snap.Recent[bufID]); got != 2 {
		t.Fatalf("window = %d, want 2", got)
	}
	if got := len(snap.Unread[bufID]); got != 3 {
		t.Fatalf("unread candidates = %d, want 3 (after ids[1])", got)
	}
	maxID := snap.Recent[bufID][len(snap.Recent[bufID])-1].ID
	for _, c := range snap.Unread[bufID] {
		if bytes.Compare(c.ID[:], maxID[:]) > 0 {
			t.Fatalf("unread candidate %s newer than window max %s", c.ID, maxID)
		}
	}
}

func TestInsertLockIsPerNetworkAndSkipsBackfill(t *testing.T) {
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ms.Close() })
	var stores []*LogStore
	var buffers []uuid.UUID
	for _, name := range []string{"a", "b"} {
		n, err := ms.UpsertNetwork(t.Context(), Network{Name: name, Host: "h", Port: 6667, Nick: "me"})
		if err != nil {
			t.Fatal(err)
		}
		buf, _, _, err := ms.EnsureBuffer(t.Context(), n.ID, "#test", BufferChannel)
		if err != nil {
			t.Fatal(err)
		}
		store, err := ms.LogStore(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, store)
		buffers = append(buffers, buf)
	}
	stores[0].insertMu.Lock()
	defer stores[0].insertMu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for _, tc := range []struct {
		name     string
		index    int
		backfill bool
	}{{"other network", 1, false}, {"backfill", 0, true}} {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() {
				_, _, _, err := InsertLogMessage(ctx, stores[tc.index], LogMessageInput{
					BufferID: buffers[tc.index], Kind: "privmsg", Sender: "bob",
					Backfill: tc.backfill, Timestamp: time.Now().Add(-time.Hour),
				})
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("insert blocked on another network's live insert lock")
			}
		})
	}
}
