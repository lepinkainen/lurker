package db

import (
	"bytes"
	"testing"
	"time"
)

func TestNewIDAtEncodesTimestamp(t *testing.T) {
	at := time.Date(2026, 3, 1, 12, 30, 45, 500_000_000, time.UTC)
	id := newIDAt(at)

	if id.Version() != 7 {
		t.Fatalf("version = %d, want 7", id.Version())
	}
	sec, nsec := id.Time().UnixTime()
	got := time.Unix(sec, nsec).UTC()
	if !got.Equal(at.Truncate(time.Millisecond)) {
		t.Fatalf("id time = %v, want %v", got, at)
	}
}

func TestNewIDAtSameMillisecondKeepsGenerationOrder(t *testing.T) {
	// CHATHISTORY replays deliver several messages with identical server
	// timestamps (same ms). Their row IDs must still sort in delivery
	// order, or history reads scramble intra-millisecond message order.
	at := time.Date(2026, 8, 5, 11, 25, 13, 287_000_000, time.UTC)
	prev := newIDAt(at)
	for i := range 20 {
		// Cross a wall-clock millisecond between generations: uuid.NewV7's
		// monotonicity counter re-randomizes each new ms, which must not
		// leak into the ordering of same-message-ms backfill IDs.
		time.Sleep(2 * time.Millisecond)
		next := newIDAt(at)
		if bytes.Compare(next[:], prev[:]) <= 0 {
			t.Fatalf("id %d (%s) does not sort after its predecessor (%s)", i+1, next, prev)
		}
		prev = next
	}
}

func TestBackfillInsertKeepsIDOrderChronological(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	bufID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)

	// Live message arrives now; a backfilled one from an hour ago is
	// inserted afterwards but must sort before it by id.
	liveID, _, _, err := InsertLogMessage(ctx, logStore.DB, LogMessageInput{
		BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "live", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	backfillID, _, inserted, err := InsertLogMessage(ctx, logStore.DB, LogMessageInput{
		BufferID: bufID, MsgID: "abc1", Sender: "bob", Kind: "privmsg", Content: "old",
		Timestamp: time.Now().Add(-time.Hour), Backfill: true,
	})
	if err != nil || !inserted {
		t.Fatalf("backfill insert: inserted=%v err=%v", inserted, err)
	}
	if bytes.Compare(backfillID[:], liveID[:]) >= 0 {
		t.Fatalf("backfill id %s should sort before live id %s", backfillID, liveID)
	}

	rows, err := RecentLogMessages(ctx, logStore.DB, bufID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Content != "old" || rows[1].Content != "live" {
		t.Fatalf("unexpected order: %+v", rows)
	}
}

func TestBackfillInsertDedupesByMsgID(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	bufID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)

	in := LogMessageInput{
		BufferID: bufID, MsgID: "dupe1", Sender: "bob", Kind: "privmsg", Content: "hello",
		Timestamp: time.Now().Add(-time.Minute), Backfill: true,
	}
	if _, _, inserted, err := InsertLogMessage(ctx, logStore.DB, in); err != nil || !inserted {
		t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
	}
	if _, _, inserted, err := InsertLogMessage(ctx, logStore.DB, in); err != nil || inserted {
		t.Fatalf("second insert should dedupe: inserted=%v err=%v", inserted, err)
	}
}

func TestLatestLogMessageTS(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, _ := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	bufID, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	logStore, _ := ms.LogStore(n.ID)

	got, err := LatestLogMessageTS(ctx, logStore.DB, bufID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("empty buffer ts = %q, want empty", got)
	}

	older := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	for _, in := range []LogMessageInput{
		{BufferID: bufID, Sender: "a", Kind: "privmsg", Content: "1", Timestamp: newer},
		{BufferID: bufID, Sender: "a", Kind: "privmsg", Content: "2", Timestamp: older},
	} {
		if _, _, _, err := InsertLogMessage(ctx, logStore.DB, in); err != nil {
			t.Fatal(err)
		}
	}
	got, err = LatestLogMessageTS(ctx, logStore.DB, bufID)
	if err != nil {
		t.Fatal(err)
	}
	if got != FormatTime(newer) {
		t.Fatalf("latest ts = %q, want %q", got, FormatTime(newer))
	}

	otherBuf, _, _, _ := ms.EnsureBuffer(ctx, n.ID, "#rust", BufferChannel)
	got, err = LatestLogMessageTS(ctx, logStore.DB, otherBuf)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("other buffer ts = %q, want empty", got)
	}
}
