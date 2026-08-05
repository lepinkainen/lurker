package irc

import (
	"fmt"
	"strings"
	"testing"
	"time"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
	"github.com/lrstanley/girc"
)

func parseEvent(t *testing.T, raw string) girc.Event {
	t.Helper()
	e := girc.ParseEvent(raw)
	if e == nil {
		t.Fatalf("unparseable event: %s", raw)
	}
	return *e
}

// stubChathistory arms the handler's girc seams: cap present, sends
// captured, server limit fixed.
func stubChathistory(h *handler, limit int, sent *[]string) {
	h.hasCap = func(name string) bool { return name == "draft/chathistory" }
	h.sendRaw = func(line string) error {
		*sent = append(*sent, line)
		return nil
	}
	h.historyLimit = func() int { return limit }
}

func TestChathistoryBatchPersistsWithoutLivePublish(t *testing.T) {
	eventHub := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(eventHub))
	events, _, unsub := eventHub.Subscribe(16)
	defer unsub()

	var sent []string
	stubChathistory(f.Handler, 100, &sent)

	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH +ref1 chathistory #go"))
	f.Handler.onPrivmsg(nil, parseEvent(t, "@batch=ref1;msgid=m1;time=2026-08-05T10:00:00.000Z :alice!a@host PRIVMSG #go :first"))
	f.Handler.onPrivmsg(nil, parseEvent(t, "@batch=ref1;msgid=m2;time=2026-08-05T10:01:00.000Z :bob!b@host PRIVMSG #go :second"))
	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH -ref1"))

	if got := handlerMessageCount(t, f); got != 2 {
		t.Fatalf("stored %d messages, want 2", got)
	}
	msg := lastHandlerMessage(t, f)
	if msg.BufferName != "#go" || msg.BufferKind != ircdb.BufferChannel || msg.Content != "second" {
		t.Fatalf("unexpected stored message: %+v", msg)
	}

	var backfills []*HistoryBackfillEvent
	for _, ev := range drainEvents(events) {
		switch ev := ev.(type) {
		case *MessageEvent:
			t.Fatalf("backfill published live message event: %+v", ev)
		case *HistoryBackfillEvent:
			backfills = append(backfills, ev)
		}
	}
	if len(backfills) != 1 || backfills[0].Count != 2 {
		t.Fatalf("unexpected backfill events: %+v", backfills)
	}
	if len(sent) != 0 {
		t.Fatalf("partial page must not paginate, sent: %v", sent)
	}
}

func TestChathistorySelfMessageFilesUnderBatchTarget(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 100, &sent)

	// Our own replayed PM: source is our nick, batch target is the peer.
	// Source-based routing would misfile this under our own nick.
	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH +ref1 chathistory peer"))
	f.Handler.onPrivmsg(nil, parseEvent(t, "@batch=ref1;msgid=m1;time=2026-08-05T10:00:00.000Z :tester!t@host PRIVMSG peer :hi there"))
	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH -ref1"))

	msg := lastHandlerMessage(t, f)
	if msg.BufferName != "peer" || msg.BufferKind != ircdb.BufferQuery {
		t.Fatalf("self message filed to %s/%s, want peer/query", msg.BufferName, msg.BufferKind)
	}
	if msg.Sender != "tester" {
		t.Fatalf("sender = %q, want tester", msg.Sender)
	}
}

func TestChathistoryDedupesReplayedMsgID(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 100, &sent)

	// Live message first, then the server replays the same msgid.
	f.Handler.onPrivmsg(nil, parseEvent(t, "@msgid=dupe;time=2026-08-05T10:00:00.000Z :alice!a@host PRIVMSG #go :hello"))
	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH +ref1 chathistory #go"))
	f.Handler.onPrivmsg(nil, parseEvent(t, "@batch=ref1;msgid=dupe;time=2026-08-05T10:00:00.000Z :alice!a@host PRIVMSG #go :hello"))
	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH -ref1"))

	if got := handlerMessageCount(t, f); got != 1 {
		t.Fatalf("stored %d messages, want 1 (deduped)", got)
	}
}

func TestChathistoryUnknownBatchRefFlowsLive(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 100, &sent)

	// A batch tag from some other batch type (e.g. netsplit) must not be
	// swallowed by the chathistory path.
	f.Handler.onPrivmsg(nil, parseEvent(t, "@batch=other;msgid=m1 :alice!a@host PRIVMSG #go :live message"))

	msg := lastHandlerMessage(t, f)
	if msg.BufferName != "#go" || msg.Content != "live message" {
		t.Fatalf("unexpected stored message: %+v", msg)
	}
}

func TestChathistoryFullPagePaginates(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 2, &sent)

	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH +ref1 chathistory #go"))
	f.Handler.onPrivmsg(nil, parseEvent(t, "@batch=ref1;msgid=m1;time=2026-08-05T10:00:00.000Z :alice!a@host PRIVMSG #go :first"))
	f.Handler.onPrivmsg(nil, parseEvent(t, "@batch=ref1;msgid=m2;time=2026-08-05T10:01:00.000Z :bob!b@host PRIVMSG #go :second"))
	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH -ref1"))

	want := "CHATHISTORY AFTER #go timestamp=2026-08-05T10:01:00.000Z 2"
	if len(sent) != 1 || sent[0] != want {
		t.Fatalf("sent = %v, want [%s]", sent, want)
	}
}

