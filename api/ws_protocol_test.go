package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
	"github.com/lepinkainen/lurker/irc"
)

// ---------------------------------------------------------------------------
// Test doubles for the api.manager surface
//
// The IRC manager surface is wide (5 + 7 + 5 + 2 + 4 + 3 methods). Rather
// than one giant mock, each cohesive sub-interface gets its own small mock
// that shares a single callRecorder. Each call records its positional args
// so dispatch tests can assert the manager was invoked with the right
// values, not just the right method name.
// ---------------------------------------------------------------------------

// callEntry captures one method invocation and its positional args.
type callEntry struct {
	Name string
	Args []any
}

// callRecorder is the shared call log + error-injection state used by every
// sub-mock. Goroutine-safe.
type callRecorder struct {
	mu           sync.Mutex
	calls        []callEntry
	returnErrors map[string]error
}

func newCallRecorder() *callRecorder {
	return &callRecorder{returnErrors: map[string]error{}}
}

// record stores the call and returns the configured error (nil if unset).
func (r *callRecorder) record(name string, args ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, callEntry{Name: name, Args: args})
	return r.returnErrors[name]
}

// setError configures method `name` to return `err`. Empty name clears all.
func (r *callRecorder) setError(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" {
		for k := range r.returnErrors {
			delete(r.returnErrors, k)
		}
		return
	}
	r.returnErrors[name] = err
}

func (r *callRecorder) called(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (r *callRecorder) callNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.calls))
	for i, c := range r.calls {
		names[i] = c.Name
	}
	return names
}

// lastArgs returns the args of the most recent call to `name`.
func (r *callRecorder) lastArgs(name string) ([]any, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.calls) - 1; i >= 0; i-- {
		if r.calls[i].Name == name {
			return r.calls[i].Args, true
		}
	}
	return nil, false
}

// assertCalledWith verifies the last call to `name` matches the given args.
func assertCalledWith(t *testing.T, rec *callRecorder, name string, want ...any) {
	t.Helper()
	args, ok := rec.lastArgs(name)
	if !ok {
		t.Fatalf("%s not called; calls=%v", name, rec.callNames())
	}
	if len(args) != len(want) {
		t.Fatalf("%s arg count = %d (%v), want %d (%v)", name, len(args), args, len(want), want)
	}
	for i, w := range want {
		if args[i] != w {
			t.Fatalf("%s arg[%d] = %v, want %v", name, i, args[i], w)
		}
	}
}

// assertNotCalled verifies `name` was never invoked.
func assertNotCalled(t *testing.T, rec *callRecorder, name string) {
	t.Helper()
	if rec.called(name) {
		t.Fatalf("%s should not have been called; calls=%v", name, rec.callNames())
	}
}

// errMockNotConnected is the canonical "not connected" error for tests.
var errMockNotConnected = errors.New("irc: network not connected")

// mockMessenger satisfies messageSender (5 methods).
type mockMessenger struct{ *callRecorder }

func (m *mockMessenger) Send(networkID uuid.UUID, target, content string) error {
	return m.record("Send", networkID, target, content)
}

func (m *mockMessenger) Me(networkID uuid.UUID, target, message string) error {
	return m.record("Me", networkID, target, message)
}

func (m *mockMessenger) Notice(networkID uuid.UUID, target, content string) error {
	return m.record("Notice", networkID, target, content)
}

func (m *mockMessenger) CTCP(networkID uuid.UUID, nick, command, args string) error {
	return m.record("CTCP", networkID, nick, command, args)
}

func (m *mockMessenger) LogOutbound(_ context.Context, networkID uuid.UUID, target, kind, content string) error {
	return m.record("LogOutbound", networkID, target, kind, content)
}

// mockChannels satisfies channelOps (7 methods).
type mockChannels struct{ *callRecorder }

func (m *mockChannels) Join(networkID uuid.UUID, channel string) error {
	return m.record("Join", networkID, channel)
}

func (m *mockChannels) Part(networkID uuid.UUID, channel, reason string) error {
	return m.record("Part", networkID, channel, reason)
}

