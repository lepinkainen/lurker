package irc

import (
	"testing"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

func TestSelfPartEmitsBufferUpdateAndPresence(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	drainEvents(events)
	f.Handler.onPart(client, mustEvent(t, ":tester!u@h PART #test :bye"))

	drained := drainEventsAfter(events)
	if !hasBufferUpdateIn(drained, false) {
		t.Fatal("missing buffer_update joined=false for self part")
	}
	if !hasPresenceIn(drained, "tester", "part") {
		t.Fatal("missing self-part presence for tester")
	}
}

func TestRemotePartPublishesPresenceOnly(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	drainEvents(events)
	f.Handler.onPart(client, mustEvent(t, ":alice!u@h PART #test :bye"))

	drained := drainEventsAfter(events)
	if !hasPresenceIn(drained, "alice", "part") {
		t.Fatal("missing remote part presence for alice")
	}
	if hasBufferUpdateIn(drained, false) {
		t.Fatal("unexpected buffer_update joined=false on remote part")
	}
}

// drainEventsAfter snapshots remaining events. Kept local so we can run the
// helpers below against the snapshot without re-draining the channel.
func drainEventsAfter(events <-chan any) []any {
	return drainEvents(events)
}

func hasBufferUpdateIn(evs []any, joined bool) bool {
	for _, ev := range evs {
		if bu, ok := ev.(*BufferUpdateEvent); ok && bu.Joined == joined {
			return true
		}
	}
	return false
}

func hasPresenceIn(evs []any, nick, state string) bool {
	for _, ev := range evs {
		if p, ok := ev.(*PresenceEvent); ok && p.Nick == nick && p.State == state {
			return true
		}
	}
	return false
}

func TestTopicUpdatePersistsTopicAndPublishesBufferUpdate(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onTopic(nil, mustEvent(t, ":alice!u@h TOPIC #test :new topic"))

	var topic string
	if err := f.LogStore.DB.QueryRow(`SELECT topic FROM buffers WHERE name='#test'`).Scan(&topic); err != nil {
		t.Fatal(err)
	}
	if topic != "new topic" {
		t.Fatalf("topic = %q, want new topic", topic)
	}
	if !hasTopicUpdate(events, "new topic") {
		t.Fatal("missing topic buffer_update")
	}
}

func TestModeRouting(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onMode(nil, mustEvent(t, ":alice!u@h MODE #test +o bob"))
	f.Handler.onMode(nil, mustEvent(t, ":irc.example MODE tester +i"))

	rows, err := f.LogStore.DB.Query(`
		SELECT b.name, b.kind, m.kind, COALESCE(m.target, ''), COALESCE(m.content, '')
		FROM messages m
		JOIN buffers b ON b.id = m.buffer_id
		ORDER BY m.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			t.Fatalf("close rows: %v", cerr)
		}
	}()
	var got []handlerMessage
	for rows.Next() {
		var msg handlerMessage
		if err := rows.Scan(&msg.BufferName, &msg.BufferKind, &msg.Kind, &msg.Target, &msg.Content); err != nil {
			t.Fatal(err)
		}
		got = append(got, msg)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("messages = %+v, want 2", got)
	}
	if got[0].BufferName != "#test" || got[0].BufferKind != ircdb.BufferChannel || got[0].Content != "+o bob" {
		t.Fatalf("channel mode message = %+v", got[0])
	}
	if got[1].BufferKind != ircdb.BufferStatus || got[1].Target != "tester" || got[1].Content != "+i" {
		t.Fatalf("user mode message = %+v", got[1])
	}
}
