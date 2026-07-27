package irc

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lepinkainen/lurker/hub"
	"github.com/lrstanley/girc"
)

// startMockClient connects a girc client over net.Pipe so Cmd.* calls write
// observable lines. Returns the client and a channel of raw lines the client
// sent. The reader goroutine ends when the pipe closes via t.Cleanup.
func startMockClient(t *testing.T) (*girc.Client, <-chan string) {
	t.Helper()
	client := girc.New(girc.Config{
		Server: "test.invalid", Port: 6667,
		Nick: "tester", User: "tester", Name: "tester",
		AllowFlood: true,
	})
	clientEnd, serverEnd := net.Pipe()
	go func() { _ = client.MockConnect(clientEnd) }()
	t.Cleanup(func() {
		client.Close()
		_ = serverEnd.Close()
	})

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(serverEnd)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	// Registration (NICK/USER/CAP) proves the client write loop is up before
	// the test fires handlers at it.
	awaitLine(t, lines, func(l string) bool {
		return strings.HasPrefix(l, "NICK ") || strings.HasPrefix(l, "USER ") || strings.HasPrefix(l, "CAP ")
	})
	return client, lines
}

func awaitLine(t *testing.T, lines <-chan string, match func(string) bool) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("connection closed before expected line")
			}
			if match(l) {
				return l
			}
		case <-deadline:
			t.Fatal("timed out waiting for expected line")
		}
	}
}

func TestOnConnectedSendsConnectCommandsThenAutojoin(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	hookCalled := false
	f.Handler.connectedHook = func(string) { hookCalled = true }
	f.Handler.connectCommands = []string{"PRIVMSG NickServ :IDENTIFY hunter2"}
	f.Handler.autojoin = []string{"#alpha", "#beta"}

	client, lines := startMockClient(t)
	f.Handler.onConnected(client, girc.Event{})

	identify := awaitLine(t, lines, func(l string) bool { return strings.Contains(l, "IDENTIFY hunter2") })
	var joins []string
	for len(joins) < 2 {
		joins = append(joins, awaitLine(t, lines, func(l string) bool { return strings.HasPrefix(l, "JOIN ") }))
	}
	if !strings.HasPrefix(identify, "PRIVMSG NickServ") {
		t.Fatalf("connect command line = %q, want PRIVMSG NickServ prefix", identify)
	}
	if joins[0] != "JOIN #alpha" || joins[1] != "JOIN #beta" {
		t.Fatalf("autojoin lines = %v, want [JOIN #alpha, JOIN #beta]", joins)
	}
	if !hookCalled {
		t.Fatal("connectedHook not called")
	}

	if !hasNetworkStateIn(drainEvents(events), StateConnected.String()) {
		t.Fatal("missing network_state connected event")
	}
	msg := lastHandlerMessage(t, f)
	if msg.Kind != StateConnected.String() || msg.BufferKind != "status" {
		t.Fatalf("status message = %+v, want kind=connected in status buffer", msg)
	}
}

func TestOnDisconnectedMarksChannelsPartedAndResetsState(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(32)
	defer unsub()

	var cleared []string
	f.Handler.clearMemberListHook = func(ch string) { cleared = append(cleared, ch) }
	f.Handler.drainJoinedHook = func() []string { return []string{"#alpha", "#beta"} }
	f.Handler.lastMemberListHash = map[string]uint64{"#alpha": 1}
	f.Handler.userChannels.addUser("alice", "#alpha")

	f.Handler.onDisconnected(nil, mustEvent(t, "ERROR :Closing Link"))

	drained := drainEvents(events)
	if !hasNetworkStateIn(drained, StateDisconnected.String()) {
		t.Fatal("missing network_state disconnected event")
	}
	if got := countBufferUpdatesJoined(drained, false); got != 2 {
		t.Fatalf("buffer_update joined=false count = %d, want 2", got)
	}
	if len(cleared) != 2 || cleared[0] != "#alpha" || cleared[1] != "#beta" {
		t.Fatalf("cleared member lists = %v, want [#alpha #beta]", cleared)
	}
	if f.Handler.lastMemberListHash != nil {
		t.Fatal("lastMemberListHash not reset")
	}
	if len(f.Handler.userChannels.byNick) != 0 {
		t.Fatal("userChannels not reset")
	}

	msg := lastHandlerMessage(t, f)
	if msg.Kind != StateDisconnected.String() || msg.BufferKind != "status" || msg.Content != "Closing Link" {
		t.Fatalf("status message = %+v, want disconnected/Closing Link in status buffer", msg)
	}
}

func TestOnDisconnectedWithoutJoinedChannelsStillLogsStatus(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onDisconnected(nil, mustEvent(t, "ERROR :Ping timeout"))

	drained := drainEvents(events)
	if !hasNetworkStateIn(drained, StateDisconnected.String()) {
		t.Fatal("missing network_state disconnected event")
	}
	if got := countBufferUpdatesJoined(drained, false); got != 0 {
		t.Fatalf("buffer_update count = %d, want 0 without drainJoinedHook", got)
	}
	msg := lastHandlerMessage(t, f)
	if msg.Kind != StateDisconnected.String() || msg.Content != "Ping timeout" {
		t.Fatalf("status message = %+v, want disconnected/Ping timeout", msg)
	}
}

func hasNetworkStateIn(evs []any, state string) bool {
	for _, ev := range evs {
		if ns, ok := ev.(*NetworkStateEvent); ok && ns.State == state {
			return true
		}
	}
	return false
}

func countBufferUpdatesJoined(evs []any, joined bool) int {
	n := 0
	for _, ev := range evs {
		if bu, ok := ev.(*BufferUpdateEvent); ok && bu.Joined == joined {
			n++
		}
	}
	return n
}