func (m *mockChannels) Rejoin(networkID uuid.UUID, channel string) error {
	return m.record("Rejoin", networkID, channel)
}

func (m *mockChannels) Topic(networkID uuid.UUID, channel, topic string) error {
	return m.record("Topic", networkID, channel, topic)
}

func (m *mockChannels) Invite(networkID uuid.UUID, nick, channel string) error {
	return m.record("Invite", networkID, nick, channel)
}

func (m *mockChannels) Kick(networkID uuid.UUID, channel, nick, reason string) error {
	return m.record("Kick", networkID, channel, nick, reason)
}

func (m *mockChannels) ListChannels(networkID uuid.UUID, filter string) error {
	return m.record("ListChannels", networkID, filter)
}

// mockPresence satisfies presenceOps (5 methods).
type mockPresence struct{ *callRecorder }

func (m *mockPresence) ChangeNick(networkID uuid.UUID, nick string) error {
	return m.record("ChangeNick", networkID, nick)
}

func (m *mockPresence) Whois(networkID uuid.UUID, nick string) error {
	return m.record("Whois", networkID, nick)
}

func (m *mockPresence) Away(networkID uuid.UUID, message string) error {
	return m.record("Away", networkID, message)
}

func (m *mockPresence) Back(networkID uuid.UUID) error {
	return m.record("Back", networkID)
}

func (m *mockPresence) Quit(networkID uuid.UUID, message string) error {
	return m.record("Quit", networkID, message)
}

// mockMode satisfies modeOps (2 methods).
type mockMode struct{ *callRecorder }

func (m *mockMode) Mode(networkID uuid.UUID, target, modes string, params ...string) error {
	args := []any{networkID, target, modes}
	for _, p := range params {
		args = append(args, p)
	}
	return m.record("Mode", args...)
}

func (m *mockMode) Raw(networkID uuid.UUID, line string) error {
	return m.record("Raw", networkID, line)
}

// mockState satisfies stateManager (4 methods). State methods do not return
// errors and only record the call for visibility.
type mockState struct{ *callRecorder }

func (m *mockState) StateSnapshot() map[uuid.UUID]string {
	_ = m.record("StateSnapshot")
	return nil
}

func (m *mockState) IsJoined(networkID uuid.UUID, channel string) bool {
	_ = m.record("IsJoined", networkID, channel)
	return false
}

func (m *mockState) ChannelMembers(networkID uuid.UUID, channel string) []ircdb.ChannelMember {
	_ = m.record("ChannelMembers", networkID, channel)
	return nil
}

func (m *mockState) Nick(networkID uuid.UUID) string {
	_ = m.record("Nick", networkID)
	return ""
}

// mockLifecycle satisfies the StartNetwork/StopNetwork portion of
// networkManager. StateSnapshot is supplied by mockState.
type mockLifecycle struct{ *callRecorder }

func (m *mockLifecycle) StartNetwork(_ context.Context, networkID uuid.UUID, cfg irc.NetworkConfig) error {
	return m.record("StartNetwork", networkID, cfg)
}

func (m *mockLifecycle) StopNetwork(networkID uuid.UUID) error {
	return m.record("StopNetwork", networkID)
}

// mockManager composes the per-feature mocks into the full api.manager
// surface. Every interface method comes from an embedded sub-mock backed by
// the shared callRecorder.
type mockManager struct {
	*callRecorder
	*mockMessenger
	*mockChannels
	*mockPresence
	*mockMode
	*mockState
	*mockLifecycle
}

// Guard: verify *mockManager satisfies the composed API manager interface at compile time.
var _ manager = (*mockManager)(nil)

func newMockManager() *mockManager {
	rec := newCallRecorder()
	return &mockManager{
		callRecorder:  rec,
		mockMessenger: &mockMessenger{rec},
		mockChannels:  &mockChannels{rec},
		mockPresence:  &mockPresence{rec},
		mockMode:      &mockMode{rec},
		mockState:     &mockState{rec},
		mockLifecycle: &mockLifecycle{rec},
	}
}

// ---------------------------------------------------------------------------
// testWSServer — shared test harness
// ---------------------------------------------------------------------------

