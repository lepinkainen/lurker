package main

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// layout constants
const (
	sidebarWidth    = 26
	membersWidth    = 22
	inputLines      = 1 // textarea height in rows
	statusHeight    = 1
	headerHeight    = 2 // content row + BorderBottom row from styleHeader
	separatorHeight = 1
	// rows rendered above the buffer list in renderSidebar:
	// connection-status row + separator row
	sidebarChromeRows = 2
)

type focusArea int

const (
	focusInput focusArea = iota
	focusSidebar
)

type model struct {
	cfg    *Config
	client *apiClient

	networks            []networkDTO
	buffers             []bufferDTO
	networkStates       map[uuid.UUID]string          // network_id -> state string
	messages            map[uuid.UUID][]messageDTO    // buffer_id -> messages
	members             map[uuid.UUID][]channelMember // buffer_id -> members
	unread              map[uuid.UUID]int             // buffer_id -> unread count (client-side accumulated)
	mentions            map[uuid.UUID]int             // buffer_id -> mention count
	sidebarItems        []sidebarItem
	sidebarSel          int
	activeBuffer        *bufferDTO
	lastPersistedBuffer uuid.UUID
	// archivesOpen tracks per-network Archives fold state. In-memory only:
	// folds reset to closed on restart (matching the folded-by-default UX).
	archivesOpen map[uuid.UUID]bool

	viewport viewport.Model
	input    textarea.Model

	focus  focusArea
	width  int
	height int
	ready  bool

	wsConn    *websocket.Conn
	wsCancel  context.CancelFunc
	wsChan    <-chan wsEvent
	wsStatus  string // "connecting" | "connected" | "reconnecting" | "offline"
	backendOK bool
	// sendWS overrides the outbound WS command path when non-nil (tests).
	// Production leaves it nil and sendCmdAsync enqueues on wsSendChan.
	sendWS func(cmd wsCmd) error
	// wsSendChan feeds the single writer goroutine for the current
	// connection, so outbound commands keep their order (an out-of-order
	// mark_read would regress the server-side read position).
	wsSendChan chan wsCmd

	// quit state: ctrl+d double-tap
	lastCtrlD time.Time

	// input history (most recent last)
	history    []string
	historyIdx int // -1 = not browsing; else index into history

	// nick autocomplete
	tabPrefix string // when non-empty, current token prefix being cycled
	tabIdx    int
	tabPos    int // start offset of token in input

	// members pane visibility
	showMembers bool

	// channel switcher
	switcher switcherModel

	// scrollback in-flight tracking
	historyLoading map[uuid.UUID]bool
	historyExhaust map[uuid.UUID]bool

	// /list result accumulator: per-network until channel_list Done=true.
	channelList map[uuid.UUID][]channelListEntry

	status  string
	loading bool
	// lastSeq is the stream cursor: the seq of the last applied frame (the
	// state snapshot included). Frames at or below it are dropped; it is
	// sent as hello.since on every (re)connect so the server replays only
	// what was missed, or answers with a fresh state frame when it can't.
	lastSeq uint64
}

func newModel(cfg *Config) model {
	ta := textarea.New()
	ta.Placeholder = "Type a message…"
	ta.Focus()
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.CharLimit = 512

	return model{
		cfg:            cfg,
		client:         newAPIClient(cfg.BackendURL),
		networkStates:  make(map[uuid.UUID]string),
		messages:       make(map[uuid.UUID][]messageDTO),
		members:        make(map[uuid.UUID][]channelMember),
		unread:         make(map[uuid.UUID]int),
		mentions:       make(map[uuid.UUID]int),
		input:          ta,
		focus:          focusInput,
		wsStatus:       "connecting",
		status:         "Connecting…",
		loading:        true,
		historyIdx:     -1,
		historyLoading: make(map[uuid.UUID]bool),
		historyExhaust: make(map[uuid.UUID]bool),
		channelList:    make(map[uuid.UUID][]channelListEntry),
		archivesOpen:   make(map[uuid.UUID]bool),
	}
}

// Init opens the WebSocket; everything else (the state snapshot or a replay
// from our cursor) arrives over it in answer to hello (see wsConnectedMsg).
func (m model) Init() tea.Cmd {
	return connectWSCmd(m.client)
}

// ── commands ──────────────────────────────────────────────────────────────────

func reconnectWSCmd(c *apiClient, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(delay)
		ctx, cancel := context.WithCancel(context.Background())
		conn, err := c.connectWS(ctx)
		if err != nil {
			cancel()
			return wsErrorMsg{err}
		}
		ch := startWSReader(ctx, conn)
		return wsConnectedMsg{conn: conn, ch: ch, cancel: cancel}
	}
}

