package irc

import (
	"testing"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

type recordingPreviewEnqueuer struct {
	calls []previewCall
}

type previewCall struct {
	networkID uuid.UUID
	bufferID  uuid.UUID
	messageID uuid.UUID
	content   string
}

func (r *recordingPreviewEnqueuer) Enqueue(networkID, bufferID, messageID uuid.UUID, content string) {
	r.calls = append(r.calls, previewCall{networkID: networkID, bufferID: bufferID, messageID: messageID, content: content})
}

func TestServerNoticeRoutesToStatusBuffer(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onPrivmsg(nil, mustEvent(t, ":irc.example NOTICE tester :maintenance soon"))

	msg := lastHandlerMessage(t, f)
	if msg.BufferKind != ircdb.BufferStatus || msg.BufferName != "*status*" {
		t.Fatalf("buffer = %q/%q, want status", msg.BufferKind, msg.BufferName)
	}
	if msg.Kind != "notice" || msg.Sender != "irc.example" || msg.Content != "maintenance soon" {
		t.Fatalf("message = %+v", msg)
	}
}

func TestAllEventsSkipsExplicitPrivmsgHandler(t *testing.T) {
	f := newTestHandlerFixture(t)
	e := mustEvent(t, "@msgid=abc123 :egobot!u@h PRIVMSG ##hntop :new post")

	f.Handler.onAllEvent(nil, e)
	f.Handler.onPrivmsg(nil, e)

	if count := handlerMessageCount(t, f); count != 1 {
		t.Fatalf("message count = %d, want 1", count)
	}
	msg := lastHandlerMessage(t, f)
	if msg.BufferName != "##hntop" || msg.BufferKind != ircdb.BufferChannel || msg.Kind != "privmsg" || msg.Content != "new post" {
		t.Fatalf("message = %+v, want channel privmsg", msg)
	}
}

// TAGMSG (typing indicators) and BATCH framing carry no displayable
// content and must not be stored as status notices.
func TestTagmsgAndBatchAreNotPersisted(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onAllEvent(nil, mustEvent(t, "@+typing=active :buggy!u@h TAGMSG #chan"))
	f.Handler.onAllEvent(nil, mustEvent(t, ":irc.example BATCH +yXNAbvnRHTRBv netsplit irc.hub other.host"))

	if count := handlerMessageCount(t, f); count != 0 {
		t.Fatalf("message count = %d, want 0", count)
	}
}

func TestPrivmsgCTCPAndActionKinds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		raw         string
		wantKind    string
		wantContent string
	}{
		{name: "action", raw: ":alice!u@h PRIVMSG #test :\x01ACTION waves\x01", wantKind: "action", wantContent: "waves"},
		{name: "ctcp", raw: ":alice!u@h PRIVMSG #test :\x01VERSION client\x01", wantKind: "ctcp", wantContent: "VERSION client"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestHandlerFixture(t)

			f.Handler.onPrivmsg(nil, mustEvent(t, tc.raw))

			msg := lastHandlerMessage(t, f)
			if msg.Kind != tc.wantKind || msg.Content != tc.wantContent {
				t.Fatalf("message = %+v, want kind=%q content=%q", msg, tc.wantKind, tc.wantContent)
			}
		})
	}
}

func TestIgnoredNickSkipsPersistence(t *testing.T) {
	f := newTestHandlerFixture(t)
	if err := ircdb.CreateIgnore(t.Context(), f.Stores.Control, f.Network.ID, "bad*", ircdb.IgnoreLevelHide); err != nil {
		t.Fatal(err)
	}

	f.Handler.onPrivmsg(nil, mustEvent(t, ":badguy!u@h PRIVMSG #test :ignore me"))
	f.Handler.onPrivmsg(nil, mustEvent(t, ":alice!u@h PRIVMSG #test :keep me"))

	if count := handlerMessageCount(t, f); count != 1 {
		t.Fatalf("message count = %d, want 1", count)
	}
	msg := lastHandlerMessage(t, f)
	if msg.Sender != "alice" || msg.Content != "keep me" {
		t.Fatalf("message = %+v", msg)
	}
}

// TestMutedNickIsStoredButFlaggedNotCountingUnread verifies mute-tier
// ignores are persisted and published like normal messages, but the
// outgoing MessageEvent has CountsAsUnread forced false so buffer tallies
// skip them.
func TestMutedNickIsStoredButFlaggedNotCountingUnread(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	if err := ircdb.CreateIgnore(t.Context(), f.Stores.Control, f.Network.ID, "weatherbot", ircdb.IgnoreLevelMute); err != nil {
		t.Fatal(err)
	}

	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onPrivmsg(nil, mustEvent(t, ":weatherbot!u@h PRIVMSG #test :sunny today"))

	if count := handlerMessageCount(t, f); count != 1 {
		t.Fatalf("message count = %d, want 1 (muted sender is still stored)", count)
	}
	msg := lastHandlerMessage(t, f)
	if msg.Sender != "weatherbot" || msg.Content != "sunny today" {
		t.Fatalf("message = %+v", msg)
	}

	drained := drainEvents(events)
	var found bool
	for _, ev := range drained {
		me, ok := ev.(*MessageEvent)
		if !ok {
			continue
		}
		found = true
		if me.CountsAsUnread {
			t.Fatalf("CountsAsUnread = true, want false for muted sender")
		}
	}
	if !found {
		t.Fatal("no MessageEvent published")
	}
}

func TestPreviewEnqueuedOnlyForUserAuthoredMessageKinds(t *testing.T) {
	f := newTestHandlerFixture(t, withTestHandlerHub(hub.New()))
	previews := &recordingPreviewEnqueuer{}
	f.Handler.previews = previews

	f.Handler.onPrivmsg(nil, mustEvent(t, ":alice!u@h PRIVMSG #test :https://example.com/a"))
	f.Handler.onPrivmsg(nil, mustEvent(t, ":alice!u@h NOTICE #test :https://example.com/b"))
	f.Handler.onPrivmsg(nil, mustEvent(t, ":alice!u@h PRIVMSG #test :\x01ACTION shares https://example.com/c\x01"))
	f.Handler.onJoin(nil, mustEvent(t, ":alice!u@h JOIN #test"))
	f.Handler.onMode(nil, mustEvent(t, ":alice!u@h MODE #test +o bob"))

	if len(previews.calls) != 3 {
		t.Fatalf("preview calls = %d, want 3: %+v", len(previews.calls), previews.calls)
	}
	for _, call := range previews.calls {
		if call.networkID != f.Network.ID || call.bufferID == uuid.Nil || call.messageID == uuid.Nil || call.content == "" {
			t.Fatalf("bad preview call: %+v", call)
		}
	}
}