// testWSServer bundles the resources needed for WebSocket integration
// tests. It creates real SQLite stores, a hub, and an optional mock
// manager. Tests use the HTTP server URL to dial WebSocket.
type testWSServer struct {
	stores  *ircdb.MultiStore
	hub     *hub.Hub
	mockMgr *mockManager // non-nil when withMockMgr was used
	httpSrv *httptest.Server
	wsURL   string
}

type testWSOption func(*Server)

func newTestWSServer(t *testing.T, opts ...testWSOption) *testWSServer {
	t.Helper()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })

	h := hub.New()

	srv := &Server{Stores: stores, Hub: h}
	for _, o := range opts {
		o(srv)
	}
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	ts := &testWSServer{
		stores:  stores,
		hub:     h,
		httpSrv: httpSrv,
		wsURL:   "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/api/stream",
	}
	if mm, ok := srv.Manager.(*mockManager); ok {
		ts.mockMgr = mm
	}
	return ts
}

// withMockMgr sets up the server with a mock Manager. The returned
// *mockManager can be used to configure return errors and verify calls.
func withMockMgr(t *testing.T) (testWSOption, *mockManager) {
	t.Helper()
	mm := newMockManager()
	return func(s *Server) { s.Manager = mm }, mm
}

// defaultNetwork creates a test network and channel buffer, returning the
// network and buffer IDs.
func (ts *testWSServer) defaultNetwork(t *testing.T) (networkID, bufferID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	n, err := ts.stores.UpsertNetwork(ctx, ircdb.Network{
		Name: "TestNet", Host: "irc.test.local", Port: 6667, Nick: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	bufID, _, _, err := ts.stores.EnsureBuffer(ctx, n.ID, "#test", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	return n.ID, bufID
}

// defaultStatusBuffer creates the default status buffer for a network.
func (ts *testWSServer) defaultStatusBuffer(t *testing.T, networkID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	bufID, _, _, err := ts.stores.EnsureBuffer(ctx, networkID, "", ircdb.BufferStatus)
	if err != nil {
		t.Fatal(err)
	}
	return bufID
}

// dial connects a WebSocket client to the test server.
func (ts *testWSServer) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	c, resp, err := websocket.Dial(ctx, ts.wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(websocket.StatusNormalClosure, "") })
	return c
}

// sendCmd writes a JSON command to the WebSocket.
func sendCmd(t *testing.T, ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	t.Helper()
	if err := wsjson.Write(ctx, c, cmd); err != nil {
		t.Fatal(err)
	}
}

// recvSkipBufferUpdate reads the next message from the WebSocket,
// skipping any buffer_update events (which may arrive after mark_read).
func recvSkipBufferUpdate(t *testing.T, ctx context.Context, c *websocket.Conn) json.RawMessage {
	t.Helper()
	for {
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
		if typ.Type == "buffer_update" {
			continue
		}
		return raw
	}
}

// recvAck reads and returns the next ack envelope (skipping buffer_update).
func recvAck(t *testing.T, ctx context.Context, c *websocket.Conn) ackEnvelope {
	t.Helper()
	raw := recvSkipBufferUpdate(t, ctx, c)
	var env ackEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode ack: %v (raw=%s)", err, raw)
	}
	return env
}

// recvErr reads and returns the next error envelope.
func recvErr(t *testing.T, ctx context.Context, c *websocket.Conn) errorEnvelope {
	t.Helper()
	raw := recvSkipBufferUpdate(t, ctx, c)
	var env errorEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode error: %v (raw=%s)", err, raw)
	}
	return env
}

// recvHistoryResult reads and returns the next history_result.
func recvHistoryResult(t *testing.T, ctx context.Context, c *websocket.Conn) historyResult {
	t.Helper()
	raw := recvSkipBufferUpdate(t, ctx, c)
	var res historyResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("decode history_result: %v (raw=%s)", err, raw)
	}
	return res
}

// ---------------------------------------------------------------------------
// Existing test: mark_read broadcasts a buffer_last_seen event
// ---------------------------------------------------------------------------

