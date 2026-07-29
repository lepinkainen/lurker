package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
)

// archiveTestBuffer flips the archived flag directly in the store.
func archiveTestBuffer(t *testing.T, ts *testWSServer, bufID uuid.UUID) {
	t.Helper()
	archived := true
	if _, err := ircdb.UpdateBufferSettings(t.Context(), ts.stores.Control, bufID, ircdb.BufferSettingsPatch{Archived: &archived}); err != nil {
		t.Fatal(err)
	}
}

// recvAll reads WS messages until one of each wanted type has arrived,
// returning them keyed by type. The direct ack write and the hub broadcast
// race on the wire, so waiting for them one at a time can swallow the other.
func recvAll(t *testing.T, ctx context.Context, c *websocket.Conn, wants ...string) map[string]json.RawMessage {
	t.Helper()
	pending := map[string]bool{}
	for _, w := range wants {
		pending[w] = true
	}
	got := map[string]json.RawMessage{}
	for range 16 {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, c, &raw); err != nil {
			t.Fatal(err)
		}
		var typ struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &typ); err != nil {
			t.Fatal(err)
		}
		if typ.Type == "error" && !pending["error"] {
			t.Fatalf("unexpected error envelope while waiting for %v: %s", wants, raw)
		}
		if pending[typ.Type] {
			got[typ.Type] = raw
			delete(pending, typ.Type)
			if len(pending) == 0 {
				return got
			}
		}
	}
	t.Fatalf("never received all of %v; still missing %v", wants, pending)
	return nil
}

func TestWSCmdDeleteBufferValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, _ := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, chanID := ts.defaultNetwork(t)
	statusID := ts.defaultStatusBuffer(t, nID)
	unknownID := uuid.Must(uuid.NewV7())

	cases := []struct {
		name, wantErr string
		cmd           clientCmd
	}{
		{"missing buffer_id", "delete_buffer requires buffer_id", clientCmd{Type: "delete_buffer", ReqID: "d1"}},
		{"unknown buffer", "unknown buffer", clientCmd{Type: "delete_buffer", ReqID: "d2", BufferID: unknownID}},
		{"status buffer", "cannot delete status buffers", clientCmd{Type: "delete_buffer", ReqID: "d3", BufferID: statusID}},
		{"not archived", "must be archived", clientCmd{Type: "delete_buffer", ReqID: "d4", BufferID: chanID}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			checkAckOrErr(t, ctx, c, tt.cmd.ReqID, tt.wantErr)
		})
	}

	// All rejections above must have left the channel buffer intact.
	if _, _, _, err := ts.stores.LookupBuffer(ctx, chanID); err != nil {
		t.Fatalf("channel buffer was deleted by a rejected command: %v", err)
	}
}

func TestWSCmdDeleteBufferSuccess(t *testing.T) {
	ctx := t.Context()
	mgrOpt, _ := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, chanID := ts.defaultNetwork(t)
	archiveTestBuffer(t, ts, chanID)

	c := ts.dial(t)
	sendCmd(t, ctx, c, clientCmd{Type: "delete_buffer", ReqID: "d5", BufferID: chanID})

	// Both the direct ack and the hub broadcast must arrive; order between
	// the two writers is not guaranteed.
	got := recvAll(t, ctx, c, "buffer_deleted", "ack")
	var ev bufferDeletedEvent
	if err := json.Unmarshal(got["buffer_deleted"], &ev); err != nil {
		t.Fatal(err)
	}
	if ev.ID != chanID || ev.NetworkID != nID {
		t.Fatalf("buffer_deleted = %+v, want id=%s network=%s", ev, chanID, nID)
	}

	if _, _, _, err := ts.stores.LookupBuffer(ctx, chanID); err == nil {
		t.Fatal("buffer still exists after delete_buffer ack")
	}
}

func TestWSCmdDeleteBufferQuery(t *testing.T) {
	ctx := t.Context()
	mgrOpt, _ := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)
	queryID, _, _, err := ts.stores.EnsureBuffer(ctx, nID, "spammer", ircdb.BufferQuery)
	if err != nil {
		t.Fatal(err)
	}
	archiveTestBuffer(t, ts, queryID)

	c := ts.dial(t)
	sendCmd(t, ctx, c, clientCmd{Type: "delete_buffer", ReqID: "d6", BufferID: queryID})
	recvAll(t, ctx, c, "buffer_deleted", "ack")

	if _, _, _, err := ts.stores.LookupBuffer(ctx, queryID); err == nil {
		t.Fatal("query buffer still exists after delete")
	}
}

func TestWSCmdArchiveUnarchiveBuffer(t *testing.T) {
	ctx := t.Context()
	mgrOpt, _ := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)
	statusID := ts.defaultStatusBuffer(t, nID)
	queryID, _, _, err := ts.stores.EnsureBuffer(ctx, nID, "friend", ircdb.BufferQuery)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("archive query", func(t *testing.T) {
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "archive_buffer", ReqID: "a1", BufferID: queryID})
		got := recvAll(t, ctx, c, "buffer_settings", "ack")
		var ev bufferSettingsEvent
		if err := json.Unmarshal(got["buffer_settings"], &ev); err != nil {
			t.Fatal(err)
		}
		if ev.ID != queryID || !ev.Archived {
			t.Fatalf("buffer_settings = %+v, want archived=true for %s", ev, queryID)
		}
	})

	t.Run("unarchive query", func(t *testing.T) {
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "unarchive_buffer", ReqID: "a2", BufferID: queryID})
		got := recvAll(t, ctx, c, "buffer_settings", "ack")
		var ev bufferSettingsEvent
		if err := json.Unmarshal(got["buffer_settings"], &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Archived {
			t.Fatalf("buffer_settings = %+v, want archived=false", ev)
		}
	})

	t.Run("status buffer rejected", func(t *testing.T) {
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "archive_buffer", ReqID: "a3", BufferID: statusID})
		checkAckOrErr(t, ctx, c, "a3", "status buffers cannot be archived")
	})
}
