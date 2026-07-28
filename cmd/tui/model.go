package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/irc"
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
	}
}

// Init fetches initial state from the backend.
func (m model) Init() tea.Cmd {
	return fetchStateCmd(m.client)
}

// ── commands ──────────────────────────────────────────────────────────────────

func fetchStateCmd(c *apiClient) tea.Cmd {
	return func() tea.Msg {
		state, err := c.fetchState(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return stateLoadedMsg{state}
	}
}

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

	case stateLoadedMsg:
		m.applyState(msg.state)
		m.loading = false
		m.backendOK = false
		m.wsStatus = "connecting"
		m.status = "Loaded — connecting WS…"
		return m, connectWSCmd(m.client)

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
		m.status = ""
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

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.switcher.open {
		return m.handleSwitcherKey(msg)
	}
	key := msg.String()
	if model, cmd, handled := m.dispatchControlKey(key); handled {
		return model, cmd
	}
	if model, cmd, handled := m.dispatchNavKey(key); handled {
		return model, cmd
	}
	m.resetTransientKeyState(key)
	if m.focus == focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return *m, cmd
	}
	return *m, nil
}

// dispatchControlKey handles meta keys (quit, popups, focus toggle, tab).
func (m *model) dispatchControlKey(key string) (tea.Model, tea.Cmd, bool) {
	switch key {
	case "ctrl+d":
		return m.handleCtrlD()
	case "ctrl+k":
		m.openSwitcher()
		return *m, nil, true
	case "ctrl+u":
		m.showMembers = !m.showMembers
		m.resizeComponents()
		m.refreshViewport()
		return *m, nil, true
	case "esc":
		// Web parity: Esc acks the active buffer (clears marker, bar and
		// badges everywhere) in addition to its focus toggle.
		m.ackActiveRead()
		m.toggleFocus()
		return *m, nil, true
	case "tab":
		if m.focus == focusInput {
			m.nickAutocomplete()
			return *m, nil, true
		}
	}
	return *m, nil, false
}

// dispatchNavKey handles up/down/pgup/pgdown/enter.
func (m *model) dispatchNavKey(key string) (tea.Model, tea.Cmd, bool) {
	switch key {
	case "up":
		return m.handleUp()
	case "down":
		return m.handleDown()
	case "pgup":
		var cmd tea.Cmd
		if m.viewport.AtTop() {
			cmd = m.requestHistory()
		}
		m.viewport.HalfPageUp()
		return *m, cmd, true
	case "pgdown":
		m.viewport.HalfPageDown()
		return *m, nil, true
	case "enter":
		if m.focus == focusSidebar {
			m.activateSidebarSel()
			m.focus = focusInput
			m.input.Focus()
			return *m, nil, true
		}
		model, cmd := m.submitInput()
		return model, cmd, true
	}
	return *m, nil, false
}

// handleMouse maps mouse input: left-click on a sidebar buffer row activates
// that buffer; wheel scrolls the message viewport (mouse mode disables
// terminal-native scrolling, so the wheel must be handled here).
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.switcher.open {
		return *m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		var cmd tea.Cmd
		if m.viewport.AtTop() {
			cmd = m.requestHistory()
		}
		m.viewport.ScrollUp(3)
		return *m, cmd
	case tea.MouseButtonWheelDown:
		m.viewport.ScrollDown(3)
		return *m, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return *m, nil
		}
		if idx, ok := m.sidebarItemAt(msg.X, msg.Y); ok {
			m.sidebarSel = idx
			m.activateSidebarSel()
			m.focus = focusInput
			m.input.Focus()
			return *m, nil
		}
		// Click on the unread bar (pinned line between header and viewport)
		// acks the active buffer — the bar's one activation affordance.
		if m.unreadBarVisible() && msg.Y == headerHeight && msg.X > sidebarWidth {
			m.ackActiveRead()
			return *m, nil
		}
		// Mouse capture disables Ghostty/iTerm native link clicking, so
		// hit-test URLs in the viewport and open them ourselves.
		if url, ok := m.urlAtClick(msg.X, msg.Y); ok {
			m.status = "Opening " + url
			return *m, openURLCmd(url)
		}
	}
	return *m, nil
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"']+`)

// urlAtClick maps a screen coordinate into the message viewport and returns
// the URL under it, if any. Viewport lines never wrap (viewport clips), so
// one content line equals one screen row.
func (m *model) urlAtClick(x, y int) (string, bool) {
	if m.activeBuffer == nil {
		return "", false
	}
	relX := x - (sidebarWidth + 1) // sidebar + its right border column
	relY := y - headerHeight
	if m.unreadBarVisible() {
		relY-- // the unread bar occupies the row above the viewport
	}
	if relX < 0 || relY < 0 || relY >= m.viewport.Height {
		return "", false
	}
	msgs := m.messages[m.activeBuffer.ID]
	if len(msgs) == 0 {
		return "", false
	}
	ownNick := ""
	if n := m.findNetwork(m.activeBuffer.NetworkID); n != nil {
		ownNick = n.Nick
	}
	lines := groupAndFormatMessages(msgs, ownNick, m.activeBuffer)
	lineIdx := m.viewport.YOffset + relY
	if lineIdx < 0 || lineIdx >= len(lines) {
		return "", false
	}
	return urlAtCol(lines[lineIdx], relX)
}

