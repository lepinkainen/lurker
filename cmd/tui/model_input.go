package main

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

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
		// /archives is a pure UI toggle (Archives fold for the current
		// network) — it never reaches the server.
		if strings.EqualFold(text, "/archives") {
			m.toggleArchives(m.activeBuffer.NetworkID)
			return *m, nil
		}
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
