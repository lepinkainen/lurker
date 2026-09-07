package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/irc"
)

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
		out := fmt.Sprintf("Loading from %s…\n", m.cfg.BackendURL)
		if m.status != "" {
			out += m.status + "\n"
		}
		return out
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
			case item.isArchiveToggle:
				line = styleArchiveToggle.Width(sidebarWidth - 2).Render(label)
			case mentions > 0:
				line = styleBufferMention.Width(sidebarWidth - 2).Render(full)
			case unread > 0:
				line = styleBufferUnread.Width(sidebarWidth - 2).Render(full)
			case item.dim:
				line = styleBufferArchived.Width(sidebarWidth - 2).Render(label)
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
