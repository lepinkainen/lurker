package db

import (
	"testing"

	"github.com/google/uuid"
)

func TestFTSQuery(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"   ":           "",
		"needle":        `"needle"*`,
		"two words":     `"two" "words"*`,
		`foo"bar`:       `"foo""bar"*`,
		"NOT a OR b":    `"NOT" "a" "OR" "b"*`,
		`sender:x NEAR`: `"sender:x" "NEAR"*`,
	}
	for in, want := range cases {
		if got := FTSQuery(in); got != want {
			t.Errorf("FTSQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchFiltersKindsPagesAndSurvivesOperators(t *testing.T) {
	ctx := t.Context()
	ms, networkID, bufferID := newSearchFixture(t)
	ls, _ := ms.LogStore(networkID)

	var ids []uuid.UUID
	for _, in := range []LogMessageInput{
		{BufferID: bufferID, Sender: "alice", Kind: "privmsg", Content: "first needle"},
		{BufferID: bufferID, Sender: "needle", Kind: "join", Content: ""},
		{BufferID: bufferID, Sender: "bob", Kind: "action", Content: "second needle"},
		{BufferID: bufferID, Sender: "bob", Kind: "privmsg", Content: "third needle"},
	} {
		id, _, _, err := InsertLogMessage(ctx, ls, in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	got, err := ms.Search(ctx, "needle", uuid.Nil, bufferID, uuid.Nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != ids[3] || got[2].ID != ids[0] {
		t.Fatalf("expected 3 conversational hits newest first, got %+v", got)
	}

	page, err := ms.Search(ctx, "needle", uuid.Nil, bufferID, ids[3], 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].ID != ids[2] {
		t.Fatalf("before-paging: got %+v want %s", page, ids[2])
	}

	for _, raw := range []string{`needle"`, `NOT needle`, `needle NEAR(x y)`, `sender:alice`, `(`} {
		if _, err := ms.Search(ctx, raw, uuid.Nil, bufferID, uuid.Nil, 10); err != nil {
			t.Errorf("query %q errored: %v", raw, err)
		}
	}
}

func TestSearchAllNetworksMergesByID(t *testing.T) {
	ctx := t.Context()
	ms, n1, b1 := newSearchFixture(t)
	n2, err := ms.UpsertNetwork(ctx, Network{Name: "Two", Host: "two.example", Port: 6697, TLS: true, Nick: "n"})
	if err != nil {
		t.Fatal(err)
	}
	b2, _, _, _ := ms.EnsureBuffer(ctx, n2.ID, "#go", BufferChannel)
	ls1, _ := ms.LogStore(n1)
	ls2, _ := ms.LogStore(n2.ID)

	var ids []uuid.UUID
	for i, ls := range []*LogStore{ls1, ls2, ls1, ls2} {
		buf := b1
		if i%2 == 1 {
			buf = b2
		}
		id, _, _, _ := InsertLogMessage(ctx, ls, LogMessageInput{BufferID: buf, Sender: "x", Kind: "privmsg", Content: "needle"})
		ids = append(ids, id)
	}
	got, err := ms.Search(ctx, "needle", uuid.Nil, uuid.Nil, uuid.Nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != ids[3] || got[1].ID != ids[2] || got[2].ID != ids[1] {
		t.Fatalf("cross-network merge: got %+v", got)
	}
}

func TestMessagesAround(t *testing.T) {
	ctx := t.Context()
	ms, networkID, bufferID := newSearchFixture(t)
	ls, _ := ms.LogStore(networkID)
	var ids []uuid.UUID
	for range 7 {
		id, _, _, _ := InsertLogMessage(ctx, ls, LogMessageInput{BufferID: bufferID, Sender: "x", Kind: "privmsg", Content: "m"})
		ids = append(ids, id)
	}
	got, err := ms.MessagesAround(ctx, bufferID, ids[3], 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("len=%d want 5 (2 older, target, 2 newer)", len(got))
	}
	for i, m := range got {
		if m.ID != ids[i+1] {
			t.Fatalf("pos %d: got %s want %s", i, m.ID, ids[i+1])
		}
	}
	// Edge: target is the oldest message → no older rows, window still ordered.
	got, err = ms.MessagesAround(ctx, bufferID, ids[0], 4)
	if err != nil || len(got) != 3 || got[0].ID != ids[0] {
		t.Fatalf("edge window: %+v err=%v", got, err)
	}
}

func newSearchFixture(t *testing.T) (*MultiStore, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ms.Close() })
	n, err := ms.UpsertNetwork(ctx, Network{Name: "One", Host: "one.example", Port: 6697, TLS: true, Nick: "n"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, _, err := ms.EnsureBuffer(ctx, n.ID, "#go", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	return ms, n.ID, b
}
