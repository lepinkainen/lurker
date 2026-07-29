package irc

import (
	"testing"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

func bufferArchived(t *testing.T, f *testHandlerFixture, name, kind string) bool {
	t.Helper()
	id, _, _, err := f.Stores.EnsureBuffer(t.Context(), f.Network.ID, name, kind)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := ircdb.GetBufferSettings(t.Context(), f.Stores.Control, id)
	if err != nil {
		t.Fatal(err)
	}
	return settings.Archived
}

func hasArchivedUpdateIn(evs []any, archived bool) bool {
	for _, ev := range evs {
		if bu, ok := ev.(*BufferUpdateEvent); ok && bu.Archived != nil && *bu.Archived == archived {
			return true
		}
	}
	return false
}

func TestSelfPartArchivesAndRejoinUnarchives(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	drainEvents(events)

	f.Handler.onPart(client, mustEvent(t, ":tester!u@h PART #test :bye"))
	drained := drainEvents(events)
	if !bufferArchived(t, f, "#test", ircdb.BufferChannel) {
		t.Fatal("self part did not archive the channel")
	}
	if !hasArchivedUpdateIn(drained, true) {
		t.Fatal("missing buffer_update archived=true for self part")
	}

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	drained = drainEvents(events)
	if bufferArchived(t, f, "#test", ircdb.BufferChannel) {
		t.Fatal("rejoin did not clear the archived flag")
	}
	if !hasArchivedUpdateIn(drained, false) {
		t.Fatal("missing buffer_update archived=false for rejoin")
	}
}

func TestSelfKickArchivesChannel(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	drainEvents(events)
	f.Handler.onKick(client, mustEvent(t, ":op!u@h KICK #test tester :begone"))

	drained := drainEvents(events)
	if !bufferArchived(t, f, "#test", ircdb.BufferChannel) {
		t.Fatal("self kick did not archive the channel")
	}
	if !hasArchivedUpdateIn(drained, true) {
		t.Fatal("missing buffer_update archived=true for self kick")
	}
}

func TestRemoteJoinDoesNotChurnArchivedFlag(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	drainEvents(events)
	f.Handler.onJoin(client, mustEvent(t, ":alice!u@h JOIN #test"))

	if hasArchivedUpdateIn(drainEvents(events), false) {
		t.Fatal("remote join republished archived=false with no change")
	}
}

func TestIncomingPMUnarchivesQuery(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	// First PM creates the query buffer; archive it manually.
	f.Handler.onPrivmsg(client, mustEvent(t, ":spammer!u@h PRIVMSG tester :hello"))
	drainEvents(events)
	id, _, _, err := f.Stores.EnsureBuffer(t.Context(), f.Network.ID, "spammer", ircdb.BufferQuery)
	if err != nil {
		t.Fatal(err)
	}
	archived := true
	if _, err := ircdb.UpdateBufferSettings(t.Context(), f.Stores.Control, id, ircdb.BufferSettingsPatch{Archived: &archived}); err != nil {
		t.Fatal(err)
	}

	f.Handler.onPrivmsg(client, mustEvent(t, ":spammer!u@h PRIVMSG tester :hello again"))
	drained := drainEvents(events)
	if bufferArchived(t, f, "spammer", ircdb.BufferQuery) {
		t.Fatal("incoming PM did not unarchive the query buffer")
	}
	if !hasArchivedUpdateIn(drained, false) {
		t.Fatal("missing buffer_update archived=false for incoming PM")
	}
}
