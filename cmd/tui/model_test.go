package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// Minimal model for tests: skip textarea/viewport state, populate only
// what each test exercises.
func testModel() *model {
	m := &model{
		networkStates:  map[uuid.UUID]string{},
		messages:       map[uuid.UUID][]messageDTO{},
		topics:         map[uuid.UUID]string{},
		members:        map[uuid.UUID][]channelMember{},
		unread:         map[uuid.UUID]int{},
		mentions:       map[uuid.UUID]int{},
		markerAnchor:   map[uuid.UUID]uuid.UUID{},
		lastMarkedRead: map[uuid.UUID]uuid.UUID{},
		historyLoading: map[uuid.UUID]bool{},
		historyExhaust: map[uuid.UUID]bool{},
		channelList:    map[uuid.UUID][]channelListEntry{},
	}
	return m
}

// Regression for 71e1e52 (fix(tui): default new buffers to show presence
// events). buffer_created on the live stream constructed bufferDTO with
// zero-value ShowPresenceEvents=false, so joins/parts/quits hid in fresh
// channel buffers until a full /api/state reload.
func TestBufferCreatedDefaultsShowPresence(t *testing.T) {
	m := testModel()
	bufID := uuid.New()
	m.handleWSEvent(wsEvent{
		Type: "buffer_created", ID: bufID,
		NetworkID: uuid.New(), Name: "#test", Kind: "channel",
	})
	if len(m.buffers) != 1 {
		t.Fatalf("expected 1 buffer, got %d", len(m.buffers))
	}
	b := m.buffers[0]
	if !b.ShowPresenceEvents {
		t.Errorf("ShowPresenceEvents=false on newly created buffer; want true")
	}
	if b.CollapsePresenceEvents {
		t.Errorf("CollapsePresenceEvents=true on newly created buffer; want false")
	}
}

// Regression for 15fa1c3 (fix(tui): clear historyLoading on outbound
// history send error). A WS write error must clear the loading flag,
// otherwise pgup at the top of the buffer becomes a silent no-op forever.
func TestHistoryFailedClearsLoadingFlag(t *testing.T) {
	m := testModel()
	bufID := uuid.New()
	m.historyLoading[bufID] = true
	updated, _ := m.Update(historyFailedMsg{bufferID: bufID, err: errors.New("boom")})
	mm := updated.(model)
	if mm.historyLoading[bufID] {
		t.Errorf("historyLoading[%s] still true after failure", bufID)
	}
	if mm.status == "" {
		t.Errorf("expected status line to mention failure, got empty")
	}
}

// Regression for 706685c (feat(tui): handle channel_list WS events for
// /list). Entries must accumulate across streamed batches and the final
// Done=true batch surfaces the count + drops the accumulator.
func TestChannelListStreamsAndFinalizes(t *testing.T) {
	m := testModel()
	netID := uuid.New()
	m.networks = []networkDTO{{ID: netID, Name: "ircnet"}}

	m.handleWSEvent(wsEvent{Type: "channel_list", NetworkID: netID, Entries: []channelListEntry{
		{Name: "#a", Count: 1}, {Name: "#b", Count: 2},
	}})
	if len(m.channelList[netID]) != 2 {
		t.Fatalf("batch 1: got %d entries", len(m.channelList[netID]))
	}
	m.handleWSEvent(wsEvent{Type: "channel_list", NetworkID: netID, Entries: []channelListEntry{
		{Name: "#c", Count: 3},
	}, Done: true})
	if _, ok := m.channelList[netID]; ok {
		t.Errorf("expected channelList accumulator cleared on Done")
	}
	if m.status == "" || m.status == "Connecting…" {
		t.Errorf("status not updated on /list completion: %q", m.status)
	}
}

