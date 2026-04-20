//go:build integration

package irc

import (
	"testing"
	"time"

	"github.com/lepinkainen/research/irc-service/db"
)

func TestManagerStartAndStopNetworkIndividually(t *testing.T) {
	stores, err := db.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	srv1 := newFakeIRC(t)
	defer srv1.close()
	h1, p1 := srv1.addr()
	srv2 := newFakeIRC(t)
	defer srv2.close()
	h2, p2 := srv2.addr()

	ctx := t.Context()
	mgr := NewManager(stores, nil)

	n1, err := stores.UpsertNetwork(ctx, db.Network{Name: "NetOne", Host: h1, Port: p1, TLS: false, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	n2, err := stores.UpsertNetwork(ctx, db.Network{Name: "NetTwo", Host: h2, Port: p2, TLS: false, Nick: "b"})
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.StartNetwork(ctx, n1.ID, NetworkConfig{Name: n1.Name, Servers: []ServerConfig{{Host: h1, Port: p1, TLS: false}}, Nick: "a", User: "a", Realname: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.StartNetwork(ctx, n2.ID, NetworkConfig{Name: n2.Name, Servers: []ServerConfig{{Host: h2, Port: p2, TLS: false}}, Nick: "b", User: "b", Realname: "b"}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-srv1.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("net1 never connected")
	}
	select {
	case <-srv2.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("net2 never connected")
	}

	waitFor(t, 3*time.Second, func() bool {
		state := mgr.StateSnapshot()
		return state[n1.ID] == "connected" && state[n2.ID] == "connected"
	}, "networks never reached connected state")

	if err := mgr.StopNetwork(n1.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		state := mgr.StateSnapshot()
		return state[n1.ID] == "disconnected" && state[n2.ID] == "connected"
	}, "stop network affected wrong runtime state")

	mgr.Wait()
}
