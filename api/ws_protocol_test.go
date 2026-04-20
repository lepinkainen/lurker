package api

import (
	"testing"

	ircdb "github.com/lepinkainen/research/irc-service/db"
)

func TestJoinUsesNetworkIDChannelAndPartUsesBufferID(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	n, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	bufferID, _, _, err := stores.UpsertBufferRegistry(ctx, n.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}

	if n.ID == 0 || bufferID == 0 {
		t.Fatal("expected non-zero ids")
	}
}