// Regression for the simplify-pass fix (m.activeBuffer pointer dangling
// after m.buffers reslices via append). Construct a slice with cap=len,
// point m.activeBuffer at it, then trigger buffer_created which appends
// (forcing reallocation). refreshActiveBuffer must re-resolve the pointer
// to the new backing array.
func TestRefreshActiveBufferAfterAppend(t *testing.T) {
	m := testModel()
	activeID := uuid.New()
	// Force cap=len so append must reallocate.
	m.buffers = append(make([]bufferDTO, 0, 1), bufferDTO{ID: activeID, Kind: "channel"})
	m.activeBuffer = &m.buffers[0]
	oldPtr := m.activeBuffer

	m.handleWSEvent(wsEvent{Type: "buffer_created", ID: uuid.New(), Kind: "channel"})

	if m.activeBuffer == nil {
		t.Fatal("activeBuffer became nil after buffer_created")
	}
	if m.activeBuffer.ID != activeID {
		t.Fatalf("activeBuffer ID changed: %s", m.activeBuffer.ID)
	}
	if m.activeBuffer == oldPtr && len(m.buffers) > 1 && cap(m.buffers) > 1 {
		// If reallocation happened, the pointer must have moved. Allow
		// equality only if reallocation did not actually occur.
		// (Go's append is allowed to reuse the backing array.) Sanity check
		// that the pointer is into the live slice:
		if &m.buffers[0] != m.activeBuffer {
			t.Errorf("activeBuffer aliases stale backing array")
		}
	}
	// Final sanity: pointer must address the current slice.
	if &m.buffers[0] != m.activeBuffer {
		t.Errorf("activeBuffer does not address live slice element")
	}
}

// Left-click on a sidebar channel row must activate that buffer. Sidebar
// rows: 0 = connection status, 1 = separator, 2 = network header, 3+ =
// buffers (matches renderSidebar layout via sidebarChromeRows).
func TestMouseClickSidebarSelectsBuffer(t *testing.T) {
	m := newModel(&Config{})
	netID := uuid.New()
	b1, b2 := uuid.New(), uuid.New()
	m.networks = []networkDTO{{ID: netID, Name: "ircnet"}}
	m.buffers = []bufferDTO{
		{ID: b1, NetworkID: netID, Name: "#a", Kind: "channel"},
		{ID: b2, NetworkID: netID, Name: "#b", Kind: "channel"},
	}
	m.rebuildSidebar()
	// Buffer activation persists tui-state.json under os.UserConfigDir —
	// point HOME at a temp dir so tests never touch the user's real config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	click := func(x, y int) model {
		updated, _ := m.Update(tea.MouseMsg{
			X: x, Y: y,
			Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		})
		return updated.(model)
	}

	// Click second buffer row (y=4 → #b).
	mm := click(3, 4)
	if mm.activeBuffer == nil || mm.activeBuffer.ID != b2 {
		t.Fatalf("click on #b row: activeBuffer = %v, want %s", mm.activeBuffer, b2)
	}
	// sidebarItems: [0]=header, [1]=#a, [2]=#b
	if mm.sidebarSel != 2 {
		t.Errorf("sidebarSel = %d, want 2", mm.sidebarSel)
	}

	// Click on network header (y=2) must not change selection.
	m = mm
	mm = click(3, 2)
	if mm.activeBuffer == nil || mm.activeBuffer.ID != b2 {
		t.Errorf("header click changed active buffer to %v", mm.activeBuffer)
	}

	// Click outside the sidebar (x >= sidebarWidth) must not change selection.
	mm = click(sidebarWidth+5, 3)
	if mm.activeBuffer == nil || mm.activeBuffer.ID != b2 {
		t.Errorf("viewport click changed active buffer to %v", mm.activeBuffer)
	}
}

// urlAtCol must hit-test display columns against URL spans in an
// ANSI-styled line (mouse capture blocks Ghostty's native link click, so
// the TUI opens URLs itself).
func TestURLAtCol(t *testing.T) {
	line := "\x1b[38;2;91;101;115m[12:00]\x1b[0m <nick> see https://example.com/x now"
	// plain: "[12:00] <nick> see https://example.com/x now"
	// URL spans display cols 19–39 (21 chars).
	if url, ok := urlAtCol(line, 19); !ok || url != "https://example.com/x" {
		t.Errorf("col 19: got (%q, %v), want URL hit", url, ok)
	}
	if url, ok := urlAtCol(line, 39); !ok || url != "https://example.com/x" {
		t.Errorf("col 39 (last char): got (%q, %v), want URL hit", url, ok)
	}
	if _, ok := urlAtCol(line, 18); ok {
		t.Errorf("col 18 (space before URL): unexpected hit")
	}
	if _, ok := urlAtCol(line, 40); ok {
		t.Errorf("col 40 (space after URL): unexpected hit")
	}
	if _, ok := urlAtCol("no links here", 3); ok {
		t.Errorf("line without URL: unexpected hit")
	}
}