// urlAtCol hit-tests display column col against URL spans in an
// ANSI-styled line.
func urlAtCol(line string, col int) (string, bool) {
	plain := ansi.Strip(line)
	for _, loc := range urlRe.FindAllStringIndex(plain, -1) {
		start := ansi.StringWidth(plain[:loc[0]])
		end := start + ansi.StringWidth(plain[loc[0]:loc[1]])
		if col >= start && col < end {
			return plain[loc[0]:loc[1]], true
		}
	}
	return "", false
}

// openURLCmd opens url in the OS default browser. The url is passed as a
// single argv element (no shell) and urlRe pins the scheme to http/https,
// so message content can't smuggle flags or other schemes.
func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var c *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			c = exec.CommandContext(ctx, "open", url) //nolint:gosec // argv-only, scheme pinned by urlRe
		case "windows":
			c = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url) //nolint:gosec // argv-only, scheme pinned by urlRe
		default:
			c = exec.CommandContext(ctx, "xdg-open", url) //nolint:gosec // argv-only, scheme pinned by urlRe
		}
		if err := c.Start(); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// sidebarItemAt resolves a screen coordinate to a selectable sidebar item
// index. Returns false for clicks outside the sidebar, on chrome rows, or
// on network headers.
func (m *model) sidebarItemAt(x, y int) (int, bool) {
	if x >= sidebarWidth {
		return 0, false
	}
	idx := y - sidebarChromeRows
	if idx < 0 || idx >= len(m.sidebarItems) || m.sidebarItems[idx].isHeader {
		return 0, false
	}
	return idx, true
}

func (m *model) handleCtrlD() (tea.Model, tea.Cmd, bool) {
	now := time.Now()
	if !m.lastCtrlD.IsZero() && now.Sub(m.lastCtrlD) < time.Second {
		model, cmd := m.quit()
		return model, cmd, true
	}
	m.lastCtrlD = now
	m.status = "Press Ctrl+D again to quit"
	return *m, nil, true
}

func (m *model) openSwitcher() {
	m.switcher.open = true
	m.switcher.query = ""
	m.switcher.sel = 0
	m.switcher.refresh(m.buffers, m.networks)
}

func (m *model) toggleFocus() {
	if m.focus == focusInput {
		m.focus = focusSidebar
		m.input.Blur()
		return
	}
	m.focus = focusInput
	m.input.Focus()
}

func (m *model) handleUp() (tea.Model, tea.Cmd, bool) {
	if m.focus == focusSidebar {
		m.moveSidebar(-1)
		return *m, nil, true
	}
	if (m.focus == focusInput && m.input.Value() == "") || m.historyIdx >= 0 {
		if m.recallHistory(-1) {
			return *m, nil, true
		}
	}
	m.viewport.ScrollUp(3)
	return *m, nil, true
}

func (m *model) handleDown() (tea.Model, tea.Cmd, bool) {
	if m.focus == focusSidebar {
		m.moveSidebar(1)
		return *m, nil, true
	}
	if m.historyIdx >= 0 && m.recallHistory(1) {
		return *m, nil, true
	}
	m.viewport.ScrollDown(3)
	return *m, nil, true
}

func (m *model) resetTransientKeyState(key string) {
	if key != "ctrl+d" {
		m.lastCtrlD = time.Time{}
	}
	if key != "tab" {
		m.tabPrefix = ""
	}
	if m.historyIdx >= 0 && key != "up" && key != "down" {
		m.historyIdx = -1
	}
}

func (m *model) handleSwitcherKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c", "ctrl+k":
		m.switcher.open = false
		return *m, nil
	case "up":
		if m.switcher.sel > 0 {
			m.switcher.sel--
		}
		return *m, nil
	case "down":
		if m.switcher.sel+1 < len(m.switcher.entries) {
			m.switcher.sel++
		}
		return *m, nil
	case "enter":
		if len(m.switcher.entries) > 0 {
			e := m.switcher.entries[m.switcher.sel]
			m.jumpToBuffer(e.bufferID)
		}
		m.switcher.open = false
		return *m, nil
	case "backspace":
		if m.switcher.query != "" {
			m.switcher.query = m.switcher.query[:len(m.switcher.query)-1]
			m.switcher.sel = 0
			m.switcher.refresh(m.buffers, m.networks)
		}
		return *m, nil
	}
	// printable input → append to query
	if r := msg.Runes; len(r) > 0 {
		m.switcher.query += string(r)
		m.switcher.sel = 0
		m.switcher.refresh(m.buffers, m.networks)
	}
	return *m, nil
}

func (m *model) jumpToBuffer(id uuid.UUID) {
	for i, it := range m.sidebarItems {
		if it.isHeader || it.bufferID != id {
			continue
		}
		m.sidebarSel = i
		m.activateSidebarSel()
		m.focus = focusInput
		m.input.Focus()
		return
	}
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	if m.wsCancel != nil {
		m.wsCancel()
	}
	if m.wsConn != nil {
		_ = m.wsConn.Close(websocket.StatusNormalClosure, "bye")
	}
	return *m, tea.Quit
}