func connectWSCmd(c *apiClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		conn, err := c.connectWS(ctx)
		if err != nil {
			cancel()
			return wsErrorMsg{err}
		}
		ch := startWSReader(ctx, conn)
		return wsConnectedMsg{conn: conn, ch: ch, cancel: cancel}
	}
}

// waitForWSEvent blocks until the next event arrives on ch.
func waitForWSEvent(ch <-chan wsEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return wsErrorMsg{fmt.Errorf("disconnected from server")}
		}
		return wsEventMsg(ev)
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeComponents()
		m.ready = true
		m.refreshViewport()

	case wsConnectedMsg:
		m.wsConn = msg.conn
		m.wsCancel = msg.cancel
		m.wsChan = msg.ch
		if m.wsSendChan != nil {
			close(m.wsSendChan)
		}
		m.wsSendChan = make(chan wsCmd, 64)
		go wsWriter(msg.conn, m.wsSendChan)
		m.backendOK = true
		m.wsStatus = "connected"
		m.status = "Syncing state…"
		// history_result frames are socket-local (no seq, never replayed), so
		// a request lost with the old socket would otherwise block paging for
		// that buffer forever. A replay-only resume never reaches applyState,
		// which used to reset this.
		m.historyLoading = make(map[uuid.UUID]bool)
		// hello is the first frame on every (re)connect: the server replays
		// what we missed since lastSeq, or answers with a state frame when
		// the cursor is unknown or too old. Anything missed while the socket
		// was down (messages, membership, markers, buffer changes) comes
		// back one of those two ways.
		m.sendCmdAsync(wsCmd{"type": "hello", "since": m.lastSeq})
		return m, waitForWSEvent(m.wsChan)

	case wsEventMsg:
		m.handleWSEvent(wsEvent(msg))
		return m, waitForWSEvent(m.wsChan)

	case wsErrorMsg:
		m.backendOK = false
		m.wsStatus = "reconnecting"
		m.status = fmt.Sprintf("WS error: %v — reconnecting in 5s…", msg.err)
		m.wsConn = nil
		m.wsChan = nil
		if m.wsSendChan != nil {
			close(m.wsSendChan)
			m.wsSendChan = nil
		}
		return m, reconnectWSCmd(m.client, 5*time.Second)

	case errMsg:
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.loading = false

	case historyFailedMsg:
		delete(m.historyLoading, msg.bufferID)
		m.status = fmt.Sprintf("History request failed: %v", msg.err)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	// Forward other events to textarea when input is focused.
	if m.focus == focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// historyFailedMsg signals a failed outbound history request so the loop
// can clear historyLoading and surface a status line — otherwise the
// flag stayed true forever after a WS write error and pgup at the top of
// the buffer became a silent no-op.
type historyFailedMsg struct {
	bufferID uuid.UUID
	err      error
}

func (m *model) requestHistory() tea.Cmd {
	if m.activeBuffer == nil || m.wsConn == nil {
		return nil
	}
	bufID := m.activeBuffer.ID
	if m.historyLoading[bufID] || m.historyExhaust[bufID] {
		return nil
	}
	msgs := m.messages[bufID]
	if len(msgs) == 0 {
		return nil
	}
	oldest := msgs[0].ID
	m.historyLoading[bufID] = true
	conn := m.wsConn
	return func() tea.Msg {
		if err := sendWSCmd(context.Background(), conn, wsCmd{
			"type":      "history",
			"buffer_id": bufID,
			"before":    oldest,
			"limit":     100,
		}); err != nil {
			return historyFailedMsg{bufferID: bufID, err: err}
		}
		return nil
	}
}

// ── read tracking / new-messages marker ───────────────────────────────────────
// Semantics in ai-docs/behaviors/new-messages-marker.md. Marker state is
// server-owned (bufferDTO.MarkerID/MarkerTS); the client holds no anchor
// state of its own. Nothing clears implicitly — the only mark_read triggers
// are the explicit acks: Esc and a click on the unread bar.

// uuidLTE compares message UUIDs by byte order (UUIDv7 is time-ordered,
// matching the web client's lexicographic string compare).
func uuidLTE(a, b uuid.UUID) bool {
	return bytes.Compare(a[:], b[:]) <= 0
}

// sendCmdAsync enqueues a WS command without blocking the update loop.
// Returns false when no connection (or a full queue) can take it.
func (m *model) sendCmdAsync(cmd wsCmd) bool {
	if m.sendWS != nil {
		return m.sendWS(cmd) == nil
	}
	if m.wsSendChan == nil {
		return false
	}
	select {
	case m.wsSendChan <- cmd:
		return true
	default:
		return false
	}
}