func TestPresenceModeStatusBufferAlwaysRaw(t *testing.T) {
	// Status buffers must render presence raw regardless of flags so the
	// netadmin view (image #6 in the original bug report) keeps showing
	// per-user QUITs.
	buf := &bufferDTO{Kind: "status", ShowPresenceEvents: false, CollapsePresenceEvents: true}
	show, collapse := presenceMode(buf)
	if !show || collapse {
		t.Errorf("status: show=%v collapse=%v, want (true,false)", show, collapse)
	}
}

func TestPresenceModeChannelHonorsFlags(t *testing.T) {
	buf := &bufferDTO{Kind: "channel", ShowPresenceEvents: true, CollapsePresenceEvents: true}
	show, collapse := presenceMode(buf)
	if !show || !collapse {
		t.Errorf("channel collapse: show=%v collapse=%v, want (true,true)", show, collapse)
	}
	buf.ShowPresenceEvents = false
	show, collapse = presenceMode(buf)
	if show {
		t.Errorf("channel hidden: show=%v, want false", show)
	}
}

// Collapsed presence runs must fold into one summary line (parity with the
// web presence-summary row and the SwiftUI DisclosureGroup), ordered and
// labeled like web/src/messages.ts presenceKindLabel.
func TestGroupAndFormatCollapsesPresenceRun(t *testing.T) {
	buf := &bufferDTO{Kind: "channel", ShowPresenceEvents: true, CollapsePresenceEvents: true}
	msgs := []messageDTO{
		{ID: seqUUID(1), TS: "2026-07-28T10:00:00Z", Sender: "alice", Kind: "away", Target: "alice", Content: "lunch"},
		{ID: seqUUID(2), TS: "2026-07-28T10:01:00Z", Sender: "alice", Kind: "back", Target: "alice"},
		{ID: seqUUID(3), TS: "2026-07-28T10:02:00Z", Sender: "bob", Kind: "nick", Target: "bob2"},
		{ID: seqUUID(4), TS: "2026-07-28T10:03:00Z", Sender: "carol", Kind: "join"},
		{ID: seqUUID(5), TS: "2026-07-28T10:04:00Z", Sender: "dave", Kind: "privmsg", Content: "hello"},
	}

	out := groupAndFormatMessages(msgs, "tester", buf)

	if len(out) != 2 {
		t.Fatalf("lines = %d, want 2 (summary + privmsg): %q", len(out), out)
	}
	want := "+ 4 presence events: 1 join, 1 nick change, 1 away, 1 back"
	if !strings.Contains(out[0], want) {
		t.Errorf("summary = %q, want contains %q", out[0], want)
	}
	if !strings.Contains(out[1], "hello") {
		t.Errorf("second line = %q, want privmsg", out[1])
	}
}

// A single presence event between messages renders as a normal row, not a
// one-item summary.
func TestGroupAndFormatSinglePresenceRendersRaw(t *testing.T) {
	buf := &bufferDTO{Kind: "channel", ShowPresenceEvents: true, CollapsePresenceEvents: true}
	msgs := []messageDTO{
		{ID: seqUUID(1), TS: "2026-07-28T10:00:00Z", Sender: "alice", Kind: "away", Target: "alice", Content: "lunch"},
		{ID: seqUUID(2), TS: "2026-07-28T10:01:00Z", Sender: "dave", Kind: "privmsg", Content: "hello"},
	}

	out := groupAndFormatMessages(msgs, "tester", buf)

	if len(out) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(out), out)
	}
	if !strings.Contains(out[0], "alice is away (lunch)") {
		t.Errorf("away line = %q, want raw away row", out[0])
	}
}

