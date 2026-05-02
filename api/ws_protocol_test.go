package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
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
// mockManager — test double for ManagerInterface
// ---------------------------------------------------------------------------

// mockManager records every method call and lets tests configure per-method
// return errors (nil means success).
type mockManager struct {
	mu           sync.Mutex
	calls        []string
	returnErrors map[string]error
}

func newMockManager() *mockManager {
	return &mockManager{
		returnErrors: map[string]error{},
	}
}

func (m *mockManager) record(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, name)
}

func (m *mockManager) errFor(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.returnErrors[name]
}

// setError configures a method name to return the given error.
// An empty method name sets the default error for all methods.
func (m *mockManager) setError(method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if method == "" {
		for k := range m.returnErrors {
			delete(m.returnErrors, k)
		}
	}
	m.returnErrors[method] = err
}

func (m *mockManager) called(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Contains(m.calls, name)
}

// managerErr is the canonical "not connected" error for tests.
var errMockNotConnected = errors.New("irc: network not connected")

// Guard: verify *mockManager satisfies the composed API manager interface at compile time.
var _ manager = (*mockManager)(nil)

func (m *mockManager) Send(networkID uuid.UUID, target, content string) error {
	m.record("Send")
	return m.errFor("Send")
}

func (m *mockManager) LogOutbound(ctx context.Context, networkID uuid.UUID, target, kind, content string) error {
	m.record("LogOutbound")
	return m.errFor("LogOutbound")
}

func (m *mockManager) Join(networkID uuid.UUID, channel string) error {
	m.record("Join")
	return m.errFor("Join")
}

func (m *mockManager) Part(networkID uuid.UUID, channel, reason string) error {
	m.record("Part")
	return m.errFor("Part")
}

func (m *mockManager) ChangeNick(networkID uuid.UUID, nick string) error {
	m.record("ChangeNick")
	return m.errFor("ChangeNick")
}

func (m *mockManager) Me(networkID uuid.UUID, target, message string) error {
	m.record("Me")
	return m.errFor("Me")
}

func (m *mockManager) Topic(networkID uuid.UUID, channel, topic string) error {
	m.record("Topic")
	return m.errFor("Topic")
}

func (m *mockManager) Whois(networkID uuid.UUID, nick string) error {
	m.record("Whois")
	return m.errFor("Whois")
}

func (m *mockManager) Invite(networkID uuid.UUID, nick, channel string) error {
	m.record("Invite")
	return m.errFor("Invite")
}

func (m *mockManager) Kick(networkID uuid.UUID, channel, nick, reason string) error {
	m.record("Kick")
	return m.errFor("Kick")
}

func (m *mockManager) Mode(networkID uuid.UUID, target, modes string, params ...string) error {
	m.record("Mode")
	return m.errFor("Mode")
}

func (m *mockManager) Raw(networkID uuid.UUID, line string) error {
	m.record("Raw")
	return m.errFor("Raw")
}

func (m *mockManager) Away(networkID uuid.UUID, message string) error {
	m.record("Away")
	return m.errFor("Away")
}

func (m *mockManager) Back(networkID uuid.UUID) error {
	m.record("Back")
	return m.errFor("Back")
}

func (m *mockManager) Quit(networkID uuid.UUID, message string) error {
	m.record("Quit")
	return m.errFor("Quit")
}

func (m *mockManager) Rejoin(networkID uuid.UUID, channel string) error {
	m.record("Rejoin")
	return m.errFor("Rejoin")
}

func (m *mockManager) Notice(networkID uuid.UUID, target, content string) error {
	m.record("Notice")
	return m.errFor("Notice")
}

func (m *mockManager) CTCP(networkID uuid.UUID, nick, command, args string) error {
	m.record("CTCP")
	return m.errFor("CTCP")
}

func (m *mockManager) ListChannels(networkID uuid.UUID, filter string) error {
	m.record("ListChannels")
	return m.errFor("ListChannels")
}

func (m *mockManager) StateSnapshot() map[uuid.UUID]string {
	m.record("StateSnapshot")
	return nil
}