func TestMarkReadBroadcastsBufferUpdate(t *testing.T) {
	ctx := t.Context()
	ts := newTestWSServer(t)
	nID, bufID := ts.defaultNetwork(t)

	events, unsub := ts.hub.Subscribe(8)
	defer unsub()

	c := ts.dial(t)
	lastSeenID := uuid.Must(uuid.NewV7())

	sendCmd(t, ctx, c, clientCmd{
		Type: "mark_read", ReqID: "r1", BufferID: bufID, MessageID: lastSeenID,
	})
	ack := recvAck(t, ctx, c)
	if ack.Type != "ack" || ack.ReqID != "r1" {
		t.Fatalf("unexpected ack: %+v", ack)
	}

	select {
	case ev := <-events:
		got, ok := ev.(bufferLastSeenEvent)
		if !ok {
			t.Fatalf("event type = %T", ev)
		}
		if got.Type != "buffer_update" || got.ID != bufID || got.NetworkID != nID || got.LastSeenID != lastSeenID {
			t.Fatalf("unexpected event: %+v", got)
		}
		if got.Unread != 0 || got.Mentions != 0 {
			t.Fatalf("expected unread=0 mentions=0 on empty buffer, got %+v", got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

// TestMarkReadComputesResidualUnread verifies that mark_read against an
// older message-id leaves a non-zero unread/mentions count derived from the
// stored history (issue #54). Backend is the source of truth so all
// connected clients converge on the same numbers.
func TestMarkReadComputesResidualUnread(t *testing.T) {
	ctx := t.Context()
	ts := newTestWSServer(t)
	nID, bufID := ts.defaultNetwork(t)

	logStore, err := ts.stores.LogStore(nID)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _, err := ircdb.InsertLogMessage(ctx, logStore.DB, ircdb.LogMessageInput{
		BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ircdb.InsertLogMessage(ctx, logStore.DB, ircdb.LogMessageInput{
		BufferID: bufID, Sender: "alice", Kind: "privmsg", Content: "ping tester",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ircdb.InsertLogMessage(ctx, logStore.DB, ircdb.LogMessageInput{
		BufferID: bufID, Sender: "alice", Kind: "join", Content: "",
	}); err != nil {
		t.Fatal(err)
	}

	events, unsub := ts.hub.Subscribe(8)
	defer unsub()

	c := ts.dial(t)
	sendCmd(t, ctx, c, clientCmd{Type: "mark_read", ReqID: "r1", BufferID: bufID, MessageID: first})
	_ = recvAck(t, ctx, c)

	select {
	case ev := <-events:
		got, ok := ev.(bufferLastSeenEvent)
		if !ok {
			t.Fatalf("event type = %T", ev)
		}
		// One privmsg ("ping tester") past last_seen; "join" does not count.
		// mockManager.Nick is "" in this server, so mentions_me cannot fire,
		// expect 0 mentions even though content contains the network nick.
		if got.Unread != 1 {
			t.Fatalf("unread = %d, want 1", got.Unread)
		}
		if got.Mentions != 0 {
			t.Fatalf("mentions = %d, want 0 (no Manager wired -> nick lookup empty)", got.Mentions)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

// ---------------------------------------------------------------------------
// Table-driven tests for the 7 focus commands
// ---------------------------------------------------------------------------

func TestWSCmdMarkRead(t *testing.T) {
	ctx := t.Context()
	ts := newTestWSServer(t)
	_, bufID := ts.defaultNetwork(t)
	unknownID := uuid.Must(uuid.NewV7())

	tests := []struct {
		name    string
		cmd     clientCmd
		wantErr string // empty means expect ack
	}{
		{
			name: "success",
			cmd:  clientCmd{Type: "mark_read", ReqID: "r1", BufferID: bufID, MessageID: uuid.Must(uuid.NewV7())},
		},
		{
			name:    "missing buffer_id",
			cmd:     clientCmd{Type: "mark_read", ReqID: "r2", MessageID: uuid.Must(uuid.NewV7())},
			wantErr: "mark_read requires buffer_id and message_id",
		},
		{
			name:    "missing message_id",
			cmd:     clientCmd{Type: "mark_read", ReqID: "r3", BufferID: bufID},
			wantErr: "mark_read requires buffer_id and message_id",
		},
		{
			name:    "unknown buffer",
			cmd:     clientCmd{Type: "mark_read", ReqID: "r4", BufferID: unknownID, MessageID: uuid.Must(uuid.NewV7())},
			wantErr: "no rows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			if tt.wantErr != "" {
				errEnv := recvErr(t, ctx, c)
				if errEnv.Type != "error" || errEnv.ReqID != tt.cmd.ReqID || !strings.Contains(errEnv.Message, tt.wantErr) {
					t.Fatalf("got error=%+v, want message containing %q", errEnv, tt.wantErr)
				}
			} else {
				ack := recvAck(t, ctx, c)
				if ack.Type != "ack" || ack.ReqID != tt.cmd.ReqID {
					t.Fatalf("got ack=%+v, want ack/%s", ack, tt.cmd.ReqID)
				}
			}
		})
	}
}

func TestWSCmdHistory(t *testing.T) {
	ctx := t.Context()
	ts := newTestWSServer(t)
	_, bufID := ts.defaultNetwork(t)
	unknownID := uuid.Must(uuid.NewV7())

	tests := []struct {
		name    string
		cmd     clientCmd
		wantErr string
	}{
		{
			name: "success without cursor",
			cmd:  clientCmd{Type: "history", ReqID: "h1", BufferID: bufID, Limit: 10},
		},
		{
			name: "success with before cursor",
			cmd:  clientCmd{Type: "history", ReqID: "h2", BufferID: bufID, Before: uuid.Must(uuid.NewV7()), Limit: 10},
		},
		{
			name:    "missing buffer_id",
			cmd:     clientCmd{Type: "history", ReqID: "h3", Limit: 10},
			wantErr: "history requires buffer_id",
		},
		{
			name:    "unknown buffer",
			cmd:     clientCmd{Type: "history", ReqID: "h4", BufferID: unknownID, Limit: 10},
			wantErr: "no rows",
		},
		{
			name: "zero limit clamped to default",
			cmd:  clientCmd{Type: "history", ReqID: "h5", BufferID: bufID, Limit: 0},
		},
		{
			name: "excessive limit clamped to max",
			cmd:  clientCmd{Type: "history", ReqID: "h6", BufferID: bufID, Limit: 99999},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			if tt.wantErr != "" {
				errEnv := recvErr(t, ctx, c)
				if errEnv.Type != "error" || errEnv.ReqID != tt.cmd.ReqID || !strings.Contains(errEnv.Message, tt.wantErr) {
					t.Fatalf("got error=%+v, want message containing %q", errEnv, tt.wantErr)
				}
			} else {
				res := recvHistoryResult(t, ctx, c)
				if res.Type != "history_result" || res.ReqID != tt.cmd.ReqID || res.BufferID != tt.cmd.BufferID {
					t.Fatalf("got result=%+v, want history_result/%s for buffer %s", res, tt.cmd.ReqID, tt.cmd.BufferID)
				}
			}
		})
	}
}

// TestWSCmdSendValidation covers payloads cmdSend must reject before
// touching the manager. Each row asserts (error message, manager untouched).
func TestWSCmdSendValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	_, bufID := ts.defaultNetwork(t)
	nID, _ := ts.defaultNetwork(t)
	statusBufID := ts.defaultStatusBuffer(t, nID)
	unknownID := uuid.Must(uuid.NewV7())

	cases := []struct {
		name, wantErr string
		cmd           clientCmd
	}{
		{"missing buffer_id", "send requires buffer_id and content", clientCmd{Type: "send", ReqID: "s2", Content: "hello"}},
		{"empty content", "send requires buffer_id and content", clientCmd{Type: "send", ReqID: "s3", BufferID: bufID, Content: ""}},
		{"whitespace-only content", "send requires buffer_id and content", clientCmd{Type: "send", ReqID: "s4", BufferID: bufID, Content: "   \t  "}},
		{"unknown buffer", "unknown buffer", clientCmd{Type: "send", ReqID: "s5", BufferID: unknownID, Content: "hello"}},
		{"status buffer rejected", "cannot send to status buffer", clientCmd{Type: "send", ReqID: "s6", BufferID: statusBufID, Content: "hello"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			checkAckOrErr(t, ctx, c, tt.cmd.ReqID, tt.wantErr)
			assertNotCalled(t, mm.callRecorder, "Send")
			assertNotCalled(t, mm.callRecorder, "LogOutbound")
		})
	}
}

// TestWSCmdSendDispatch covers the happy path and manager-error path. Each
// case asserts the manager method was invoked with the exact resolved args
// (networkID, channel name, content) and whether LogOutbound was paired.
func TestWSCmdSendDispatch(t *testing.T) {
	ctx := t.Context()

	t.Run("success calls Send and pairs LogOutbound", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)

		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "send", ReqID: "s1", BufferID: bufID, Content: "hello"})
		checkAckOrErr(t, ctx, c, "s1", "")
		assertCalledWith(t, mm.callRecorder, "Send", nID, "#test", "hello")
		assertCalledWith(t, mm.callRecorder, "LogOutbound", nID, "#test", "privmsg", "hello")
	})

	t.Run("manager error bubbles and skips LogOutbound", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)
		mm.setError("Send", errMockNotConnected)

		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "send", ReqID: "s7", BufferID: bufID, Content: "hello"})
		checkAckOrErr(t, ctx, c, "s7", errMockNotConnected.Error())
		assertCalledWith(t, mm.callRecorder, "Send", nID, "#test", "hello")
		assertNotCalled(t, mm.callRecorder, "LogOutbound")
	})
}

