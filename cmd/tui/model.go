package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/irc"
)

// layout constants
const (
	sidebarWidth = 26
	membersWidth = 22
	inputLines   = 3 // textarea height in rows
	statusHeight = 1
	headerHeight = 1
)

type focusArea int

const (
	focusInput focusArea = iota
	focusSidebar
)

type model struct {
	cfg    *Config
	client *apiClient

	networks      []networkDTO
	buffers       []bufferDTO
	networkStates map[uuid.UUID]string          // network_id -> state string
	messages      map[uuid.UUID][]messageDTO    // buffer_id -> messages
	topics        map[uuid.UUID]string          // buffer_id -> topic
	members       map[uuid.UUID][]channelMember // buffer_id -> members
	unread        map[uuid.UUID]int             // buffer_id -> unread count (client-side accumulated)
	mentions      map[uuid.UUID]int             // buffer_id -> mention count
	sidebarItems  []sidebarItem
	sidebarSel    int
	activeBuffer  *bufferDTO

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
		topics:         make(map[uuid.UUID]string),
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
		m.backendOK = true
		m.wsStatus = "connected"
		m.status = fmt.Sprintf("Connected to %s", m.cfg.BackendURL)
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
		return m, reconnectWSCmd(m.client, 5*time.Second)

	case errMsg:
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.loading = false

	case historyFailedMsg:
		delete(m.historyLoading, msg.bufferID)
		m.status = fmt.Sprintf("History request failed: %v", msg.err)

	case tea.KeyMsg:
		return m.handleKey(msg)
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

// refreshActiveBuffer re-resolves the m.activeBuffer pointer to the
// current m.buffers slice. Call after any operation that may have
// replaced or reslized m.buffers (applyState, buffer_created append) —
// otherwise the pointer would alias a stale backing array.
func (m *model) refreshActiveBuffer() {
	if m.activeBuffer == nil {
		return
	}
	id := m.activeBuffer.ID
	for i := range m.buffers {
		if m.buffers[i].ID == id {
			m.activeBuffer = &m.buffers[i]
			return
		}
	}
	m.activeBuffer = nil
}

func (m *model) applyState(s *stateResponse) {
	m.networks = s.Networks
	m.buffers = s.Buffers
	m.refreshActiveBuffer()
	for _, b := range s.Buffers {
		if b.Topic != "" {
			m.topics[b.ID] = b.Topic
		}
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
		m.activateSidebarSel()
	}
}

func (m *model) rebuildSidebar() {
	bufsByNet := make(map[uuid.UUID][]bufferDTO)
	for _, b := range m.buffers {
		bufsByNet[b.NetworkID] = append(bufsByNet[b.NetworkID], b)
	}

	items := []sidebarItem{}
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
	for i := range m.buffers {
		if m.buffers[i].ID == item.bufferID {
			m.activeBuffer = &m.buffers[i]
			break
		}
	}
	// Clear unread when buffer becomes active.
	// TODO: wire mark_read ws cmd once TUI is stable; for now we only clear
	// local counters so badges disappear visually.
	if m.activeBuffer != nil {
		m.unread[m.activeBuffer.ID] = 0
		m.mentions[m.activeBuffer.ID] = 0
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
		MentionsMe: ev.MentionsMe, CountsAsUnread: ev.CountsAsUnread,
		TSParsed: parsed,
	}
	atBottom := m.viewport.AtBottom()
	m.messages[ev.BufferID] = append(m.messages[ev.BufferID], msg)

	isActive := m.activeBuffer != nil && ev.BufferID == m.activeBuffer.ID
	if !isActive && ev.CountsAsUnread {
		m.unread[ev.BufferID]++
		if ev.MentionsMe {
			m.mentions[ev.BufferID]++
		}
		return
	}
	if !isActive {
		return
	}
	m.refreshViewport()
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *model) applyBufferUpdate(ev wsEvent) {
	bufID := ev.ID // backend uses "id" for buffer_update, not "buffer_id"
	if ev.Topic != "" {
		m.topics[bufID] = ev.Topic
	}
	for i := range m.buffers {
		if m.buffers[i].ID != bufID {
			continue
		}
		if ev.Topic != "" {
			m.buffers[i].Topic = ev.Topic
		}
		m.buffers[i].Joined = ev.Joined
		if ev.LastSeenID != uuid.Nil {
			m.buffers[i].LastSeenID = ev.LastSeenID
			m.unread[bufID] = ev.Unread
			m.mentions[bufID] = ev.Mentions
		}
		return
	}
}

func (m *model) applyBufferSettings(ev wsEvent) {
	bufID := ev.ID
	for i := range m.buffers {
		if m.buffers[i].ID != bufID {
			continue
		}
		m.buffers[i].ShowPresenceEvents = ev.ShowPresenceEvents
		m.buffers[i].CollapsePresenceEvents = ev.CollapsePresenceEvents
		if m.activeBuffer != nil && m.activeBuffer.ID == bufID {
			m.activeBuffer = &m.buffers[i]
			m.refreshViewport()
		}
		return
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
	if rightW < 1 {
		rightW = 1
	}
	vpH := m.height - headerHeight - inputLines - statusHeight - 2
	if vpH < 1 {
		vpH = 1
	}

	if !m.ready {
		m.viewport = viewport.New(rightW, vpH)
		m.viewport.SetContent("")
	} else {
		m.viewport.Width = rightW
		m.viewport.Height = vpH
	}

	m.input.SetWidth(rightW)
}

func (m *model) refreshViewport() {
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
	for _, line := range groupAndFormatMessages(msgs, ownNick, m.activeBuffer) {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	m.viewport.SetContent(sb.String())
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
		for _, p := range g.Plain {
			if i, ok := idxByID[p.ID]; ok {
				out = append(out, formatMessage(run[i], ownNick))
			}
		}
	}
	return out
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
		if m.MentionsMe && !isSelf {
			return ts + " " + styleMentionLine.Render(body)
		}
		return action(body)
	case "privmsg", "message":
		sender := styledSender(m.Sender, isSelf)
		line := fmt.Sprintf("%s %s %s", ts, sender, content)
		if m.MentionsMe && !isSelf {
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

	sidebarH := m.height - statusHeight
	sidebar := m.renderSidebar(sidebarH)

	rightW := m.width - sidebarWidth - 1
	if m.showMembers {
		rightW -= membersWidth + 1
	}
	header := m.renderHeader(rightW)
	messages := m.viewport.View()
	inputBox := m.input.View()
	statusLine := styleStatus.Width(rightW).Render(m.status)

	rightPane := lipgloss.JoinVertical(lipgloss.Left,
		header,
		messages,
		inputBox,
		statusLine,
	)

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
	topic := m.topics[m.activeBuffer.ID]
	title := m.activeBuffer.Name
	if topic != "" {
		title += " — " + mircFormat(topic)
	}
	return styleHeader.Width(width).Render(title)
}

// renderConnStatus draws the connection status row at the top of the sidebar.
// TODO: add tailscale + update-available rows once TUI is feature-stable.
func (m model) renderConnStatus() string {
	var dot, label string
	switch m.wsStatus {
	case "connected":
		dot = styleStatusOk.Render("●")
		label = "Connected"
	case "connecting":
		dot = styleStatusWarn.Render("●")
		label = "Connecting…"
	case "reconnecting":
		dot = styleStatusWarn.Render("●")
		label = "Reconnecting…"
	default:
		dot = styleStatusBad.Render("●")
		label = "Offline"
	}
	return styleStatusBox.Render(dot + " " + label)
}

func (m model) renderSidebar(height int) string {
	var sb strings.Builder
	sb.WriteString(m.renderConnStatus())
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
