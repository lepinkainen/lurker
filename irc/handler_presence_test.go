package irc

import (
	"testing"

	"github.com/lepinkainen/lurker/hub"
)

func TestNickPublishesPresenceAndUpdatesSelfNick(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)
	var currentNick string
	f.Handler.connectedHook = func(nick string) { currentNick = nick }

	f.Handler.onNick(client, mustEvent(t, ":tester!u@h NICK newtester"))

	if currentNick != "newtester" {
		t.Fatalf("current nick = %q, want newtester", currentNick)
	}
	if !hasPresence(events, "tester", "nick") {
		t.Fatal("missing nick presence event")
	}
	msg := lastHandlerMessage(t, f)
	if msg.Kind != "nick" || msg.Target != "newtester" {
		t.Fatalf("message = %+v", msg)
	}
}

func TestAwayStoresAwayAndBack(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onAway(nil, mustEvent(t, ":alice!u@h AWAY :lunch"))
	f.Handler.onAway(nil, mustEvent(t, ":alice!u@h AWAY"))

	rows, err := f.LogStore.DB.Query(`SELECT kind, target, content FROM messages ORDER BY id`)
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
		if err := rows.Scan(&msg.Kind, &msg.Target, &msg.Content); err != nil {
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
	if got[0].Kind != "away" || got[0].Target != "alice" || got[0].Content != "lunch" {
		t.Fatalf("away message = %+v", got[0])
	}
	if got[1].Kind != "back" || got[1].Target != "alice" || got[1].Content != "" {
		t.Fatalf("back message = %+v", got[1])
	}
}

func TestAccountStarStoresEmptyAccount(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onAccount(nil, mustEvent(t, ":alice!u@h ACCOUNT *"))

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "account" || msg.Target != "alice" || msg.Content != "" {
		t.Fatalf("message = %+v", msg)
	}
}

// Regression for the per-channel QUIT fan-out: a remote user that we
// share two channels with must produce one quit row in each channel and
// no status-buffer row. Status-only quits are reserved for unknown nicks.
func TestQuitFansOutToSharedChannels(t *testing.T) {
	f := newTestHandlerFixture(t)
	// Seed userChannels: alice is in #a and #b.
	f.Handler.userChannels.addUser("alice", "#a")
	f.Handler.userChannels.addUser("alice", "#b")

	f.Handler.onQuit(nil, mustEvent(t, ":alice!u@h QUIT :irc.cc.tut.fi irc2.inet.fi"))

	rows, err := f.LogStore.DB.Query(`
		SELECT b.name, b.kind, m.kind, m.content
		FROM messages m JOIN buffers b ON b.id = m.buffer_id
		ORDER BY b.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type row struct{ Buf, BufKind, Kind, Content string }
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Buf, &r.BufKind, &r.Kind, &r.Content); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 channel rows, got %d: %+v", len(got), got)
	}
	for _, r := range got {
		if r.Kind != "quit" {
			t.Errorf("row kind=%q, want quit", r.Kind)
		}
		if r.BufKind != "channel" {
			t.Errorf("row buffer kind=%q, want channel (status fallback indicates fan-out missed)", r.BufKind)
		}
		if r.Content != "irc.cc.tut.fi irc2.inet.fi" {
			t.Errorf("row content=%q, want netsplit reason preserved", r.Content)
		}
	}
}

// Regression for the userChannels store-missing branch: when a quit
// arrives for a nick we don't know about, the row must still be written
// to the status buffer so netadmins keep cross-network visibility.
func TestQuitForUntrackedNickFallsBackToStatus(t *testing.T) {
	f := newTestHandlerFixture(t)
	f.Handler.onQuit(nil, mustEvent(t, ":stranger!u@h QUIT :Ping timeout"))

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "quit" || msg.BufferKind != "status" || msg.Content != "Ping timeout" {
		t.Fatalf("status fallback row = %+v", msg)
	}
}

func TestChghostStoresIdentAndHost(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onChghost(nil, mustEvent(t, ":alice!u@old.host CHGHOST newident new.host"))

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "chghost" || msg.Target != "alice" || msg.Content != "newident new.host" {
		t.Fatalf("message = %+v", msg)
	}
}