func (m *mockManager) IsJoined(networkID uuid.UUID, channel string) bool {
	m.record("IsJoined")
	return false
}

func (m *mockManager) ChannelMembers(networkID uuid.UUID, channel string) []ircdb.ChannelMember {
	m.record("ChannelMembers")
	return nil
}

func (m *mockManager) StopNetwork(networkID uuid.UUID) error {
	m.record("StopNetwork")
	return nil
}

func (m *mockManager) StartNetwork(ctx context.Context, networkID uuid.UUID, nc irc.NetworkConfig) error {
	m.record("StartNetwork")
	return nil
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

func TestWSCmdSend(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	_, bufID := ts.defaultNetwork(t)
	nID, _ := ts.defaultNetwork(t)
	statusBufID := ts.defaultStatusBuffer(t, nID)
	unknownID := uuid.Must(uuid.NewV7())

	tests := []struct {
		name       string
		cmd        clientCmd
		wantErr    string // empty means expect ack
		mgrErr     error  // error mock Manager returns for Send
		wantCalled string // method name expected to be called
	}{
		{
			name:       "success",
			cmd:        clientCmd{Type: "send", ReqID: "s1", BufferID: bufID, Content: "hello"},
			wantCalled: "Send",
		},
		{
			name:       "success calls LogOutbound",
			cmd:        clientCmd{Type: "send", ReqID: "s1b", BufferID: bufID, Content: "hello"},
			wantCalled: "LogOutbound",
		},
		{
			name:    "missing buffer_id",
			cmd:     clientCmd{Type: "send", ReqID: "s2", Content: "hello"},
			wantErr: "send requires buffer_id and content",
		},
		{
			name:    "empty content",
			cmd:     clientCmd{Type: "send", ReqID: "s3", BufferID: bufID, Content: ""},
			wantErr: "send requires buffer_id and content",
		},
		{
			name:    "whitespace-only content",
			cmd:     clientCmd{Type: "send", ReqID: "s4", BufferID: bufID, Content: "   \t  "},
			wantErr: "send requires buffer_id and content",
		},
		{
			name:    "unknown buffer",
			cmd:     clientCmd{Type: "send", ReqID: "s5", BufferID: unknownID, Content: "hello"},
			wantErr: "unknown buffer",
		},
		{
			name:    "status buffer rejected",
			cmd:     clientCmd{Type: "send", ReqID: "s6", BufferID: statusBufID, Content: "hello"},
			wantErr: "cannot send to status buffer",
		},
		{
			name:       "manager returns error",
			cmd:        clientCmd{Type: "send", ReqID: "s7", BufferID: bufID, Content: "hello"},
			wantErr:    errMockNotConnected.Error(),
			mgrErr:     errMockNotConnected,
			wantCalled: "Send",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm.setError("Send", tt.mgrErr)
			mm.setError("LogOutbound", nil)
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
			if tt.wantCalled != "" && !mm.called(tt.wantCalled) {
				t.Fatalf("expected mock method %q to be called, calls=%v", tt.wantCalled, mm.calls)
			}
			if tt.mgrErr == nil && tt.wantCalled == "Send" && !mm.called("LogOutbound") {
				t.Fatal("expected LogOutbound to be called after successful Send")
			}
		})
	}
}

func TestWSCmdJoin(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)

	tests := []struct {
		name       string
		cmd        clientCmd
		wantErr    string
		mgrErr     error
		wantCalled string
	}{
		{
			name:       "success",
			cmd:        clientCmd{Type: "join", ReqID: "j1", NetworkID: nID, Channel: "#newchan"},
			wantCalled: "Join",
		},
		{
			name:    "missing network_id",
			cmd:     clientCmd{Type: "join", ReqID: "j2", Channel: "#newchan"},
			wantErr: "join requires network_id and channel",
		},
		{
			name:    "empty channel",
			cmd:     clientCmd{Type: "join", ReqID: "j3", NetworkID: nID, Channel: ""},
			wantErr: "join requires network_id and channel",
		},
		{
			name:       "manager returns error",
			cmd:        clientCmd{Type: "join", ReqID: "j4", NetworkID: nID, Channel: "#newchan"},
			wantErr:    errMockNotConnected.Error(),
			mgrErr:     errMockNotConnected,
			wantCalled: "Join",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm.setError("Join", tt.mgrErr)
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
			if tt.wantCalled != "" && !mm.called(tt.wantCalled) {
				t.Fatalf("expected mock method %q to be called, calls=%v", tt.wantCalled, mm.calls)
			}
		})
	}
}