// Hidden presence (show_presence_events=false) drops the new kinds too.
func TestGroupAndFormatHidesAllPresenceKinds(t *testing.T) {
	buf := &bufferDTO{Kind: "channel", ShowPresenceEvents: false, CollapsePresenceEvents: false}
	msgs := []messageDTO{
		{ID: seqUUID(1), TS: "2026-07-28T10:00:00Z", Sender: "alice", Kind: "away", Target: "alice"},
		{ID: seqUUID(2), TS: "2026-07-28T10:01:00Z", Sender: "alice", Kind: "account", Target: "alice", Content: "acct"},
		{ID: seqUUID(3), TS: "2026-07-28T10:02:00Z", Sender: "alice", Kind: "chghost", Target: "alice", Content: "u h"},
		{ID: seqUUID(4), TS: "2026-07-28T10:03:00Z", Sender: "dave", Kind: "privmsg", Content: "hello"},
	}

	out := groupAndFormatMessages(msgs, "tester", buf)

	if len(out) != 1 || !strings.Contains(out[0], "hello") {
		t.Fatalf("lines = %q, want only privmsg", out)
	}
}

// ── new-messages marker (parity with web; see ai-docs/behaviors/new-messages-marker.md) ──

// seqUUID builds deterministic, byte-ordered UUIDs so tests can rely on
// message-ID ordering the way UUIDv7 provides it in production.
func seqUUID(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", n))
}

// markerModel returns a model with two channel buffers on one network,
// sidebar built, buffer A active, and a WS send recorder installed.
func markerModel(t *testing.T) (*model, uuid.UUID, uuid.UUID, *[]wsCmd) {
	t.Helper()
	m := testModel()
	netID := uuid.New()
	a, b := seqUUID(1), seqUUID(2)
	m.networks = []networkDTO{{ID: netID, Name: "ircnet"}}
	m.buffers = []bufferDTO{
		{ID: a, NetworkID: netID, Name: "#a", Kind: "channel"},
		{ID: b, NetworkID: netID, Name: "#b", Kind: "channel"},
	}
	m.rebuildSidebar()
	var sent []wsCmd
	m.sendWS = func(cmd wsCmd) error {
		sent = append(sent, cmd)
		return nil
	}
	m.activateBufferByID(a)
	sent = sent[:0] // drop mark_read from initial activation
	return m, a, b, &sent
}

// activateBufferByID selects the sidebar row for id and activates it.
func (m *model) activateBufferByID(id uuid.UUID) {
	m.lastPersistedBuffer = id // skip tui-state.json write in tests
	for i, item := range m.sidebarItems {
		if !item.isHeader && item.bufferID == id {
			m.sidebarSel = i
			m.activateSidebarSel()
			return
		}
	}
	panic("buffer not in sidebar: " + id.String())
}

func msgEvent(bufID, msgID uuid.UUID) wsEvent {
	return wsEvent{
		Type: "message", ID: msgID, BufferID: bufID,
		Sender: "alice", Kind: "privmsg", Content: "hi",
		CountsAsUnread: true,
	}
}

func TestMarkerAnchorSetOnInactiveArrivalAndSticky(t *testing.T) {
	m, _, b, _ := markerModel(t)
	first, second := seqUUID(100), seqUUID(101)
	m.handleWSEvent(msgEvent(b, first))
	if got := m.markerAnchor[b]; got != first {
		t.Fatalf("anchor = %v, want %v", got, first)
	}
	m.handleWSEvent(msgEvent(b, second))
	if got := m.markerAnchor[b]; got != first {
		t.Errorf("anchor moved to %v after second arrival, want sticky %v", got, first)
	}
	if m.unread[b] != 2 {
		t.Errorf("unread = %d, want 2", m.unread[b])
	}
}

