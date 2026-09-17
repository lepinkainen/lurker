package api

import (
	"encoding/json"
	"testing"

	ircdb "github.com/lepinkainen/lurker/db"
)

func TestWSCmdModeValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, bufID := ts.defaultNetwork(t)
	statusBufID := ts.defaultStatusBuffer(t, nID)
	c := ts.dial(t)

	t.Run("missing content", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "mode", ReqID: "m1", BufferID: bufID})
		checkAckOrErr(t, ctx, c, "m1", "mode requires buffer_id and content")
	})
	t.Run("status buffer rejected", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "mode", ReqID: "m2", BufferID: statusBufID, Content: "+m"})
		checkAckOrErr(t, ctx, c, "m2", "mode only works on channel buffers")
	})
	assertNotCalled(t, mm.callRecorder, "Mode")
}

func TestWSCmdModeDispatch(t *testing.T) {
	ctx := t.Context()

	t.Run("modes with params split into fields", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)

		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "mode", ReqID: "m3", BufferID: bufID, Content: "+ov alice bob"})
		checkAckOrErr(t, ctx, c, "m3", "")
		assertCalledWith(t, mm.callRecorder, "Mode", nID, "#test", "+ov", "alice", "bob")
	})

	t.Run("bare mode without params", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)

		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "mode", ReqID: "m4", BufferID: bufID, Content: "+m"})
		checkAckOrErr(t, ctx, c, "m4", "")
		assertCalledWith(t, mm.callRecorder, "Mode", nID, "#test", "+m")
	})

	t.Run("manager error bubbles", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		_, bufID := ts.defaultNetwork(t)
		mm.setError("Mode", errMockNotConnected)

		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "mode", ReqID: "m5", BufferID: bufID, Content: "+m"})
		checkAckOrErr(t, ctx, c, "m5", errMockNotConnected.Error())
	})
}

func TestWSCmdKickbanDispatch(t *testing.T) {
	ctx := t.Context()

	t.Run("bans then kicks with wildcard mask", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)

		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "kickban", ReqID: "kb1", BufferID: bufID, Target: "alice", Content: "spamming"})
		checkAckOrErr(t, ctx, c, "kb1", "")
		assertCalledWith(t, mm.callRecorder, "Mode", nID, "#test", "+b", "alice!*@*")
		assertCalledWith(t, mm.callRecorder, "Kick", nID, "#test", "alice", "spamming")
	})

	t.Run("ban failure skips kick", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		_, bufID := ts.defaultNetwork(t)
		mm.setError("Mode", errMockNotConnected)

		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "kickban", ReqID: "kb2", BufferID: bufID, Target: "alice"})
		checkAckOrErr(t, ctx, c, "kb2", errMockNotConnected.Error())
		assertNotCalled(t, mm.callRecorder, "Kick")
	})

	t.Run("missing target rejected", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		_, bufID := ts.defaultNetwork(t)

		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "kickban", ReqID: "kb3", BufferID: bufID})
		checkAckOrErr(t, ctx, c, "kb3", "kickban requires buffer_id and target")
		assertNotCalled(t, mm.callRecorder, "Mode")
	})
}

func TestWSCmdIgnoreRoundtrip(t *testing.T) {
	ctx := t.Context()
	mgrOpt, _ := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)
	c := ts.dial(t)

	t.Run("validation", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "ignore", ReqID: "g0", Target: "spammer!*@*"})
		checkAckOrErr(t, ctx, c, "g0", "ignore requires network_id and target")
	})

	t.Run("ignore persists mask", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "ignore", ReqID: "g1", NetworkID: nID, Target: "spammer!*@*"})
		checkAckOrErr(t, ctx, c, "g1", "")
		entries, err := ircdb.ListIgnores(ctx, ts.stores.Control, nID)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Mask != "spammer!*@*" || entries[0].Level != ircdb.IgnoreLevelHide {
			t.Fatalf("stored entries = %v, want [{spammer!*@* hide}]", entries)
		}
	})

	t.Run("ignorelist returns mask", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "ignorelist", ReqID: "g2", NetworkID: nID})
		raw := recvSkipBufferUpdate(t, ctx, c)
		var res ignoreListResult
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatalf("decode ignorelist_result: %v (raw=%s)", err, raw)
		}
		if res.Type != "ignorelist_result" || res.ReqID != "g2" || res.NetworkID != nID {
			t.Fatalf("unexpected envelope: %+v", res)
		}
		if len(res.Entries) != 1 || res.Entries[0].Mask != "spammer!*@*" || res.Entries[0].Level != ircdb.IgnoreLevelHide {
			t.Fatalf("entries = %v, want [{spammer!*@* hide}]", res.Entries)
		}
	})

	t.Run("mute promotes existing mask", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "mute", ReqID: "g2b", NetworkID: nID, Target: "spammer!*@*"})
		checkAckOrErr(t, ctx, c, "g2b", "")
		entries, err := ircdb.ListIgnores(ctx, ts.stores.Control, nID)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Mask != "spammer!*@*" || entries[0].Level != ircdb.IgnoreLevelMute {
			t.Fatalf("entries after mute = %v, want [{spammer!*@* mute}]", entries)
		}
	})

	t.Run("unmute removes mask", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "unmute", ReqID: "g2c", NetworkID: nID, Target: "spammer!*@*"})
		checkAckOrErr(t, ctx, c, "g2c", "")
		entries, err := ircdb.ListIgnores(ctx, ts.stores.Control, nID)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("entries after unmute = %v, want empty", entries)
		}
	})

	t.Run("re-ignore for unignore test", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "ignore", ReqID: "g2d", NetworkID: nID, Target: "spammer!*@*"})
		checkAckOrErr(t, ctx, c, "g2d", "")
	})

	t.Run("unignore removes mask", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "unignore", ReqID: "g3", NetworkID: nID, Target: "spammer!*@*"})
		checkAckOrErr(t, ctx, c, "g3", "")
		entries, err := ircdb.ListIgnores(ctx, ts.stores.Control, nID)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("entries after unignore = %v, want empty", entries)
		}
	})

	t.Run("ignorelist empty is non-nil array", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "ignorelist", ReqID: "g4", NetworkID: nID})
		raw := recvSkipBufferUpdate(t, ctx, c)
		var res ignoreListResult
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatalf("decode ignorelist_result: %v (raw=%s)", err, raw)
		}
		if res.Entries == nil || len(res.Entries) != 0 {
			t.Fatalf("entries = %#v, want empty non-nil slice", res.Entries)
		}
	})

	t.Run("mutelist alias returns entries", func(t *testing.T) {
		sendCmd(t, ctx, c, clientCmd{Type: "mute", ReqID: "g5a", NetworkID: nID, Target: "botty"})
		checkAckOrErr(t, ctx, c, "g5a", "")
		sendCmd(t, ctx, c, clientCmd{Type: "mutelist", ReqID: "g5", NetworkID: nID})
		raw := recvSkipBufferUpdate(t, ctx, c)
		var res ignoreListResult
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatalf("decode ignorelist_result: %v (raw=%s)", err, raw)
		}
		if res.Type != "ignorelist_result" || len(res.Entries) != 1 || res.Entries[0].Mask != "botty" || res.Entries[0].Level != ircdb.IgnoreLevelMute {
			t.Fatalf("mutelist entries = %+v, want [{botty mute}]", res.Entries)
		}
	})
}