func (m *model) submitInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" || m.activeBuffer == nil || m.wsConn == nil {
		return *m, nil
	}

	m.pushHistory(text)
	m.input.Reset()
	m.historyIdx = -1

	conn := m.wsConn
	bufID := m.activeBuffer.ID

	if strings.HasPrefix(text, "/") {
		if cmd, ok := parseSlash(text, m.activeBuffer); ok {
			return *m, func() tea.Msg {
				if err := sendWSCmd(context.Background(), conn, cmd); err != nil {
					return errMsg{err}
				}
				return nil
			}
		}
		m.status = "Unknown command: " + strings.Fields(text)[0]
		return *m, nil
	}

	return *m, func() tea.Msg {
		if err := sendMessage(context.Background(), conn, bufID, text); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m *model) pushHistory(line string) {
	const maxHist = 100
	// don't dupe identical previous entry
	if n := len(m.history); n > 0 && m.history[n-1] == line {
		return
	}
	m.history = append(m.history, line)
	if len(m.history) > maxHist {
		m.history = m.history[len(m.history)-maxHist:]
	}
}

func (m *model) recallHistory(delta int) bool {
	if len(m.history) == 0 {
		return false
	}
	if m.historyIdx < 0 {
		if delta > 0 {
			return false
		}
		m.historyIdx = len(m.history) - 1
	} else {
		m.historyIdx += delta
		if m.historyIdx < 0 {
			m.historyIdx = 0
		}
		if m.historyIdx >= len(m.history) {
			m.historyIdx = -1
			m.input.SetValue("")
			return true
		}
	}
	m.input.SetValue(m.history[m.historyIdx])
	m.input.CursorEnd()
	return true
}

// nickAutocomplete cycles through member nicks matching the last token in input.
func (m *model) nickAutocomplete() {
	val := m.input.Value()
	if m.activeBuffer == nil {
		return
	}
	members := m.members[m.activeBuffer.ID]
	if len(members) == 0 {
		return
	}

	// Determine token boundary: from end back to last whitespace.
	end := len(val)
	start := strings.LastIndexAny(val[:end], " \t")
	start++ // 0 if no space

	if m.tabPrefix == "" {
		m.tabPrefix = strings.ToLower(val[start:end])
		m.tabIdx = 0
		m.tabPos = start
	}

	// Collect candidates.
	cands := make([]string, 0, len(members))
	for _, mem := range members {
		if strings.HasPrefix(strings.ToLower(mem.Nick), m.tabPrefix) {
			cands = append(cands, mem.Nick)
		}
	}
	if len(cands) == 0 {
		return
	}
	sort.Strings(cands)
	pick := cands[m.tabIdx%len(cands)]
	m.tabIdx++

	suffix := ""
	if m.tabPos == 0 {
		suffix = ": "
	} else {
		suffix = " "
	}
	newVal := val[:m.tabPos] + pick + suffix
	m.input.SetValue(newVal)
	m.input.CursorEnd()
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
	b.LastSeenID = last
	b.MarkerID = uuid.Nil
	b.MarkerTS = ""
	m.unread[b.ID] = 0
	m.mentions[b.ID] = 0
	m.refreshViewport()
}

// ── state helpers ─────────────────────────────────────────────────────────────

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