func TestMarkerAnchorNotSetOnActiveArrival(t *testing.T) {
	m, a, _, _ := markerModel(t)
	m.handleWSEvent(msgEvent(a, seqUUID(100)))
	if _, ok := m.markerAnchor[a]; ok {
		t.Errorf("anchor set on active buffer arrival")
	}
}

func TestActiveArrivalAdvancesReadPosition(t *testing.T) {
	m, a, _, sent := markerModel(t)
	msgID := seqUUID(100)
	m.handleWSEvent(msgEvent(a, msgID))
	if len(*sent) != 1 {
		t.Fatalf("sent %d cmds, want 1 mark_read", len(*sent))
	}
	cmd := (*sent)[0]
	if cmd["type"] != "mark_read" || cmd["message_id"] != msgID {
		t.Errorf("cmd = %v, want mark_read for %v", cmd, msgID)
	}
}

func TestEntryMarksReadKeepsAnchor(t *testing.T) {
	m, _, b, sent := markerModel(t)
	first, second := seqUUID(100), seqUUID(101)
	m.handleWSEvent(msgEvent(b, first))
	m.handleWSEvent(msgEvent(b, second))

	m.activateBufferByID(b)
	if got := m.markerAnchor[b]; got != first {
		t.Errorf("anchor = %v after entry, want kept at %v", got, first)
	}
	if m.unread[b] != 0 || m.mentions[b] != 0 {
		t.Errorf("badges not zeroed: unread=%d mentions=%d", m.unread[b], m.mentions[b])
	}
	if len(*sent) != 1 {
		t.Fatalf("sent %d cmds, want 1", len(*sent))
	}
	cmd := (*sent)[0]
	if cmd["type"] != "mark_read" || cmd["buffer_id"] != b || cmd["message_id"] != second {
		t.Errorf("cmd = %v, want mark_read buffer=%v message=%v", cmd, b, second)
	}
}

func TestExitClearsAnchorAndReentryShowsNone(t *testing.T) {
	m, a, b, _ := markerModel(t)
	m.handleWSEvent(msgEvent(b, seqUUID(100)))
	m.activateBufferByID(b) // enter: anchor kept
	m.activateBufferByID(a) // exit b: anchor cleared
	if _, ok := m.markerAnchor[b]; ok {
		t.Errorf("anchor survived exit from buffer")
	}
}

func TestMarkReadNotResentWhenCaughtUp(t *testing.T) {
	m, a, b, sent := markerModel(t)
	m.handleWSEvent(msgEvent(b, seqUUID(100)))
	m.activateBufferByID(b)
	m.activateBufferByID(a)
	m.activateBufferByID(b) // nothing new since last mark_read
	var markReads int
	for _, c := range *sent {
		if c["type"] == "mark_read" {
			markReads++
		}
	}
	if markReads != 1 {
		t.Errorf("mark_read sent %d times, want 1", markReads)
	}
}

func TestEscClearsActiveMarker(t *testing.T) {
	m, _, b, _ := markerModel(t)
	m.handleWSEvent(msgEvent(b, seqUUID(100)))
	m.activateBufferByID(b)
	if _, ok := m.markerAnchor[b]; !ok {
		t.Fatalf("precondition: anchor missing after entry")
	}
	m.dispatchControlKey("esc")
	if _, ok := m.markerAnchor[b]; ok {
		t.Errorf("anchor survived esc")
	}
}

func TestBufferUpdateEchoKeepsActiveAnchor(t *testing.T) {
	m, _, b, _ := markerModel(t)
	msgID := seqUUID(100)
	m.handleWSEvent(msgEvent(b, msgID))
	m.activateBufferByID(b)
	// server echo of our entry mark_read: caught up, unread=0
	m.handleWSEvent(wsEvent{Type: "buffer_update", ID: b, LastSeenID: msgID, Unread: 0})
	if _, ok := m.markerAnchor[b]; !ok {
		t.Errorf("buffer_update echo cleared the active buffer's anchor")
	}
}

