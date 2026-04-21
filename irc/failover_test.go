//go:build integration

package irc

import (
	"testing"
	"time"

	"github.com/lepinkainen/lurker/db"
)

func TestYAMLStyleNetworkUsesOneLogicalConnectionConfigWithMultipleServers(t *testing.T) {
	stores, err := db.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	srv := newFakeIRC(t)
	defer srv.close()
	host, port := srv.addr()

	ctx := t.Context()

	mgr := NewManager(stores, nil)
	err = mgr.Start(ctx, []NetworkConfig{{
		Name: "Ircnet",
		Servers: []ServerConfig{
			{Host: "127.0.0.1", Port: 1, TLS: false},
			{Host: host, Port: port, TLS: false},
		},
		Nick: "tester", User: "tester", Realname: "tester",
	}})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-srv.ready:
	case <-time.After(4 * time.Second):
		t.Fatal("server never saw failover registration")
	}

	nets, err := db.ListNetworks(t.Context(), stores.Control)
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 1 {
		t.Fatalf("networks len = %d, want 1", len(nets))
	}

	mgr.Wait()
}