func (m *model) applyState(s *stateResponse) {
	m.networks = s.Networks
	m.buffers = s.Buffers
	m.refreshActiveBuffer()
	for _, b := range s.Buffers {
		m.unread[b.ID] = b.Unread
		m.mentions[b.ID] = b.Mentions
	}
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

func (m *model) pinnedSidebarItems() []sidebarItem {
	netOrder := make(map[uuid.UUID]int, len(m.networks))
	enabled := make(map[uuid.UUID]bool, len(m.networks))
	for i, n := range m.networks {
		netOrder[n.ID] = i
		enabled[n.ID] = !n.Disabled
	}
	pinned := []bufferDTO{}
	for _, b := range m.buffers {
		if b.Kind == "channel" && b.Pinned && enabled[b.NetworkID] {
			pinned = append(pinned, b)
		}
	}
	if len(pinned) == 0 {
		return nil
	}
	sort.Slice(pinned, func(i, j int) bool {
		ni, nj := netOrder[pinned[i].NetworkID], netOrder[pinned[j].NetworkID]
		if ni != nj {
			return ni < nj
		}
		return strings.ToLower(pinned[i].Name) < strings.ToLower(pinned[j].Name)
	})
	items := make([]sidebarItem, 0, len(pinned)+1)
	items = append(items, sidebarItem{label: "⚑ Pinned", isHeader: true})
	for _, b := range pinned {
		items = append(items, sidebarItem{
			label:     "⚑ " + b.Name,
			bufferID:  b.ID,
			networkID: b.NetworkID,
		})
	}
	return items
}

func (m *model) rebuildSidebar() {
	bufsByNet := make(map[uuid.UUID][]bufferDTO)
	for _, b := range m.buffers {
		bufsByNet[b.NetworkID] = append(bufsByNet[b.NetworkID], b)
	}

	items := []sidebarItem{}
	items = append(items, m.pinnedSidebarItems()...)
	for _, n := range m.networks {
		if n.Disabled {
			continue
		}
		items = append(items, sidebarItem{
			label:     n.Name,
			networkID: n.ID,
			isHeader:  true,
		})
		for _, b := range bufsByNet[n.ID] {
			label := b.Name
			if b.Kind == "status" {
				label = "(status)"
			}
			items = append(items, sidebarItem{
				label:     label,
				bufferID:  b.ID,
				networkID: n.ID,
			})
		}
	}
	m.sidebarItems = items
	if m.sidebarSel >= len(items) {
		m.sidebarSel = 0
	}
}

func (m *model) moveSidebar(delta int) {
	n := len(m.sidebarItems)
	if n == 0 {
		return
	}
	idx := m.sidebarSel + delta
	for range n {
		idx = (idx + n) % n
		if !m.sidebarItems[idx].isHeader {
			break
		}
		idx += delta
	}
	m.sidebarSel = (idx + n) % n
	m.activateSidebarSel()
}

func (m *model) activateSidebarSel() {
	if m.sidebarSel >= len(m.sidebarItems) {
		return
	}
	item := m.sidebarItems[m.sidebarSel]
	if item.isHeader {
		return
	}
	if b := m.findBuffer(item.bufferID); b != nil {
		m.activeBuffer = b
	}
	// Entering a buffer never acks: marker, bar and badges persist until
	// the user explicitly acks (Esc / bar click).
	if m.activeBuffer != nil {
		if m.activeBuffer.ID != m.lastPersistedBuffer {
			if err := savePersistedBufferID(m.activeBuffer.ID); err == nil {
				m.lastPersistedBuffer = m.activeBuffer.ID
			}
		}
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
}

func (m *model) handleWSEvent(ev wsEvent) {
	switch ev.Type {
	case "message":
		m.applyMessageEvent(ev)
	case "buffer_update":
		m.applyBufferUpdate(ev)
	case "buffer_settings":
		m.applyBufferSettings(ev)
	case "network_state":
		m.networkStates[ev.NetworkID] = ev.State
	case "buffer_created":
		// Match backend defaults (db/buffer_settings.go newBufferSettings):
		// ShowPresenceEvents=true, CollapsePresenceEvents=false. Without
		// these defaults, presenceMode would treat the freshly created
		// buffer as "hide presence" until the next /api/state reload.
		m.buffers = append(m.buffers, bufferDTO{
			ID: ev.ID, NetworkID: ev.NetworkID, Name: ev.Name, Kind: ev.Kind,
			ShowPresenceEvents: true,
		})
		// append may have reslized m.buffers; refresh m.activeBuffer
		// before any subsequent code dereferences the (now stale) pointer.
		m.refreshActiveBuffer()
		m.rebuildSidebar()
	case "member_list":
		if ev.BufferID != uuid.Nil {
			m.members[ev.BufferID] = ev.Members
		}
	case "history_result":
		m.applyHistoryResult(ev)
	case "channel_list":
		m.applyChannelList(ev)
	}
}

func (m *model) applyChannelList(ev wsEvent) {
	if ev.NetworkID == uuid.Nil {
		return
	}
	m.channelList[ev.NetworkID] = append(m.channelList[ev.NetworkID], ev.Entries...)
	if !ev.Done {
		return
	}
	entries := m.channelList[ev.NetworkID]
	delete(m.channelList, ev.NetworkID)
	netName := ""
	if n := m.findNetwork(ev.NetworkID); n != nil {
		netName = n.Name
	}
	m.status = fmt.Sprintf("/list %s: %d channels", netName, len(entries))
}

func (m *model) applyMessageEvent(ev wsEvent) {
	parsed, _ := time.Parse(time.RFC3339Nano, ev.TS)
	msg := messageDTO{
		ID: ev.ID, NetworkID: ev.NetworkID, BufferID: ev.BufferID,
		TS: ev.TS, Sender: ev.Sender, Kind: ev.Kind, Target: ev.Target, Content: ev.Content,
		MentionsMe: ev.MentionsMe, Highlight: ev.Highlight, CountsAsUnread: ev.CountsAsUnread,
		TSParsed: parsed,
	}
	atBottom := m.viewport.AtBottom()
	m.messages[ev.BufferID] = append(m.messages[ev.BufferID], msg)

	// Unread accounting applies to every buffer, active included — there is
	// no "actively watching" suppression and no auto-ack. Self-authored
	// messages never count and never anchor; the id > LastSeenID guard keeps
	// history replays from spawning a marker.
	b := m.findBuffer(ev.BufferID)
	if b != nil && ev.CountsAsUnread && !ev.IsSelf && !uuidLTE(ev.ID, b.LastSeenID) {
		m.unread[ev.BufferID]++
		if ev.MentionsMe || ev.Highlight {
			m.mentions[ev.BufferID]++
		}
		if b.MarkerID == uuid.Nil {
			b.MarkerID = ev.ID
			b.MarkerTS = ev.TS
		}
	}
	if m.activeBuffer != nil && ev.BufferID == m.activeBuffer.ID {
		m.refreshViewport()
		if atBottom {
			m.viewport.GotoBottom()
		}
	}
}

func (m *model) applyBufferUpdate(ev wsEvent) {
	bufID := ev.ID // backend uses "id" for buffer_update, not "buffer_id"
	b := m.findBuffer(bufID)
	if b == nil {
		return
	}
	if ev.Topic != nil {
		b.Topic = *ev.Topic
	}
	if ev.TopicSetBy != nil {
		b.TopicSetBy = *ev.TopicSetBy
	}
	if ev.Joined != nil {
		b.Joined = *ev.Joined
	}
	// mark_read variant (discriminated by last_seen_id): counts and marker
	// are taken verbatim — a nil MarkerID means caught up and clears the
	// marker even on the active buffer (a remote ack dismisses everywhere).
	if ev.LastSeenID != uuid.Nil {
		b.LastSeenID = ev.LastSeenID
		m.unread[bufID] = ev.Unread
		m.mentions[bufID] = ev.Mentions
		if ev.MarkerID != nil {
			b.MarkerID = *ev.MarkerID
		} else {
			b.MarkerID = uuid.Nil
		}
		if ev.MarkerTS != nil {
			b.MarkerTS = *ev.MarkerTS
		} else {
			b.MarkerTS = ""
		}
		if m.activeBuffer != nil && m.activeBuffer.ID == bufID {
			m.refreshViewport()
		}
	}
}

func (m *model) applyBufferSettings(ev wsEvent) {
	if b := m.findBuffer(ev.ID); b != nil {
		b.ShowPresenceEvents = ev.ShowPresenceEvents
		b.CollapsePresenceEvents = ev.CollapsePresenceEvents
		if m.activeBuffer != nil && m.activeBuffer.ID == ev.ID {
			m.activeBuffer = b
			m.refreshViewport()
		}
	}
}

func (m *model) applyHistoryResult(ev wsEvent) {
	bufID := ev.BufferID
	m.historyLoading[bufID] = false
	if len(ev.Messages) == 0 {
		m.historyExhaust[bufID] = true
		return
	}
	for i := range ev.Messages {
		ev.Messages[i].TSParsed, _ = time.Parse(time.RFC3339Nano, ev.Messages[i].TS)
	}
	existing := m.messages[bufID]
	combined := make([]messageDTO, 0, len(ev.Messages)+len(existing))
	combined = append(combined, ev.Messages...)
	combined = append(combined, existing...)
	m.messages[bufID] = combined
	if m.activeBuffer != nil && m.activeBuffer.ID == bufID {
		m.refreshViewport()
	}
}

// ── viewport ──────────────────────────────────────────────────────────────────

func (m *model) resizeComponents() {
	rightW := m.width - sidebarWidth - 1
	if m.showMembers {
		rightW -= membersWidth + 1
	}
	rightW = max(rightW, 1)
	vpH := m.viewportHeight()

	if !m.ready {
		m.viewport = viewport.New(rightW, vpH)
		m.viewport.SetContent("")
	} else {
		m.viewport.Width = rightW
		m.viewport.Height = vpH
	}

	m.input.SetWidth(rightW)
}

// viewportHeight is the message viewport's row count: the fixed chrome rows
// plus one more when the unread bar is pinned above the viewport.
func (m *model) viewportHeight() int {
	h := max(m.height-headerHeight-separatorHeight-inputLines-statusHeight, 1)
	if m.unreadBarVisible() {
		h = max(h-1, 1)
	}
	return h
}

func (m *model) refreshViewport() {
	if m.ready {
		// Bar visibility changes with marker state; keep the viewport height
		// in sync so header+bar+viewport+input chrome always fills m.height.
		m.viewport.Height = m.viewportHeight()
	}
	if m.activeBuffer == nil {
		m.viewport.SetContent("No buffer selected")
		return
	}
	msgs := m.messages[m.activeBuffer.ID]
	if len(msgs) == 0 {
		m.viewport.SetContent("No messages yet")
		return
	}

	// own nick for this network
	ownNick := ""
	if n := m.findNetwork(m.activeBuffer.NetworkID); n != nil {
		ownNick = n.Nick
	}

	var sb strings.Builder
	anchor := m.activeBuffer.MarkerID
	for _, line := range renderBufferLines(msgs, ownNick, m.activeBuffer, anchor, m.viewport.Width) {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	m.viewport.SetContent(sb.String())
}

// renderBufferLines renders msgs with the "New messages" marker line above
// the anchored message. anchor == uuid.Nil (or an ID not in msgs) renders
// without a marker. The marker splits presence grouping — a netsplit run
// never spans the read boundary.
func renderBufferLines(msgs []messageDTO, ownNick string, buf *bufferDTO, anchor uuid.UUID, width int) []string {
	if anchor != uuid.Nil {
		for i := range msgs {
			if msgs[i].ID == anchor {
				out := groupAndFormatMessages(msgs[:i], ownNick, buf)
				out = append(out, markerLine(width))
				return append(out, groupAndFormatMessages(msgs[i:], ownNick, buf)...)
			}
		}
	}
	return groupAndFormatMessages(msgs, ownNick, buf)
}

// markerLine renders the horizontal "New messages" divider at width cols.
func markerLine(width int) string {
	const label = " New messages "
	fill := width - len(label)
	if fill < 4 {
		return styleMarker.Render(strings.TrimSpace(label))
	}
	left := fill / 2
	return styleMarker.Render(strings.Repeat("─", left) + label + strings.Repeat("─", fill-left))
}

// ── unread bar ────────────────────────────────────────────────────────────────

// unreadBarVisible reports whether the pinned unread bar shows above the
// message viewport: the active buffer has a server-derived marker and there
// is something loaded to ack (empty buffer → nothing renderable/ackable).
func (m *model) unreadBarVisible() bool {
	if m.activeBuffer == nil || len(m.messages[m.activeBuffer.ID]) == 0 {
		return false
	}
	// unread fallback: keeps the ack affordance available when the server
	// predates marker_id (version skew) — without it there is no way to
	// clear the badge at all.
	return m.activeBuffer.MarkerID != uuid.Nil || m.unread[m.activeBuffer.ID] > 0
}

// renderUnreadBar renders the one-line bar pinned between the header and the
// viewport. Shows the unread count; falls back to "new since <t>" when the
// count is unreliable (at the server cap of 1000) or the marker message is
// outside loaded history.
func (m *model) renderUnreadBar(width int) string {
	b := m.activeBuffer
	unread := m.unread[b.ID]
	markerLoaded := false
	for _, msg := range m.messages[b.ID] {
		if msg.ID == b.MarkerID {
			markerLoaded = true
			break
		}
	}
	var label string
	switch {
	case unread >= 1000 || !markerLoaded:
		label = " new since " + formatMarkerTime(b.MarkerTS) + " "
	case unread == 1:
		label = " 1 new message "
	default:
		label = fmt.Sprintf(" %d new messages ", unread)
	}
	fill := width - len(label)
	if fill < 4 {
		return styleMarker.Render(strings.TrimSpace(label))
	}
	left := fill / 2
	return styleMarker.Render(strings.Repeat("─", left) + label + strings.Repeat("─", fill-left))
}

// formatMarkerTime renders the marker boundary's RFC3339 timestamp in local
// time: today → "15:04", yesterday → "yesterday 15:04", else "Jan 2 15:04".
func formatMarkerTime(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return "??:??"
	}
	lt := t.Local()
	now := time.Now()
	sameDay := func(a, b time.Time) bool {
		ay, am, ad := a.Date()
		by, bm, bd := b.Date()
		return ay == by && am == bm && ad == bd
	}
	switch {
	case sameDay(lt, now):
		return lt.Format("15:04")
	case sameDay(lt, now.AddDate(0, 0, -1)):
		return "yesterday " + lt.Format("15:04")
	default:
		return lt.Format("Jan 2 15:04")
	}
}

// groupAndFormatMessages renders a buffer slice with netsplit collapsing.
// Plain (non-presence) messages render individually; runs of presence
// events are passed through irc.GroupPresence so netsplits become a single
// summary line. Status buffers always render raw to preserve debug visibility
// of server numerics and unfanned QUITs. Channel buffers honor
// show/collapse_presence_events flags from buffer settings.
func groupAndFormatMessages(msgs []messageDTO, ownNick string, buf *bufferDTO) []string {
	showPresence, collapse := presenceMode(buf)
	var out []string
	if !collapse {
		for _, msg := range msgs {
			if !showPresence && irc.IsPresenceKind(msg.Kind) {
				continue
			}
			out = append(out, formatMessage(msg, ownNick))
		}
		return out
	}
	var run []messageDTO
	flush := func() {
		if len(run) > 0 {
			out = append(out, flushPresenceRun(run, ownNick)...)
			run = run[:0]
		}
	}
	for _, msg := range msgs {
		if irc.IsPresenceKind(msg.Kind) {
			if !showPresence {
				continue
			}
			run = append(run, msg)
			continue
		}
		flush()
		out = append(out, formatMessage(msg, ownNick))
	}
	flush()
	return out
}

// presenceMode returns (showPresence, collapseAndGroup). Status buffers
// always render raw; non-status buffers obey their settings (defaulting to
// show=true, collapse=false to match backend defaults).
func presenceMode(buf *bufferDTO) (show, collapse bool) {
	if buf == nil || buf.Kind == "status" {
		return true, false
	}
	return buf.ShowPresenceEvents, buf.CollapsePresenceEvents
}

func flushPresenceRun(run []messageDTO, ownNick string) []string {
	entries := make([]irc.PresenceEntry, 0, len(run))
	idxByID := make(map[string]int, len(run))
	for i, m := range run {
		entries = append(entries, irc.PresenceEntry{
			ID: m.ID.String(), Kind: m.Kind, Sender: m.Sender, Content: m.Content, TS: m.TSParsed,
		})
		idxByID[m.ID.String()] = i
	}
	var out []string
	for _, g := range irc.GroupPresence(entries) {
		if g.Netsplit != nil {
			out = append(out, formatNetsplit(g.Netsplit))
			continue
		}
		var plain []messageDTO
		for _, p := range g.Plain {
			if i, ok := idxByID[p.ID]; ok {
				plain = append(plain, run[i])
			}
		}
		if len(plain) > 1 {
			out = append(out, formatPresenceSummary(plain))
			continue
		}
		for _, m := range plain {
			out = append(out, formatMessage(m, ownNick))
		}
	}
	return out
}

// presenceSummaryOrder mirrors the web client's PRESENCE_KINDS display
// order for collapsed-run summaries (web/src/messages.ts).
var presenceSummaryOrder = []string{"join", "part", "quit", "nick", "away", "back", "account", "chghost"}

// formatPresenceSummary renders a collapsed run of presence events as one
// summary line, matching the web client's presence-summary row. The TUI has
// no expand interaction — turning off collapse_presence_events on the
// buffer shows the raw rows.
func formatPresenceSummary(run []messageDTO) string {
	ts := styleTimestamp.Render(formatTS(run[0].TS))
	counts := map[string]int{}
	for _, m := range run {
		counts[m.Kind]++
	}
	parts := make([]string, 0, len(presenceSummaryOrder))
	for _, kind := range presenceSummaryOrder {
		if n := counts[kind]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, presenceKindLabel(kind, n)))
		}
	}
	body := fmt.Sprintf("+ %d presence events: %s", len(run), strings.Join(parts, ", "))
	return ts + " " + styleAction.Render(body)
}