func TestBufferUpdateReconcilesInactiveAnchor(t *testing.T) {
	m, _, b, _ := markerModel(t)
	msgID := seqUUID(100)
	m.handleWSEvent(msgEvent(b, msgID))
	// another client marked b read; b is not active here
	m.handleWSEvent(wsEvent{Type: "buffer_update", ID: b, LastSeenID: msgID, Unread: 0})
	if _, ok := m.markerAnchor[b]; ok {
		t.Errorf("stale anchor kept on inactive caught-up buffer")
	}
}

func TestRenderBufferLinesInsertsMarker(t *testing.T) {
	buf := &bufferDTO{Kind: "channel", ShowPresenceEvents: true}
	anchor := seqUUID(2)
	msgs := []messageDTO{
		{ID: seqUUID(1), Sender: "alice", Kind: "privmsg", Content: "old"},
		{ID: anchor, Sender: "bob", Kind: "privmsg", Content: "first new"},
		{ID: seqUUID(3), Sender: "bob", Kind: "privmsg", Content: "second new"},
	}
	lines := renderBufferLines(msgs, "me", buf, anchor, 40)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (3 messages + marker)", len(lines))
	}
	if !strings.Contains(lines[1], "New messages") {
		t.Errorf("line[1] = %q, want marker line", lines[1])
	}
	// no anchor -> no marker
	lines = renderBufferLines(msgs, "me", buf, uuid.Nil, 40)
	if len(lines) != 3 {
		t.Errorf("got %d lines without anchor, want 3", len(lines))
	}
	// anchor not in slice (history trimmed) -> no marker
	lines = renderBufferLines(msgs, "me", buf, seqUUID(99), 40)
	if len(lines) != 3 {
		t.Errorf("got %d lines with unknown anchor, want 3", len(lines))
	}
}

func TestBufferUpdateEchoDoesNotStompJoined(t *testing.T) {
	m, _, b, _ := markerModel(t)
	m.findBuffer(b).Joined = true

	// mark_read echo: the JSON carries no "joined" key, which must not
	// read as "parted".
	raw := fmt.Sprintf(`{"type":"buffer_update","id":%q,"last_seen_id":%q,"unread":0}`, b, seqUUID(100))
	var ev wsEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	m.handleWSEvent(ev)
	if !m.findBuffer(b).Joined {
		t.Errorf("mark_read echo stomped Joined to false")
	}

	// An explicit joined=false (real part) still applies.
	joined := false
	m.handleWSEvent(wsEvent{Type: "buffer_update", ID: b, Joined: &joined})
	if m.findBuffer(b).Joined {
		t.Errorf("explicit joined=false not applied")
	}
}

func TestMarkReadResentAfterReconnect(t *testing.T) {
	m, a, _, sent := markerModel(t)
	m.handleWSEvent(msgEvent(a, seqUUID(100)))
	if len(*sent) != 1 {
		t.Fatalf("precondition: sent %d cmds, want 1", len(*sent))
	}

	// WS drops: the queued mark_read may never have reached the server, so
	// the reconnect-time markActiveRead must resend the read position.
	updated, _ := m.Update(wsErrorMsg{errors.New("gone")})
	*m = updated.(model)
	m.markActiveRead()
	if len(*sent) != 2 {
		t.Fatalf("mark_read not resent after reconnect: sent %d cmds", len(*sent))
	}
}

func TestActiveArrivalMarkReadThrottled(t *testing.T) {
	m, a, _, sent := markerModel(t)
	m.handleWSEvent(msgEvent(a, seqUUID(100))) // leading edge sends
	cmd := m.handleWSEvent(msgEvent(a, seqUUID(101)))
	if len(*sent) != 1 {
		t.Fatalf("sent %d cmds, want 1 (second mark_read throttled)", len(*sent))
	}
	if cmd == nil {
		t.Fatal("no trailing flush scheduled for the throttled mark_read")
	}

	updated, _ := m.Update(markReadFlushMsg{})
	*m = updated.(model)
	if len(*sent) != 2 {
		t.Fatalf("flush did not send: %d cmds", len(*sent))
	}
	if got := (*sent)[1]["message_id"]; got != seqUUID(101) {
		t.Errorf("flush marked %v, want final message %v", got, seqUUID(101))
	}
}
