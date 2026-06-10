package api

import (
	"testing"

	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
)

// blueskyNetwork creates a non-IRC (bluesky) network with one channel buffer.
func (ts *testWSServer) blueskyNetwork(t *testing.T) (networkID, bufferID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	n, err := ts.stores.UpsertNetwork(ctx, ircdb.Network{
		Name: "BskyNet", Host: "bsky.test.local", Port: 443, Nick: "tester",
		Kind: ircdb.NetworkKindBluesky,
	})
	if err != nil {
		t.Fatal(err)
	}
	bufID, _, _, err := ts.stores.EnsureBuffer(ctx, n.ID, "#feed", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	return n.ID, bufID
}

func TestWSCmdInputValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	_, bufID := ts.defaultNetwork(t)
	c := ts.dial(t)

	t.Run("missing buffer_id", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "input", ReqID: "i1", Content: "hello"})
		checkAckOrErr(t, ctx, c, "i1", "input requires buffer_id and content")
	})
	t.Run("blank content", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "input", ReqID: "i2", BufferID: bufID, Content: "   "})
		checkAckOrErr(t, ctx, c, "i2", "input requires buffer_id and content")
	})
	t.Run("unknown buffer", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "input", ReqID: "i3", BufferID: uuid.Must(uuid.NewV7()), Content: "hello"})
		checkAckOrErr(t, ctx, c, "i3", "unknown buffer")
	})
	t.Run("unknown slash command", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "input", ReqID: "i4", BufferID: bufID, Content: "/bogus arg"})
		env := recvErr(t, ctx, c)
		if env.ReqID != "i4" || env.Message == "" {
			t.Fatalf("expected parse error for /bogus, got %+v", env)
		}
	})
	assertNotCalled(t, mm.callRecorder, "Send")
}

func TestWSCmdInputPlainTextDispatchesSend(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, bufID := ts.defaultNetwork(t)

	c := ts.dial(t)
	sendCmd(t, ctx, c, clientCmd{Type: "input", ReqID: "i5", BufferID: bufID, Content: "hello there"})
	checkAckOrErr(t, ctx, c, "i5", "")
	assertCalledWith(t, mm.callRecorder, "Send", nID, "#test", "hello there")
	assertCalledWith(t, mm.callRecorder, "LogOutbound", nID, "#test", "privmsg", "hello there")
}

func TestWSCmdInputSlashCommandDispatches(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, bufID := ts.defaultNetwork(t)

	c := ts.dial(t)
	sendCmd(t, ctx, c, clientCmd{Type: "input", ReqID: "i6", BufferID: bufID, Content: "/join #other"})
	checkAckOrErr(t, ctx, c, "i6", "")
	assertCalledWith(t, mm.callRecorder, "Join", nID, "#other")
}

func TestWSCmdInputBlockedOnNonIRCNetwork(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	_, bufID := ts.blueskyNetwork(t)

	c := ts.dial(t)
	// Plain text parses to "send", which is not in commandsAllowedForNonIRC.
	sendCmd(t, ctx, c, clientCmd{Type: "input", ReqID: "i7", BufferID: bufID, Content: "hello"})
	checkAckOrErr(t, ctx, c, "i7", "command not supported on bluesky networks")
	assertNotCalled(t, mm.callRecorder, "Send")

	// Slash commands are IRC mutations and must be blocked too.
	sendCmd(t, ctx, c, clientCmd{Type: "input", ReqID: "i8", BufferID: bufID, Content: "/join #nope"})
	checkAckOrErr(t, ctx, c, "i8", "command not supported on bluesky networks")
	assertNotCalled(t, mm.callRecorder, "Join")
}