func presenceKindLabel(kind string, count int) string {
	plural := count != 1
	switch kind {
	case "nick":
		if plural {
			return "nick changes"
		}
		return "nick change"
	case "away", "back":
		return kind
	case "account":
		if plural {
			return "account changes"
		}
		return "account change"
	case "chghost":
		if plural {
			return "host changes"
		}
		return "host change"
	}
	if plural {
		return kind + "s"
	}
	return kind
}

func formatNetsplit(ns *irc.NetsplitGroup) string {
	ts := styleTimestamp.Render(formatTSTime(ns.SplitTS))
	rejoined := len(ns.Rejoins)
	lost := max(len(ns.Quits)-rejoined, 0)
	body := fmt.Sprintf("↮ %s ⇎ %s · %d split (rejoined %d, lost %d)",
		ns.ServerA, ns.ServerB, len(ns.Quits), rejoined, lost)
	return ts + " " + styleAction.Render(body)
}

func formatTSTime(t time.Time) string {
	if t.IsZero() {
		return "[??:??]"
	}
	return t.Local().Format("[15:04]")
}

func formatMessage(m messageDTO, ownNick string) string {
	ts := styleTimestamp.Render(formatTS(m.TS))
	isSelf := m.Sender != "" && strings.EqualFold(m.Sender, ownNick)
	content := mircFormat(m.Content)
	// action prepends the timestamp outside any style wrap so styleAction's
	// italic only applies to the event body itself.
	action := func(body string) string {
		return ts + " " + styleAction.Render(body)
	}

	switch m.Kind {
	case "action":
		body := fmt.Sprintf("* %s %s", m.Sender, content)
		if (m.MentionsMe || m.Highlight) && !isSelf {
			return ts + " " + styleMentionLine.Render(body)
		}
		return action(body)
	case "privmsg", "message":
		sender := styledSender(m.Sender, isSelf)
		line := fmt.Sprintf("%s %s %s", ts, sender, content)
		if (m.MentionsMe || m.Highlight) && !isSelf {
			return styleMentionLine.Render(line)
		}
		return line
	case "notice":
		sender := lipgloss.NewStyle().
			Foreground(nickColor(m.Sender)).
			Bold(true).
			Render("-" + m.Sender + "-")
		return fmt.Sprintf("%s %s %s", ts, sender, content)
	case "join":
		return action(fmt.Sprintf("→ %s joined", m.Sender))
	case "part":
		if m.Content != "" {
			return action(fmt.Sprintf("← %s left (%s)", m.Sender, m.Content))
		}
		return action(fmt.Sprintf("← %s left", m.Sender))
	case "quit":
		if m.Content != "" {
			return action(fmt.Sprintf("⇠ %s quit (%s)", m.Sender, m.Content))
		}
		return action(fmt.Sprintf("⇠ %s quit", m.Sender))
	case "nick":
		return action(fmt.Sprintf("— %s is now %s", m.Sender, m.Target))
	case "kick":
		return action(fmt.Sprintf("⛔ %s kicked %s (%s)", m.Sender, m.Target, m.Content))
	case "mode":
		return action(fmt.Sprintf("⚙ %s set mode %s %s", m.Sender, m.Target, m.Content))
	case "topic":
		return action(fmt.Sprintf("📌 %s set topic: %s", m.Sender, content))
	case "invite":
		return action(fmt.Sprintf("✉ %s invited to %s", m.Sender, m.Target))
	case "away":
		if m.Content != "" {
			return action(fmt.Sprintf("💤 %s is away (%s)", m.Target, content))
		}
		return action(fmt.Sprintf("💤 %s is away", m.Target))
	case "back":
		return action(fmt.Sprintf("☀ %s is back", m.Target))
	case "account":
		if m.Content != "" {
			return action(fmt.Sprintf("— %s logged in as %s", m.Target, m.Content))
		}
		return action(fmt.Sprintf("— %s logged out", m.Target))
	case "chghost":
		return action(fmt.Sprintf("— %s changed host to %s", m.Target, m.Content))
	case "ctcp":
		return action(fmt.Sprintf("[CTCP %s] %s", m.Sender, content))
	default:
		if m.Sender != "" {
			return fmt.Sprintf("%s [%s] %s %s", ts, m.Kind, m.Sender, content)
		}
		return fmt.Sprintf("%s *** %s", ts, content)
	}
}

