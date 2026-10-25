package db

import (
	"bytes"
	"testing"

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