func TestWSCmdPart(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	_, bufID := ts.defaultNetwork(t)
	unknownID := uuid.Must(uuid.NewV7())

	tests := []struct {
		name       string
		cmd        clientCmd
		wantErr    string
		mgrErr     error
		wantCalled string
	}{
		{
			name:       "success",
			cmd:        clientCmd{Type: "part", ReqID: "p1", BufferID: bufID, Content: "bye"},
			wantCalled: "Part",
		},
		{
			name:    "missing buffer_id",
			cmd:     clientCmd{Type: "part", ReqID: "p2"},
			wantErr: "part requires buffer_id",
		},
		{
			name:    "unknown buffer",
			cmd:     clientCmd{Type: "part", ReqID: "p3", BufferID: unknownID},
			wantErr: "part only works on channel buffers",
		},
		{
			name:       "manager returns error",
			cmd:        clientCmd{Type: "part", ReqID: "p4", BufferID: bufID, Content: "bye"},
			wantErr:    errMockNotConnected.Error(),
			mgrErr:     errMockNotConnected,
			wantCalled: "Part",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm.setError("Part", tt.mgrErr)
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
			if tt.wantCalled != "" && !mm.called(tt.wantCalled) {
				t.Fatalf("expected mock method %q to be called, calls=%v", tt.wantCalled, mm.calls)
			}
		})
	}
}

func TestWSCmdMsg(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)

	tests := []struct {
		name       string
		cmd        clientCmd
		wantErr    string
		mgrErr     error
		wantCalled string
	}{
		{
			name:       "success",
			cmd:        clientCmd{Type: "msg", ReqID: "m1", NetworkID: nID, Target: "#chan", Content: "hello"},
			wantCalled: "Send",
		},
		{
			name:    "missing network_id",
			cmd:     clientCmd{Type: "msg", ReqID: "m2", Target: "#chan", Content: "hello"},
			wantErr: "msg requires network_id, target, and content",
		},
		{
			name:    "empty target",
			cmd:     clientCmd{Type: "msg", ReqID: "m3", NetworkID: nID, Target: "", Content: "hello"},
			wantErr: "msg requires network_id, target, and content",
		},
		{
			name:    "empty content",
			cmd:     clientCmd{Type: "msg", ReqID: "m4", NetworkID: nID, Target: "#chan", Content: ""},
			wantErr: "msg requires network_id, target, and content",
		},
		{
			name:    "whitespace-only target trimmed",
			cmd:     clientCmd{Type: "msg", ReqID: "m5", NetworkID: nID, Target: "   ", Content: "hello"},
			wantErr: "msg requires network_id, target, and content",
		},
		{
			name:    "whitespace-only content trimmed",
			cmd:     clientCmd{Type: "msg", ReqID: "m6", NetworkID: nID, Target: "#chan", Content: "   "},
			wantErr: "msg requires network_id, target, and content",
		},
		{
			name:       "manager returns error",
			cmd:        clientCmd{Type: "msg", ReqID: "m7", NetworkID: nID, Target: "#chan", Content: "hello"},
			wantErr:    errMockNotConnected.Error(),
			mgrErr:     errMockNotConnected,
			wantCalled: "Send",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm.setError("Send", tt.mgrErr)
			mm.setError("LogOutbound", nil)
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
			if tt.wantCalled != "" && !mm.called(tt.wantCalled) {
				t.Fatalf("expected mock method %q to be called, calls=%v", tt.wantCalled, mm.calls)
			}
		})
	}
}

