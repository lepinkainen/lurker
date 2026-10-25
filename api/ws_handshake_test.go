package api

import (
	"encoding/json"
	"testing"

	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

// publishMessage stores a message and publishes its event the way the IRC
// handler does (one hub.Do), returning the event's stream position.
func (ts *testWSServer) publishMessage(t *testing.T, nID, bufID uuid.UUID, content string) uint64 {
	t.Helper()
	logStore, err := ts.stores.LogStore(nID)
	if err != nil {
		t.Fatal(err)
	}
	ev := &irc.MessageEvent{Type: "message"}
	err = ts.hub.Do(func(publish func(any)) error {
		id, ts, _, err := ircdb.InsertLogMessage(t.Context(), logStore, ircdb.LogMessageInput{
			BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: content,
		})
		if err != nil {
			return err
		}
		ev.ID, ev.NetworkID, ev.BufferID, ev.TS, ev.Sender, ev.Kind, ev.Content = id, nID, bufID, ts, "alice", "privmsg", content
		publish(ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev.Seq
}

type seqFrame struct {
	Type    string `json:"type"`
	Seq     uint64 `json:"seq"`
	Content string `json:"content"`
}

func TestHandshakeSnapshotCarriesStreamPosition(t *testing.T) {
	ts := newTestWSServer(t)
	nID, bufID := ts.defaultNetwork(t)
	seq := ts.publishMessage(t, nID, bufID, "one")

	_, snap := ts.dialSince(t, 0)
	if snap.Seq != seq {
		t.Fatalf("state.seq = %d, want %d (the last published event)", snap.Seq, seq)
	}
	if got := snap.InitialMessages[bufID.String()]; len(got) != 1 || got[0].Content != "one" {
		t.Fatalf("snapshot messages = %+v", got)
	}
}

func TestHandshakeReplaysFromCursorThenStreamsLive(t *testing.T) {
	ctx := t.Context()
	ts := newTestWSServer(t)
	nID, bufID := ts.defaultNetwork(t)
	seq1 := ts.publishMessage(t, nID, bufID, "one")
	ts.publishMessage(t, nID, bufID, "two")
	ts.publishMessage(t, nID, bufID, "three")

	c, _ := ts.dialSince(t, seq1)
	for i, want := range []string{"two", "three"} {
		var f seqFrame
		if err := wsjson.Read(ctx, c, &f); err != nil {
			t.Fatal(err)
		}
		if f.Type != "message" || f.Content != want || f.Seq != seq1+uint64(i)+1 {
			t.Fatalf("replayed frame %d = %+v, want %q at seq %d", i, f, want, seq1+uint64(i)+1)
		}
	}
	seq4 := ts.publishMessage(t, nID, bufID, "four")
	var f seqFrame
	if err := wsjson.Read(ctx, c, &f); err != nil {
		t.Fatal(err)
	}
	if f.Content != "four" || f.Seq != seq4 {
		t.Fatalf("live frame after replay = %+v, want four at seq %d", f, seq4)
	}
}

func TestHandshakeUnknownCursorFallsBackToSnapshot(t *testing.T) {
	ctx := t.Context()
	ts := newTestWSServer(t)
	nID, bufID := ts.defaultNetwork(t)
	ts.publishMessage(t, nID, bufID, "one")

	for _, since := range []uint64{1, ts.hub.Seq() + 1000} {
		c, _ := ts.dialSince(t, since)
		var raw json.RawMessage
		if err := wsjson.Read(ctx, c, &raw); err != nil {
			t.Fatal(err)
		}
		var f seqFrame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		if f.Type != "state" || f.Seq != ts.hub.Seq() {
			t.Fatalf("since=%d: first frame = %s, want a state snapshot at the current seq", since, raw)
		}
	}
}

func TestHandshakeRequiresHello(t *testing.T) {
	ctx := t.Context()
	ts := newTestWSServer(t)
	c := dialRaw(t, ts)
	sendCmd(t, ctx, c, clientCmd{Type: "history", ReqID: "r1", BufferID: uuid.Must(uuid.NewV7())})
	env := recvErr(t, ctx, c)
	if env.Type != "error" || env.ReqID != "r1" {
		t.Fatalf("expected an error envelope for a non-hello first command, got %+v", env)
	}
}