func formatTS(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return "[??:??]"
	}
	return t.Local().Format("[15:04]")
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "Initialising…\n"
	}
	if m.loading {
		return fmt.Sprintf("Loading from %s…\n", m.cfg.BackendURL)
	}

	sidebarH := m.height
	sidebar := m.renderSidebar(sidebarH)

	rightW := m.width - sidebarWidth - 1
	if m.showMembers {
		rightW -= membersWidth + 1
	}
	header := m.renderHeader(rightW)
	messages := m.viewport.View()
	inputBox := m.input.View()
	separator := styleSeparator.Width(rightW).Render(strings.Repeat("─", rightW))
	statusLine := styleStatus.Width(rightW).Render(m.status)

	rows := []string{header}
	if m.unreadBarVisible() {
		// Pinned bar between header and viewport (not scrolling content);
		// viewportHeight already gave this row back. Click acks.
		rows = append(rows, m.renderUnreadBar(rightW))
	}
	rows = append(rows, messages, separator, inputBox, statusLine)
	rightPane := lipgloss.JoinVertical(lipgloss.Left, rows...)

	cols := []string{
		styleSidebar.Height(sidebarH).Render(sidebar),
		rightPane,
	}
	if m.showMembers {
		cols = append(cols, m.renderMembers(sidebarH))
	}
	view := lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	if m.switcher.open {
		popup := m.switcher.render(min(60, m.width-4))
		return overlay(view, popup, m.width, m.height)
	}
	return view
}