func TestWSCmdNick(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	nID, _ := ts.defaultNetwork(t)

	tests := []struct {
		name       string
		cmd        clientCmd
		wantErr    string
		mgrErr     error
		wantCalled string
	}{
		{
			name:       "success",
			cmd:        clientCmd{Type: "nick", ReqID: "n1", NetworkID: nID, Content: "newnick"},
			wantCalled: "ChangeNick",
		},
		{
			name:    "missing network_id",
			cmd:     clientCmd{Type: "nick", ReqID: "n2", Content: "newnick"},
			wantErr: "nick requires network_id and content",
		},
		{
			name:    "empty content",
			cmd:     clientCmd{Type: "nick", ReqID: "n3", NetworkID: nID, Content: ""},
			wantErr: "nick requires network_id and content",
		},
		{
			name:    "whitespace-only content trimmed to empty",
			cmd:     clientCmd{Type: "nick", ReqID: "n4", NetworkID: nID, Content: "   "},
			wantErr: "nick requires network_id and content",
		},
		{
			name:       "manager returns error",
			cmd:        clientCmd{Type: "nick", ReqID: "n5", NetworkID: nID, Content: "newnick"},
			wantErr:    errMockNotConnected.Error(),
			mgrErr:     errMockNotConnected,
			wantCalled: "ChangeNick",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm.setError("ChangeNick", tt.mgrErr)
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
			if tt.wantCalled != "" && !mm.called(tt.wantCalled) {
				t.Fatalf("expected mock method %q to be called, calls=%v", tt.wantCalled, mm.calls)
			}
		})
	}
}

func TestWSCmdMe(t *testing.T) {
	ctx := t.Context()
	mgrOpt, mm := withMockMgr(t)
	ts := newTestWSServer(t, mgrOpt)
	_, bufID := ts.defaultNetwork(t)
	nID, _ := ts.defaultNetwork(t)
	statusBufID := ts.defaultStatusBuffer(t, nID)
	unknownID := uuid.Must(uuid.NewV7())

	tests := []struct {
		name       string
		cmd        clientCmd
		wantErr    string
		mgrErr     error
		wantCalled string
	}{
		{
			name:       "success",
			cmd:        clientCmd{Type: "me", ReqID: "e1", BufferID: bufID, Content: "waves"},
			wantCalled: "Me",
		},
		{
			name:    "missing buffer_id",
			cmd:     clientCmd{Type: "me", ReqID: "e2", Content: "waves"},
			wantErr: "me requires buffer_id and content",
		},
		{
			name:    "empty content",
			cmd:     clientCmd{Type: "me", ReqID: "e3", BufferID: bufID, Content: ""},
			wantErr: "me requires buffer_id and content",
		},
		{
			name:    "whitespace-only content trimmed to empty",
			cmd:     clientCmd{Type: "me", ReqID: "e4", BufferID: bufID, Content: "   "},
			wantErr: "me requires buffer_id and content",
		},
		{
			name:    "unknown buffer",
			cmd:     clientCmd{Type: "me", ReqID: "e5", BufferID: unknownID, Content: "waves"},
			wantErr: "invalid buffer for /me",
		},
		{
			name:    "status buffer rejected",
			cmd:     clientCmd{Type: "me", ReqID: "e6", BufferID: statusBufID, Content: "waves"},
			wantErr: "invalid buffer for /me",
		},
		{
			name:       "manager returns error",
			cmd:        clientCmd{Type: "me", ReqID: "e7", BufferID: bufID, Content: "waves"},
			wantErr:    errMockNotConnected.Error(),
			mgrErr:     errMockNotConnected,
			wantCalled: "Me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm.setError("Me", tt.mgrErr)
			mm.setError("LogOutbound", nil)
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
			if tt.wantCalled != "" && !mm.called(tt.wantCalled) {
				t.Fatalf("expected mock method %q to be called, calls=%v", tt.wantCalled, mm.calls)
			}
		})
	}
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

// ---------------------------------------------------------------------------
// Existing test: verify join/part use correct ID fields
// ---------------------------------------------------------------------------

func TestJoinUsesNetworkIDChannelAndPartUsesBufferID(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	n, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	bufferID, _, _, err := stores.EnsureBuffer(ctx, n.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}

	if n.ID == uuid.Nil || bufferID == uuid.Nil {
		t.Fatal("expected non-zero ids")
	}
}