func checkAckOrErr(t *testing.T, ctx context.Context, c *websocket.Conn, reqID, wantErr string) {
	t.Helper()
	if wantErr != "" {
		errEnv := recvErr(t, ctx, c)
		if errEnv.Type != "error" || errEnv.ReqID != reqID || !strings.Contains(errEnv.Message, wantErr) {
			t.Fatalf("got error=%+v, want message containing %q", errEnv, wantErr)
		}
		return
	}
	ack := recvAck(t, ctx, c)
	if ack.Type != "ack" || ack.ReqID != reqID {
		t.Fatalf("got ack=%+v, want ack/%s", ack, reqID)
	}
}

func TestWSCmdJoinValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)

	cases := []struct {
		name string
		cmd  clientCmd
	}{
		{"missing network_id", clientCmd{Type: "join", ReqID: "j2", Channel: "#newchan"}},
		{"empty channel", clientCmd{Type: "join", ReqID: "j3", NetworkID: nID, Channel: ""}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			checkAckOrErr(t, ctx, c, tt.cmd.ReqID, "join requires network_id and channel")
			assertNotCalled(t, mm.callRecorder, "Join")
		})
	}
}

// TestWSCmdJoinDispatch verifies join forwards the WS network_id and channel
// straight to Manager.Join (issue #64 — earlier test only asserted UUIDs were
// non-nil, this one checks the actual call args).
func TestWSCmdJoinDispatch(t *testing.T) {
	ctx := t.Context()

	t.Run("success calls Join with network_id and channel", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, _ := ts.defaultNetwork(t)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "join", ReqID: "j1", NetworkID: nID, Channel: "#newchan"})
		checkAckOrErr(t, ctx, c, "j1", "")
		assertCalledWith(t, mm.callRecorder, "Join", nID, "#newchan")
	})

	t.Run("manager error bubbles up", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, _ := ts.defaultNetwork(t)
		mm.setError("Join", errMockNotConnected)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "join", ReqID: "j4", NetworkID: nID, Channel: "#newchan"})
		checkAckOrErr(t, ctx, c, "j4", errMockNotConnected.Error())
		assertCalledWith(t, mm.callRecorder, "Join", nID, "#newchan")
	})
}

func TestWSCmdPartValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	unknownID := uuid.Must(uuid.NewV7())

	cases := []struct {
		name, wantErr string
		cmd           clientCmd
	}{
		{"missing buffer_id", "part requires buffer_id", clientCmd{Type: "part", ReqID: "p2"}},
		{"unknown buffer", "part only works on channel buffers", clientCmd{Type: "part", ReqID: "p3", BufferID: unknownID}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			checkAckOrErr(t, ctx, c, tt.cmd.ReqID, tt.wantErr)
			assertNotCalled(t, mm.callRecorder, "Part")
		})
	}
}

// TestWSCmdPartDispatch verifies part resolves buffer_id to (networkID,
// channelName) and forwards those plus the part reason to Manager.Part.
// (issue #64 — earlier test only asserted ids were non-nil.)
func TestWSCmdPartDispatch(t *testing.T) {
	ctx := t.Context()

	t.Run("success calls Part with resolved channel name and reason", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "part", ReqID: "p1", BufferID: bufID, Content: "bye"})
		checkAckOrErr(t, ctx, c, "p1", "")
		assertCalledWith(t, mm.callRecorder, "Part", nID, "#test", "bye")
	})

	t.Run("manager error bubbles up", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)
		mm.setError("Part", errMockNotConnected)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "part", ReqID: "p4", BufferID: bufID, Content: "bye"})
		checkAckOrErr(t, ctx, c, "p4", errMockNotConnected.Error())
		assertCalledWith(t, mm.callRecorder, "Part", nID, "#test", "bye")
	})
}

func TestWSCmdMsgValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)

	cases := []struct {
		name string
		cmd  clientCmd
	}{
		{"missing network_id", clientCmd{Type: "msg", ReqID: "m2", Target: "#chan", Content: "hello"}},
		{"empty target", clientCmd{Type: "msg", ReqID: "m3", NetworkID: nID, Target: "", Content: "hello"}},
		{"empty content", clientCmd{Type: "msg", ReqID: "m4", NetworkID: nID, Target: "#chan", Content: ""}},
		{"whitespace-only target", clientCmd{Type: "msg", ReqID: "m5", NetworkID: nID, Target: "   ", Content: "hello"}},
		{"whitespace-only content", clientCmd{Type: "msg", ReqID: "m6", NetworkID: nID, Target: "#chan", Content: "   "}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			checkAckOrErr(t, ctx, c, tt.cmd.ReqID, "msg requires network_id, target, and content")
			assertNotCalled(t, mm.callRecorder, "Send")
			assertNotCalled(t, mm.callRecorder, "LogOutbound")
		})
	}
}

func TestWSCmdMsgDispatch(t *testing.T) {
	ctx := t.Context()

	t.Run("success calls Send with explicit target and pairs LogOutbound", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, _ := ts.defaultNetwork(t)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "msg", ReqID: "m1", NetworkID: nID, Target: "#chan", Content: "hello"})
		checkAckOrErr(t, ctx, c, "m1", "")
		assertCalledWith(t, mm.callRecorder, "Send", nID, "#chan", "hello")
		assertCalledWith(t, mm.callRecorder, "LogOutbound", nID, "#chan", "privmsg", "hello")
	})

	t.Run("manager error bubbles and skips LogOutbound", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, _ := ts.defaultNetwork(t)
		mm.setError("Send", errMockNotConnected)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "msg", ReqID: "m7", NetworkID: nID, Target: "#chan", Content: "hello"})
		checkAckOrErr(t, ctx, c, "m7", errMockNotConnected.Error())
		assertCalledWith(t, mm.callRecorder, "Send", nID, "#chan", "hello")
		assertNotCalled(t, mm.callRecorder, "LogOutbound")
	})
}

func TestWSCmdNickValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)

	cases := []struct {
		name string
		cmd  clientCmd
	}{
		{"missing network_id", clientCmd{Type: "nick", ReqID: "n2", Content: "newnick"}},
		{"empty content", clientCmd{Type: "nick", ReqID: "n3", NetworkID: nID, Content: ""}},
		{"whitespace-only content", clientCmd{Type: "nick", ReqID: "n4", NetworkID: nID, Content: "   "}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			checkAckOrErr(t, ctx, c, tt.cmd.ReqID, "nick requires network_id and content")
			assertNotCalled(t, mm.callRecorder, "ChangeNick")
		})
	}
}

