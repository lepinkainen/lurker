package db

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeleteBufferRemovesEverything(t *testing.T) {
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
	doomed, _, _, err := ms.EnsureBuffer(ctx, n.ID, "#doomed", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	sibling, _, _, err := ms.EnsureBuffer(ctx, n.ID, "#sibling", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := ms.LogStore(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: doomed, Sender: "spammer", Kind: "privmsg", Content: "buy crypto now"}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := InsertLogMessage(ctx, logStore.DB, LogMessageInput{BufferID: sibling, Sender: "alice", Kind: "privmsg", Content: "legit chatter"}); err != nil {
		t.Fatal(err)
	}
	archived := true
	if _, err := UpdateBufferSettings(ctx, ms.Control, doomed, BufferSettingsPatch{Archived: &archived}); err != nil {
		t.Fatal(err)
	}

	if err := ms.DeleteBuffer(ctx, doomed); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := ms.LookupBuffer(ctx, doomed); err == nil {
		t.Fatal("registry row survived deletion")
	}
	if _, err := GetBufferSettings(ctx, ms.Control, doomed); err == nil {
		t.Fatal("buffer_settings row survived deletion")
	}
	if msgs, err := ms.RecentMessages(ctx, sibling, 10); err != nil || len(msgs) != 1 {
		t.Fatalf("sibling buffer damaged: msgs=%d err=%v", len(msgs), err)
	}
	// FTS must not surface deleted history (messages_ad trigger fired).
	hits, err := SearchLogMessages(ctx, logStore.DB, "crypto", uuid.Nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("FTS still finds deleted messages: %d hits", len(hits))
	}

	// Recreating the same name must mint a fresh UUID — no history resurrection.
	reborn, created, _, err := ms.EnsureBuffer(ctx, n.ID, "#doomed", BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected recreated buffer to be reported as created")
	}
	if reborn == doomed {
		t.Fatal("recreated buffer reused the deleted UUID")
	}
	if msgs, err := ms.RecentMessages(ctx, reborn, 10); err != nil || len(msgs) != 0 {
		t.Fatalf("recreated buffer has resurrected history: msgs=%d err=%v", len(msgs), err)
	}
}

func TestDeleteBufferRejectsStatusAndUnknown(t *testing.T) {
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
	status, _, _, err := ms.EnsureBuffer(ctx, n.ID, "Libera", BufferStatus)
	if err != nil {
		t.Fatal(err)
	}
	if err := ms.DeleteBuffer(ctx, status); err == nil {
		t.Fatal("expected error deleting status buffer")
	}
	if err := ms.DeleteBuffer(ctx, uuid.New()); err == nil {
		t.Fatal("expected error deleting unknown buffer")
	}
}