func TestChathistoryPaginationBounded(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 1, &sent)

	// Every page comes back full; pagination must stop at the cap.
	for i := range chathistoryMaxPages + 3 {
		ref := fmt.Sprintf("ref%d", i)
		f.Handler.onBatch(nil, parseEvent(t, fmt.Sprintf(":irc.test BATCH +%s chathistory #go", ref)))
		f.Handler.onPrivmsg(nil, parseEvent(t, fmt.Sprintf("@batch=%s;msgid=m%d;time=2026-08-05T10:0%d:00.000Z :alice!a@host PRIVMSG #go :msg", ref, i, i%10)))
		f.Handler.onBatch(nil, parseEvent(t, fmt.Sprintf(":irc.test BATCH -%s", ref)))
	}

	if len(sent) > chathistoryMaxPages {
		t.Fatalf("sent %d requests, want at most %d", len(sent), chathistoryMaxPages)
	}
}

func TestMaybeRequestChathistoryAnchorsOnNewestStoredTS(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 100, &sent)

	ctx := t.Context()
	bufID, _, _, err := f.Stores.EnsureBuffer(ctx, f.Network.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 5, 9, 30, 0, 0, time.UTC)
	if _, _, _, err := ircdb.InsertLogMessage(ctx, f.LogStore.DB, ircdb.LogMessageInput{
		BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "old", Timestamp: at,
	}); err != nil {
		t.Fatal(err)
	}

	f.Handler.maybeRequestChathistory("#go")

	want := "CHATHISTORY AFTER #go timestamp=2026-08-05T09:30:00.000Z 100"
	if len(sent) != 1 || sent[0] != want {
		t.Fatalf("sent = %v, want [%s]", sent, want)
	}
}

func TestMaybeRequestChathistorySkipsWithoutAnchor(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 100, &sent)

	// Unknown buffer: nothing stored yet, no anchor to request AFTER.
	f.Handler.maybeRequestChathistory("#never-seen")

	// Buffer exists but is empty.
	ctx := t.Context()
	if _, _, _, err := f.Stores.EnsureBuffer(ctx, f.Network.ID, "#empty", ircdb.BufferChannel); err != nil {
		t.Fatal(err)
	}
	f.Handler.maybeRequestChathistory("#empty")

	if len(sent) != 0 {
		t.Fatalf("sent = %v, want none", sent)
	}
}

func TestMaybeRequestChathistorySkipsWithoutCap(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 100, &sent)
	f.Handler.hasCap = func(string) bool { return false }

	ctx := t.Context()
	bufID, _, _, err := f.Stores.EnsureBuffer(ctx, f.Network.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ircdb.InsertLogMessage(ctx, f.LogStore.DB, ircdb.LogMessageInput{
		BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "old", Timestamp: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	f.Handler.maybeRequestChathistory("#go")
	if len(sent) != 0 {
		t.Fatalf("sent = %v, want none", sent)
	}
}

func TestRequestQueryBackfillsTargetsQueriesOnly(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 100, &sent)

	ctx := t.Context()
	at := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	for name, kind := range map[string]string{
		"#go":   ircdb.BufferChannel,
		"peer":  ircdb.BufferQuery,
		"other": ircdb.BufferQuery,
	} {
		bufID, _, _, err := f.Stores.EnsureBuffer(ctx, f.Network.ID, name, kind)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := ircdb.InsertLogMessage(ctx, f.LogStore.DB, ircdb.LogMessageInput{
			BufferID: bufID, Sender: "x", Kind: "privmsg", Content: "m", Timestamp: at,
		}); err != nil {
			t.Fatal(err)
		}
	}

	f.Handler.requestQueryBackfills()

	if len(sent) != 2 {
		t.Fatalf("sent %d requests, want 2 (queries only): %v", len(sent), sent)
	}
	for _, line := range sent {
		if strings.Contains(line, "#go") {
			t.Fatalf("channel buffer requested from requestQueryBackfills: %v", sent)
		}
	}
}

func TestChathistoryActionKind(t *testing.T) {
	f := newTestHandlerFixture(t)
	var sent []string
	stubChathistory(f.Handler, 100, &sent)

	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH +ref1 chathistory #go"))
	f.Handler.onPrivmsg(nil, parseEvent(t, "@batch=ref1;msgid=m1;time=2026-08-05T10:00:00.000Z :alice!a@host PRIVMSG #go :\x01ACTION waves\x01"))
	f.Handler.onBatch(nil, parseEvent(t, ":irc.test BATCH -ref1"))

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "action" || msg.Content != "waves" {
		t.Fatalf("kind=%q content=%q, want action/waves", msg.Kind, msg.Content)
	}
}