// wsWriter is the single outbound writer for one connection: commands go
// out in enqueue order, and a stalled peer can't accumulate goroutines.
// Exits when the channel is closed (reconnect) or a write fails.
func wsWriter(conn *websocket.Conn, ch <-chan wsCmd) {
	for cmd := range ch {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := sendWSCmd(ctx, conn, cmd)
		cancel()
		if err != nil {
			// Conn is dead; the read loop will surface wsErrorMsg. Drain
			// so late enqueues don't back up until the channel closes.
			for range ch { //nolint:revive // intentional drain
			}
			return
		}
	}
}

// ackActiveRead is the explicit user ack (Esc / unread-bar click): sends
// mark_read for the newest loaded message and optimistically drops the
// marker, bar and badges. If the send can't go out (no connection, full
// queue) it is a complete no-op — visible state stays truthful to the
// server, and the next buffer_update / resync restores the marker.
func (m *model) ackActiveRead() {
	b := m.activeBuffer
	if b == nil {
		return
	}
	msgs := m.messages[b.ID]
	if len(msgs) == 0 {
		return
	}
	last := msgs[len(msgs)-1].ID
	if !m.sendCmdAsync(wsCmd{"type": "mark_read", "buffer_id": b.ID, "message_id": last}) {
		return
	}
	if !uuidLTE(last, b.LastSeenID) {
		b.LastSeenID = last
	}
	b.MarkerID = uuid.Nil
	b.MarkerTS = ""
	m.unread[b.ID] = 0
	m.mentions[b.ID] = 0
	m.refreshViewport()
}

// ── state helpers ─────────────────────────────────────────────────────────────

// sortNetworks restores server sidebar order after a live network event;
// /api/state already arrives sorted.
func (m *model) sortNetworks() {
	sort.SliceStable(m.networks, func(i, j int) bool { return m.networks[i].SortOrder < m.networks[j].SortOrder })
}

// findNetwork resolves a network by ID. Returns nil if unknown.
// Linear scan is fine — typical session has a handful of networks.
func (m *model) findNetwork(id uuid.UUID) *networkDTO {
	for i := range m.networks {
		if m.networks[i].ID == id {
			return &m.networks[i]
		}
	}
	return nil
}

// findBuffer resolves a buffer by ID against the live slice. Returns nil
// if unknown. The pointer aliases m.buffers — do not hold it across
// anything that may replace or grow the slice.
func (m *model) findBuffer(id uuid.UUID) *bufferDTO {
	for i := range m.buffers {
		if m.buffers[i].ID == id {
			return &m.buffers[i]
		}
	}
	return nil
}

// refreshActiveBuffer re-resolves the m.activeBuffer pointer to the
// current m.buffers slice. Call after any operation that may have
// replaced or reslized m.buffers (applyState, buffer_created append) —
// otherwise the pointer would alias a stale backing array.
func (m *model) refreshActiveBuffer() {
	if m.activeBuffer == nil {
		return
	}
	m.activeBuffer = m.findBuffer(m.activeBuffer.ID)
}

// applyState replaces local state with a fresh /api/state snapshot. Runs on
// every (re)connect, so it must be safe to call repeatedly: per-buffer
// message lists are replaced by the snapshot's recent window (older pages
// the user scrolled to are dropped and can be re-fetched), and the
// history-exhausted flags are cleared accordingly.
// applyState installs a state frame: the full picture as of its seq. Every
// later frame on the stream is newer than it, so nothing needs reconciling.
func (m *model) applyState(s *stateResponse) {
	m.networks = s.Networks
	m.buffers = s.Buffers
	m.refreshActiveBuffer()
	for _, b := range s.Buffers {
		m.unread[b.ID] = b.Unread
		m.mentions[b.ID] = b.Mentions
	}
	m.historyExhaust = make(map[uuid.UUID]bool)
	m.historyLoading = make(map[uuid.UUID]bool)
	for key, msgs := range s.InitialMessages {
		id, err := uuid.Parse(key)
		if err != nil {
			continue
		}
		for i := range msgs {
			msgs[i].TSParsed, _ = time.Parse(time.RFC3339Nano, msgs[i].TS)
		}
		m.messages[id] = msgs
	}
	for key, mems := range s.Members {
		id, err := uuid.Parse(key)
		if err != nil {
			continue
		}
		m.members[id] = mems
	}
	m.rebuildSidebar()
	if len(m.sidebarItems) > 0 {
		m.selectStartupBuffer()
		m.activateSidebarSel()
	}
}

func (m *model) selectStartupBuffer() {
	var initial uuid.UUID
	if m.activeBuffer != nil {
		initial = m.activeBuffer.ID
	} else {
		persisted := loadPersistedBufferID()
		m.lastPersistedBuffer = persisted
		initial = pickStartupBuffer(m.networks, m.buffers, persisted)
	}
	if initial == uuid.Nil {
		return
	}
	for i, item := range m.sidebarItems {
		if !item.isHeader && item.bufferID == initial {
			m.sidebarSel = i
			return
		}
	}
}
