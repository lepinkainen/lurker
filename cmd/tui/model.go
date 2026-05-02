package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// layout constants
const (
	sidebarWidth = 26
	inputLines   = 3 // textarea height in rows
	statusHeight = 1
	headerHeight = 1
)

// styles
var (
	styleSidebar = lipgloss.NewStyle().
			Width(sidebarWidth).
			BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))

	styleNetHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("33"))

	styleBufferItem = lipgloss.NewStyle().
			PaddingLeft(2)

	styleSelected = lipgloss.NewStyle().
			PaddingLeft(2).
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("255"))

	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("33")).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))

	styleStatus = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)

	styleSender = lipgloss.NewStyle().Bold(true)

	styleTimestamp = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	styleAction = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
)

type focusArea int

const (
	focusInput   focusArea = iota
	focusSidebar           // up/down navigates buffers
)

type model struct {
	cfg    *Config
	client *apiClient

	networks      []networkDTO
	buffers       []bufferDTO
	networkStates map[uuid.UUID]string       // network_id -> state string
	messages      map[uuid.UUID][]messageDTO // buffer_id -> messages
	topics        map[uuid.UUID]string       // buffer_id -> topic
	sidebarItems  []sidebarItem
	sidebarSel    int
	activeBuffer  *bufferDTO

	viewport viewport.Model
	input    textarea.Model

	focus  focusArea
	width  int
	height int
	ready  bool

	wsConn   *websocket.Conn
	wsCancel context.CancelFunc
	wsChan   <-chan wsEvent

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
		cfg:           cfg,
		client:        newAPIClient(cfg.BackendURL),
		networkStates: make(map[uuid.UUID]string),
		messages:      make(map[uuid.UUID][]messageDTO),
		topics:        make(map[uuid.UUID]string),
		input:         ta,
		focus:         focusInput,
		status:        "Connecting…",
		loading:       true,
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
		_ = cancel
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
		// stash cancel so it can be called on quit; carry it via a closure trick:
		// we embed a *context.CancelFunc in the message.
		_ = cancel // ownership transferred to model via wsConnectedMsg
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
		m.status = "Loaded — connecting WS…"
		return m, connectWSCmd(m.client)

	case wsConnectedMsg:
		m.wsConn = msg.conn
		m.wsCancel = msg.cancel
		m.wsChan = msg.ch
		m.status = fmt.Sprintf("Connected to %s", m.cfg.BackendURL)
		return m, waitForWSEvent(m.wsChan)

	case wsEventMsg:
		m.handleWSEvent(wsEvent(msg))
		return m, waitForWSEvent(m.wsChan)

	case wsErrorMsg:
		m.status = fmt.Sprintf("WS error: %v — reconnecting in 5s…", msg.err)
		m.wsConn = nil
		m.wsChan = nil
		return m, reconnectWSCmd(m.client, 5*time.Second)

	case errMsg:
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.loading = false

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward key events to textarea when input is focused.
	if m.focus == focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()

	case "tab":
		if m.focus == focusInput {
			m.focus = focusSidebar
			m.input.Blur()
		} else {
			m.focus = focusInput
			m.input.Focus()
		}
		return *m, nil

	case "up":
		if m.focus == focusSidebar {
			m.moveSidebar(-1)
			return *m, nil
		}
		// scroll viewport when in input focus
		m.viewport.ScrollUp(3)
		return *m, nil

	case "down":
		if m.focus == focusSidebar {
			m.moveSidebar(1)
			return *m, nil
		}
		m.viewport.ScrollDown(3)
		return *m, nil

	case "pgup":
		m.viewport.HalfPageUp()
		return *m, nil

	case "pgdown":
		m.viewport.HalfPageDown()
		return *m, nil

	case "enter":
		if m.focus == focusSidebar {
			m.activateSidebarSel()
			m.focus = focusInput
			m.input.Focus()
			return *m, nil
		}
		return m.submitInput()
	}

	// All other keys go to the textarea.
	if m.focus == focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return *m, cmd
	}
	return *m, nil
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

	bufID := m.activeBuffer.ID
	conn := m.wsConn
	m.input.Reset()

	return *m, func() tea.Msg {
		if err := sendMessage(context.Background(), conn, bufID, text); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// ── state helpers ─────────────────────────────────────────────────────────────

func (m *model) applyState(s *stateResponse) {
	m.networks = s.Networks
	m.buffers = s.Buffers
	for _, b := range s.Buffers {
		if b.Topic != "" {
			m.topics[b.ID] = b.Topic
		}
	}
	for key, msgs := range s.InitialMessages {
		id, err := uuid.Parse(key)
		if err != nil {
			continue
		}
		m.messages[id] = msgs
	}
	m.rebuildSidebar()
	if len(m.sidebarItems) > 0 {
		m.activateSidebarSel()
	}
}

func (m *model) rebuildSidebar() {
	netIndex := make(map[uuid.UUID]networkDTO, len(m.networks))
	for _, n := range m.networks {
		netIndex[n.ID] = n
	}

	// Group buffers by network, preserving network sort order.
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
}

func (m *model) moveSidebar(delta int) {
	n := len(m.sidebarItems)
	if n == 0 {
		return
	}
	idx := m.sidebarSel + delta
	// wrap and skip headers
	for i := 0; i < n; i++ {
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
	m.refreshViewport()
}

func (m *model) handleWSEvent(ev wsEvent) {
	switch ev.Type {
	case "message":
		msg := messageDTO{
			ID: ev.ID, NetworkID: ev.NetworkID, BufferID: ev.BufferID,
			TS: ev.TS, Sender: ev.Sender, Kind: ev.Kind, Content: ev.Content,
		}
		atBottom := m.viewport.AtBottom()
		m.messages[ev.BufferID] = append(m.messages[ev.BufferID], msg)
		if m.activeBuffer != nil && ev.BufferID == m.activeBuffer.ID {
			m.refreshViewport()
			if atBottom {
				m.viewport.GotoBottom()
			}
		}

	case "buffer_update":
		bufID := ev.ID // backend uses "id" for buffer_update, not "buffer_id"
		if ev.Topic != "" {
			m.topics[bufID] = ev.Topic
		}
		for i := range m.buffers {
			if m.buffers[i].ID == bufID {
				if ev.Topic != "" {
					m.buffers[i].Topic = ev.Topic
				}
				m.buffers[i].Joined = ev.Joined
				break
			}
		}

	case "network_state":
		m.networkStates[ev.NetworkID] = ev.State

	case "buffer_created":
		buf := bufferDTO{
			ID: ev.ID, NetworkID: ev.NetworkID,
			Name: ev.Name, Kind: ev.Kind,
		}
		m.buffers = append(m.buffers, buf)
		m.rebuildSidebar()
	}
}

// ── viewport ──────────────────────────────────────────────────────────────────

func (m *model) resizeComponents() {
	// sidebar border takes 1 col on the right
	contentW := m.width - sidebarWidth - 1
	if contentW < 1 {
		contentW = 1
	}
	vpH := m.height - headerHeight - inputLines - statusHeight - 2 // 2 for borders
	if vpH < 1 {
		vpH = 1
	}

	if !m.ready {
		m.viewport = viewport.New(contentW, vpH)
		m.viewport.SetContent("")
	} else {
		m.viewport.Width = contentW
		m.viewport.Height = vpH
	}

	m.input.SetWidth(contentW)
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

	var sb strings.Builder
	for _, msg := range msgs {
		sb.WriteString(formatMessage(msg))
		sb.WriteByte('\n')
	}
	m.viewport.SetContent(sb.String())
}

func formatMessage(m messageDTO) string {
	ts := formatTS(m.TS)
	switch m.Kind {
	case "action":
		return styleAction.Render(fmt.Sprintf("%s * %s %s",
			styleTimestamp.Render(ts), m.Sender, m.Content))
	case "privmsg":
		return fmt.Sprintf("%s %s %s",
			styleTimestamp.Render(ts),
			styleSender.Render("<"+m.Sender+">"),
			m.Content)
	case "notice":
		return fmt.Sprintf("%s %s %s",
			styleTimestamp.Render(ts),
			styleSender.Render("-"+m.Sender+"-"),
			m.Content)
	default:
		if m.Sender != "" {
			return fmt.Sprintf("%s [%s] %s", styleTimestamp.Render(ts), m.Sender, m.Content)
		}
		return fmt.Sprintf("%s *** %s", styleTimestamp.Render(ts), m.Content)
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

	header := m.renderHeader()
	messages := m.viewport.View()
	inputBox := m.input.View()
	status := styleStatus.Width(m.width - sidebarWidth - 1).Render(m.status)

	rightPane := lipgloss.JoinVertical(lipgloss.Left,
		header,
		messages,
		inputBox,
		status,
	)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		styleSidebar.Height(sidebarH).Render(sidebar),
		rightPane,
	)
}

func (m model) renderHeader() string {
	contentW := m.width - sidebarWidth - 1
	if m.activeBuffer == nil {
		return styleHeader.Width(contentW).Render("lurker-tui")
	}
	topic := m.topics[m.activeBuffer.ID]
	title := m.activeBuffer.Name
	if topic != "" {
		title += " — " + topic
	}
	return styleHeader.Width(contentW).Render(title)
}

func (m model) renderSidebar(height int) string {
	var sb strings.Builder
	for i, item := range m.sidebarItems {
		var line string
		if item.isHeader {
			state := m.networkStates[item.networkID]
			label := item.label
			if state != "" && state != "connected" {
				label += " (" + state + ")"
			}
			line = styleNetHeader.Width(sidebarWidth - 2).Render(label)
		} else if i == m.sidebarSel {
			line = styleSelected.Width(sidebarWidth - 2).Render(item.label)
		} else {
			line = styleBufferItem.Width(sidebarWidth - 2).Render(item.label)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	if len(m.sidebarItems) == 0 {
		sb.WriteString(styleBufferItem.Render("(no networks)"))
	}

	// pad to fill height so the border extends the full terminal height
	content := sb.String()
	lines := strings.Count(content, "\n")
	for i := lines; i < height-1; i++ {
		content += "\n"
	}
	return content
}
