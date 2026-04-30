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
	client := newTestClient(t, "tester")
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

func TestChghostStoresIdentAndHost(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onChghost(nil, mustEvent(t, ":alice!u@old.host CHGHOST newident new.host"))

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "chghost" || msg.Target != "alice" || msg.Content != "newident new.host" {
		t.Fatalf("message = %+v", msg)
	}
}