func (m model) renderHeader(width int) string {
	if m.activeBuffer == nil {
		return styleHeader.Width(width).Render("lurker-tui")
	}
	topic := m.activeBuffer.Topic
	title := m.activeBuffer.Name
	if topic != "" {
		title += " — " + mircFormat(topic)
		if setBy := m.activeBuffer.TopicSetBy; setBy != "" {
			title += " (set by " + setBy + ")"
		}
	}
	return styleHeader.Width(width).Render(title)
}

// renderConnStatus draws the backend WS connection row at the top of the
// sidebar. Tailscale/update rows are webui-only and intentionally omitted.
func (m model) renderConnStatus() string {
	var dot, value string
	switch m.wsStatus {
	case "connected":
		dot = styleStatusOk.Render("●")
		value = "Connected"
	case "connecting":
		dot = styleStatusWarn.Render("●")
		value = "Connecting…"
	case "reconnecting":
		dot = styleStatusWarn.Render("●")
		value = "Reconnecting…"
	default:
		dot = styleStatusBad.Render("●")
		value = "Offline"
	}
	return styleStatusBox.Render(dot + " " + value)
}

func (m model) renderSidebar(height int) string {
	var sb strings.Builder
	sb.WriteString(m.renderConnStatus())
	sb.WriteByte('\n')
	sb.WriteString(styleSidebarSep.Width(sidebarWidth - 2).Render(strings.Repeat("─", sidebarWidth-2)))
	sb.WriteByte('\n')

	for i, item := range m.sidebarItems {
		var line string
		if item.isHeader {
			state := m.networkStates[item.networkID]
			label := item.label
			if state != "" && state != "connected" {
				label += " (" + state + ")"
			}
			line = styleNetHeader.Width(sidebarWidth - 2).Render(label)
		} else {
			unread := m.unread[item.bufferID]
			mentions := m.mentions[item.bufferID]
			label := item.label
			suffix := ""
			if unread > 0 {
				if unread > 99 {
					suffix = " (99+)"
				} else {
					suffix = fmt.Sprintf(" (%d)", unread)
				}
			}
			full := label + suffix

			switch {
			case i == m.sidebarSel:
				line = styleSelected.Width(sidebarWidth - 2).Render(full)
			case mentions > 0:
				line = styleBufferMention.Width(sidebarWidth - 2).Render(full)
			case unread > 0:
				line = styleBufferUnread.Width(sidebarWidth - 2).Render(full)
			default:
				line = styleBufferItem.Width(sidebarWidth - 2).Render(label)
			}
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	if len(m.sidebarItems) == 0 {
		sb.WriteString(styleBufferItem.Render("(no networks)"))
	}

	content := sb.String()
	lines := strings.Count(content, "\n")
	for i := lines; i < height-1; i++ {
		content += "\n"
	}
	return content
}

func (m model) renderMembers(height int) string {
	if m.activeBuffer == nil {
		return styleMembersPane.Width(membersWidth).Height(height).Render("")
	}
	members := m.members[m.activeBuffer.ID]
	if len(members) == 0 {
		return styleMembersPane.Width(membersWidth).Height(height).Render("(no members)")
	}
	sorted := make([]channelMember, len(members))
	copy(sorted, members)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri := memberRank(sorted[i].Prefix)
		rj := memberRank(sorted[j].Prefix)
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(sorted[i].Nick) < strings.ToLower(sorted[j].Nick)
	})

	var sb strings.Builder
	sb.WriteString(styleNetHeader.Width(membersWidth - 1).Render(fmt.Sprintf("Members (%d)", len(sorted))))
	sb.WriteByte('\n')
	for _, mem := range sorted {
		line := mem.Prefix + mem.Nick
		st := lipgloss.NewStyle().Background(lipgloss.Color(colorPanel)).Foreground(nickColor(mem.Nick))
		switch mem.Prefix {
		case "@":
			st = styleMemberOp
		case "+":
			st = styleMemberVoice
		}
		if mem.Away {
			st = styleMemberAway
		}
		sb.WriteString(st.Width(membersWidth - 1).Render(line))
		sb.WriteByte('\n')
	}
	return styleMembersPane.Width(membersWidth).Height(height).Render(sb.String())
}

func memberRank(prefix string) int {
	switch prefix {
	case "~":
		return 0
	case "&":
		return 1
	case "@":
		return 2
	case "%":
		return 3
	case "+":
		return 4
	}
	return 5
}
