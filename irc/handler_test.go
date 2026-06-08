package irc

import (
	"testing"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

type testHandlerFixture struct {
	Stores   *ircdb.MultiStore
	LogStore *ircdb.LogStore
	Handler  *handler
	Hub      *hub.Hub
	Network  ircdb.Network
}

type testHandlerConfig struct {
	name string
	nick string
	hub  *hub.Hub
}

type testHandlerOption func(*testHandlerConfig)

func withTestHandlerHub(h *hub.Hub) testHandlerOption {
	return func(cfg *testHandlerConfig) {
		cfg.hub = h
	}
}

type handlerMessage struct {
	BufferName string
	BufferKind string
	Kind       string
	Sender     string
	Target     string
	Content    string
}

func newTestHandlerFixture(t *testing.T, opts ...testHandlerOption) *testHandlerFixture {
	t.Helper()

	cfg := testHandlerConfig{name: "fake", nick: "tester"}
	for _, opt := range opts {
		opt(&cfg)
	}

	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	})

	network, err := stores.UpsertNetwork(t.Context(), ircdb.Network{Name: cfg.name, Host: "127.0.0.1", Port: 6667, Nick: cfg.nick})
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(network.ID)
	if err != nil {
		t.Fatal(err)
	}

	h := &handler{stores: stores, db: logStore.DB, hub: cfg.hub, networkID: network.ID, networkName: network.Name, userChannels: newUserChannels()}
	return &testHandlerFixture{Stores: stores, LogStore: logStore, Handler: h, Hub: cfg.hub, Network: network}
}

func lastHandlerMessage(t *testing.T, f *testHandlerFixture) handlerMessage {
	t.Helper()
	var msg handlerMessage
	if err := f.LogStore.DB.QueryRow(`
		SELECT b.name, b.kind, m.kind, COALESCE(m.sender, ''), COALESCE(m.target, ''), COALESCE(m.content, '')
		FROM messages m
		JOIN buffers b ON b.id = m.buffer_id
		ORDER BY m.id DESC
		LIMIT 1`).Scan(&msg.BufferName, &msg.BufferKind, &msg.Kind, &msg.Sender, &msg.Target, &msg.Content); err != nil {
		t.Fatal(err)
	}
	return msg
}

func handlerMessageCount(t *testing.T, f *testHandlerFixture) int {
	t.Helper()
	var count int
	if err := f.LogStore.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func drainEvents(events <-chan any) []any {
	var drained []any
	for {
		select {
		case ev := <-events:
			drained = append(drained, ev)
		default:
			return drained
		}
	}
}

func hasPresence(events <-chan any, nick, state string) bool {
	for _, ev := range drainEvents(events) {
		presence, ok := ev.(*PresenceEvent)
		if ok && presence.Nick == nick && presence.State == state {
			return true
		}
	}
	return false
}

func hasBufferUpdate(events <-chan any, _ string, joined bool) bool {
	for _, ev := range drainEvents(events) {
		update, ok := ev.(*BufferUpdateEvent)
		if ok && update.Joined == joined {
			return true
		}
	}
	return false
}

func hasTopicUpdate(events <-chan any, topic string) bool {
	for _, ev := range drainEvents(events) {
		update, ok := ev.(*BufferUpdateEvent)
		if ok && update.Topic == topic {
			return true
		}
	}
	return false
}