func TestWSCmdNickDispatch(t *testing.T) {
	ctx := t.Context()

	t.Run("success calls ChangeNick with trimmed nick", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, _ := ts.defaultNetwork(t)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "nick", ReqID: "n1", NetworkID: nID, Content: "newnick"})
		checkAckOrErr(t, ctx, c, "n1", "")
		assertCalledWith(t, mm.callRecorder, "ChangeNick", nID, "newnick")
	})

	t.Run("manager error bubbles up", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, _ := ts.defaultNetwork(t)
		mm.setError("ChangeNick", errMockNotConnected)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "nick", ReqID: "n5", NetworkID: nID, Content: "newnick"})
		checkAckOrErr(t, ctx, c, "n5", errMockNotConnected.Error())
		assertCalledWith(t, mm.callRecorder, "ChangeNick", nID, "newnick")
	})
}

func TestWSCmdMeValidation(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	_, bufID := ts.defaultNetwork(t)
	nID, _ := ts.defaultNetwork(t)
	statusBufID := ts.defaultStatusBuffer(t, nID)
	unknownID := uuid.Must(uuid.NewV7())

	cases := []struct {
		name, wantErr string
		cmd           clientCmd
	}{
		{"missing buffer_id", "me requires buffer_id and content", clientCmd{Type: "me", ReqID: "e2", Content: "waves"}},
		{"empty content", "me requires buffer_id and content", clientCmd{Type: "me", ReqID: "e3", BufferID: bufID, Content: ""}},
		{"whitespace-only content", "me requires buffer_id and content", clientCmd{Type: "me", ReqID: "e4", BufferID: bufID, Content: "   "}},
		{"unknown buffer", "invalid buffer for /me", clientCmd{Type: "me", ReqID: "e5", BufferID: unknownID, Content: "waves"}},
		{"status buffer rejected", "invalid buffer for /me", clientCmd{Type: "me", ReqID: "e6", BufferID: statusBufID, Content: "waves"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := ts.dial(t)
			sendCmd(t, ctx, c, tt.cmd)
			checkAckOrErr(t, ctx, c, tt.cmd.ReqID, tt.wantErr)
			assertNotCalled(t, mm.callRecorder, "Me")
			assertNotCalled(t, mm.callRecorder, "LogOutbound")
		})
	}
}

func TestWSCmdMeDispatch(t *testing.T) {
	ctx := t.Context()

	t.Run("success calls Me and logs action", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "me", ReqID: "e1", BufferID: bufID, Content: "waves"})
		checkAckOrErr(t, ctx, c, "e1", "")
		assertCalledWith(t, mm.callRecorder, "Me", nID, "#test", "waves")
		assertCalledWith(t, mm.callRecorder, "LogOutbound", nID, "#test", "action", "waves")
	})

	t.Run("manager error bubbles and skips LogOutbound", func(t *testing.T) {
		mgrOpt, mm := withMockMgr(t)
		ts := newTestWSServer(t, mgrOpt)
		nID, bufID := ts.defaultNetwork(t)
		mm.setError("Me", errMockNotConnected)
		c := ts.dial(t)
		sendCmd(t, ctx, c, clientCmd{Type: "me", ReqID: "e7", BufferID: bufID, Content: "waves"})
		checkAckOrErr(t, ctx, c, "e7", errMockNotConnected.Error())
		assertCalledWith(t, mm.callRecorder, "Me", nID, "#test", "waves")
		assertNotCalled(t, mm.callRecorder, "LogOutbound")
	})
}

// ---------------------------------------------------------------------------
// handleCmd dispatch: unknown command routing
// ---------------------------------------------------------------------------

func TestWSHandleCmdUnknownCommand(t *testing.T) {
	ctx := t.Context()
	ts := newTestWSServer(t)
	c := ts.dial(t)

	sendCmd(t, ctx, c, clientCmd{Type: "nonexistent", ReqID: "x1"})
	env := recvErr(t, ctx, c)
	if env.Type != "error" || !strings.Contains(env.Message, "unknown command") {
		t.Fatalf("got error=%+v, want unknown command error", env)
	}
}

