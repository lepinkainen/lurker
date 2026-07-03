package irc

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

// newCommandTestManager builds a Manager with a network registered in the
// stores but no live IRC connection. Every command method should therefore
// return ErrNotConnected.
func newCommandTestManager(t *testing.T) (*Manager, uuid.UUID) {
	t.Helper()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	n, err := stores.UpsertNetwork(t.Context(), ircdb.Network{
		Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(t.Context(), stores, hub.New()), n.ID
}

func TestManagerCommandsReturnErrNotConnectedWhenNoClient(t *testing.T) {
	m, netID := newCommandTestManager(t)
	cases := []struct {
		name string
		fn   func() error
	}{
		{"Send", func() error { return m.Send(netID, "#chan", "hi") }},
		{"Join", func() error { return m.Join(netID, "#chan") }},
		{"Part", func() error { return m.Part(netID, "#chan", "bye") }},
		{"ChangeNick", func() error { return m.ChangeNick(netID, "newnick") }},
		{"Me", func() error { return m.Me(netID, "#chan", "waves") }},
		{"Topic", func() error { return m.Topic(netID, "#chan", "new topic") }},
		{"Whois", func() error { return m.Whois(netID, "alice") }},
		{"Invite", func() error { return m.Invite(netID, "alice", "#chan") }},
		{"Kick", func() error { return m.Kick(netID, "#chan", "alice", "bye") }},
		{"Mode", func() error { return m.Mode(netID, "#chan", "+o", "alice") }},
		{"Away", func() error { return m.Away(netID, "afk") }},
		{"Back", func() error { return m.Back(netID) }},
		{"Quit", func() error { return m.Quit(netID, "later") }},
		{"Rejoin", func() error { return m.Rejoin(netID, "#chan") }},
		{"Notice", func() error { return m.Notice(netID, "#chan", "fyi") }},
		{"CTCP", func() error { return m.CTCP(netID, "alice", "VERSION", "") }},
		{"Raw", func() error { return m.Raw(netID, "PING :x") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); !errors.Is(err, ErrNotConnected) {
				t.Fatalf("err = %v, want ErrNotConnected", err)
			}
		})
	}
}

func TestManagerSendLogOutboundFailsWithoutNick(t *testing.T) {
	m, netID := newCommandTestManager(t)
	// No runtime registered → Nick returns ""; LogOutbound rejects.
	if err := m.LogOutbound(context.Background(), netID, "#go", "privmsg", "hi"); err == nil {
		t.Fatal("expected error logging outbound without known nick")
	}
}

func TestManagerIsJoinedReflectsSetJoined(t *testing.T) {
	m, netID := newCommandTestManager(t)
	if m.IsJoined(netID, "#test") {
		t.Fatal("expected fresh manager to report not joined")
	}
	m.setJoined(netID, "#test", true)
	if !m.IsJoined(netID, "#test") {
		t.Fatal("expected joined=true after setJoined")
	}
	m.setJoined(netID, "#test", false)
	if m.IsJoined(netID, "#test") {
		t.Fatal("expected joined=false after setJoined(false)")
	}
}

func TestManagerChannelMembersFallsBackToFixtures(t *testing.T) {
	m, netID := newCommandTestManager(t)
	// No connection → ChannelMembers returns the fixture slice. Empty for a
	// fresh manager.
	if got := m.ChannelMembers(netID, "#test"); got != nil {
		t.Fatalf("expected nil fixture members, got %+v", got)
	}
	if got := m.ChannelMembers(netID, ""); got != nil {
		t.Fatalf("empty channel should yield nil, got %+v", got)
	}
}

func TestManagerNickFallsBackToRuntimeConfig(t *testing.T) {
	m, netID := newCommandTestManager(t)
	// Nick should be empty: no runtime has been registered yet.
	if got := m.Nick(netID); got != "" {
		t.Fatalf("Nick = %q, want empty for unregistered network", got)
	}
}
