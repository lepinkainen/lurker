package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// Minimal model for tests: skip textarea/viewport state, populate only
// what each test exercises.
func testModel() *model {
	m := &model{
		networkStates:  map[uuid.UUID]string{},
		messages:       map[uuid.UUID][]messageDTO{},
		members:        map[uuid.UUID][]channelMember{},
		unread:         map[uuid.UUID]int{},
		mentions:       map[uuid.UUID]int{},
		historyLoading: map[uuid.UUID]bool{},
		historyExhaust: map[uuid.UUID]bool{},
		channelList:    map[uuid.UUID][]channelListEntry{},
		archivesOpen:   map[uuid.UUID]bool{},
	}
	return m
}

// A cleared topic arrives as an explicit empty string on the wire and must
// unlearn both the topic text and its setter attribution in the header.
func TestBufferUpdateTopicClearUnlearnsSetter(t *testing.T) {
	m := testModel()
	bufID := uuid.New()
	m.buffers = []bufferDTO{{ID: bufID, Name: "#test", Kind: "channel"}}
	m.activeBuffer = &m.buffers[0]
	m.width = 120

	topic, setBy := "hello world", "alice"
	m.applyBufferUpdate(wsEvent{Type: "buffer_update", ID: bufID, Topic: &topic, TopicSetBy: &setBy})
	header := m.renderHeader(120)
	if !strings.Contains(header, "hello world") || !strings.Contains(header, "set by alice") {
		t.Fatalf("header = %q, want topic and setter", header)
	}

	cleared := ""
	m.applyBufferUpdate(wsEvent{Type: "buffer_update", ID: bufID, Topic: &cleared, TopicSetBy: &cleared})
	header = m.renderHeader(120)
	if strings.Contains(header, "hello world") || strings.Contains(header, "alice") {
		t.Fatalf("header = %q, want no stale topic or setter after clear", header)
	}
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
		SortOrder: 7,
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
	if b.SortOrder != 7 {
		t.Errorf("SortOrder=%d on newly created buffer; want 7", b.SortOrder)
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
	m.activateBufferByID(a) // entry sends nothing — acks are explicit only
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

func TestMarkerSetOnArrivalAndSticky(t *testing.T) {
	m, _, b, sent := markerModel(t)
	first, second := seqUUID(100), seqUUID(101)
	m.handleWSEvent(msgEvent(b, first))
	if got := m.findBuffer(b).MarkerID; got != first {
		t.Fatalf("marker = %v, want %v", got, first)
	}
	m.handleWSEvent(msgEvent(b, second))
	if got := m.findBuffer(b).MarkerID; got != first {
		t.Errorf("marker moved to %v after second arrival, want sticky %v", got, first)
	}
	if m.unread[b] != 2 {
		t.Errorf("unread = %d, want 2", m.unread[b])
	}
	if len(*sent) != 0 {
		t.Errorf("arrival sent %d cmds, want 0 (no implicit mark_read)", len(*sent))
	}
}

// Arrival in the ACTIVE buffer counts and anchors exactly like any other
// buffer — there is no "actively watching" suppression and no auto-ack.
func TestActiveArrivalCountsAndAnchorsNoAck(t *testing.T) {
	m, a, _, sent := markerModel(t)
	msgID := seqUUID(100)
	m.handleWSEvent(msgEvent(a, msgID))
	if got := m.findBuffer(a).MarkerID; got != msgID {
		t.Errorf("marker = %v on active arrival, want %v", got, msgID)
	}
	if m.unread[a] != 1 {
		t.Errorf("unread = %d, want 1", m.unread[a])
	}
	if len(*sent) != 0 {
		t.Errorf("active arrival sent %d cmds, want 0", len(*sent))
	}
}

// Self-authored messages never count toward unread and never anchor.
func TestSelfMessageDoesNotCountOrAnchor(t *testing.T) {
	m, a, _, _ := markerModel(t)
	ev := msgEvent(a, seqUUID(100))
	ev.IsSelf = true
	m.handleWSEvent(ev)
	if m.unread[a] != 0 {
		t.Errorf("unread = %d for self message, want 0", m.unread[a])
	}
	if got := m.findBuffer(a).MarkerID; got != uuid.Nil {
		t.Errorf("marker = %v for self message, want none", got)
	}
}

// Replayed messages at or below the read position never spawn a marker.
func TestHistoryReplayDoesNotSpawnMarker(t *testing.T) {
	m, _, b, _ := markerModel(t)
	m.findBuffer(b).LastSeenID = seqUUID(200)
	m.handleWSEvent(msgEvent(b, seqUUID(150)))
	if m.unread[b] != 0 {
		t.Errorf("unread = %d for replayed message, want 0", m.unread[b])
	}
	if got := m.findBuffer(b).MarkerID; got != uuid.Nil {
		t.Errorf("marker = %v for replayed message, want none", got)
	}
}

// Entering a buffer is not an ack: no mark_read goes out and the marker,
// badges and counts all survive entry, exit and re-entry.
func TestEntryAndExitDoNotAck(t *testing.T) {
	m, a, b, sent := markerModel(t)
	first := seqUUID(100)
	m.handleWSEvent(msgEvent(b, first))
	m.activateBufferByID(b) // enter
	m.activateBufferByID(a) // exit
	m.activateBufferByID(b) // re-enter
	if len(*sent) != 0 {
		t.Fatalf("buffer switching sent %d cmds, want 0", len(*sent))
	}
	if got := m.findBuffer(b).MarkerID; got != first {
		t.Errorf("marker = %v after entry/exit, want kept at %v", got, first)
	}
	if m.unread[b] != 1 {
		t.Errorf("unread = %d after entry/exit, want 1", m.unread[b])
	}
}

// Esc acks: mark_read for the newest loaded message goes out and the
// marker + badges clear together, optimistically.
func TestEscAcksActiveBuffer(t *testing.T) {
	m, _, b, sent := markerModel(t)
	first, second := seqUUID(100), seqUUID(101)
	m.handleWSEvent(msgEvent(b, first))
	m.handleWSEvent(msgEvent(b, second))
	m.activateBufferByID(b)

	m.dispatchControlKey("esc")
	if len(*sent) != 1 {
		t.Fatalf("sent %d cmds, want 1 mark_read", len(*sent))
	}
	cmd := (*sent)[0]
	if cmd["type"] != "mark_read" || cmd["buffer_id"] != b || cmd["message_id"] != second {
		t.Errorf("cmd = %v, want mark_read buffer=%v message=%v", cmd, b, second)
	}
	buf := m.findBuffer(b)
	if buf.MarkerID != uuid.Nil || buf.MarkerTS != "" {
		t.Errorf("marker not cleared: id=%v ts=%q", buf.MarkerID, buf.MarkerTS)
	}
	if buf.LastSeenID != second {
		t.Errorf("LastSeenID = %v, want optimistic %v", buf.LastSeenID, second)
	}
	if m.unread[b] != 0 || m.mentions[b] != 0 {
		t.Errorf("badges not zeroed: unread=%d mentions=%d", m.unread[b], m.mentions[b])
	}
	// Esc keeps its focus-toggle behavior.
	if m.focus != focusSidebar {
		t.Errorf("focus = %v after esc, want focusSidebar", m.focus)
	}
}

// A second Esc when caught up sends another (harmless, idempotent)
// mark_read only if there is something loaded; an empty buffer never acks.
func TestAckEmptyBufferIsNoop(t *testing.T) {
	m, _, _, sent := markerModel(t)
	m.ackActiveRead() // active buffer a has no messages
	if len(*sent) != 0 {
		t.Errorf("empty-buffer ack sent %d cmds, want 0", len(*sent))
	}
}

// When the send cannot go out, the ack is a complete no-op: optimistic
// state must not diverge from the server (buffer_update/resync restores).
func TestAckWithoutConnectionLeavesStateAlone(t *testing.T) {
	m, _, b, _ := markerModel(t)
	first := seqUUID(100)
	m.handleWSEvent(msgEvent(b, first))
	m.activateBufferByID(b)
	m.sendWS = func(wsCmd) error { return errors.New("no conn") }

	m.ackActiveRead()
	buf := m.findBuffer(b)
	if buf.MarkerID != first {
		t.Errorf("marker = %v after failed ack, want %v", buf.MarkerID, first)
	}
	if m.unread[b] != 1 {
		t.Errorf("unread = %d after failed ack, want 1", m.unread[b])
	}
}

// mark_read variant of buffer_update (last_seen_id set) takes marker fields
// verbatim: JSON null marker_id clears the marker even on the ACTIVE buffer
// (a remote ack dismisses everywhere).
func TestBufferUpdateNullMarkerClearsActiveBuffer(t *testing.T) {
	m, a, _, _ := markerModel(t)
	msgID := seqUUID(100)
	m.handleWSEvent(msgEvent(a, msgID))
	if m.findBuffer(a).MarkerID != msgID {
		t.Fatal("precondition: marker not set")
	}

	raw := fmt.Sprintf(`{"type":"buffer_update","id":%q,"last_seen_id":%q,"marker_id":null,"unread":0,"mentions":0}`, a, msgID)
	var ev wsEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	m.handleWSEvent(ev)
	buf := m.findBuffer(a)
	if buf.MarkerID != uuid.Nil || buf.MarkerTS != "" {
		t.Errorf("marker not cleared on null marker_id: id=%v ts=%q", buf.MarkerID, buf.MarkerTS)
	}
	if buf.LastSeenID != msgID {
		t.Errorf("LastSeenID = %v, want %v", buf.LastSeenID, msgID)
	}
	if m.unread[a] != 0 {
		t.Errorf("unread = %d, want 0", m.unread[a])
	}
}

// A buffer_update carrying a marker applies it verbatim (e.g. a remote
// partial catch-up re-derives the marker at a newer message).
func TestBufferUpdateSetsMarkerVerbatim(t *testing.T) {
	m, _, b, _ := markerModel(t)
	seen, marker := seqUUID(100), seqUUID(101)
	raw := fmt.Sprintf(`{"type":"buffer_update","id":%q,"last_seen_id":%q,"marker_id":%q,"marker_ts":"2026-07-28T10:00:00Z","unread":3,"mentions":1}`, b, seen, marker)
	var ev wsEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	m.handleWSEvent(ev)
	buf := m.findBuffer(b)
	if buf.MarkerID != marker || buf.MarkerTS != "2026-07-28T10:00:00Z" {
		t.Errorf("marker = (%v, %q), want (%v, 2026-07-28T10:00:00Z)", buf.MarkerID, buf.MarkerTS, marker)
	}
	if m.unread[b] != 3 || m.mentions[b] != 1 {
		t.Errorf("counts = (%d, %d), want (3, 1)", m.unread[b], m.mentions[b])
	}
}

// /api/state hydrates markers: they exist at startup without any client
// bookkeeping (reload brings badge, divider and bar back identically).
func TestApplyStateHydratesMarker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	m := testModel()
	netID, bufID, marker := uuid.New(), seqUUID(1), seqUUID(50)
	m.applyState(&stateResponse{
		Networks: []networkDTO{{ID: netID, Name: "ircnet"}},
		Buffers: []bufferDTO{{
			ID: bufID, NetworkID: netID, Name: "#a", Kind: "channel",
			MarkerID: marker, MarkerTS: "2026-07-28T10:00:00Z",
			Unread: 5, Mentions: 2,
		}},
	})
	buf := m.findBuffer(bufID)
	if buf.MarkerID != marker || buf.MarkerTS != "2026-07-28T10:00:00Z" {
		t.Errorf("marker = (%v, %q) after applyState", buf.MarkerID, buf.MarkerTS)
	}
	if m.unread[bufID] != 5 || m.mentions[bufID] != 2 {
		t.Errorf("counts = (%d, %d), want (5, 2)", m.unread[bufID], m.mentions[bufID])
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

// ── unread bar ──

func TestUnreadBarVisibility(t *testing.T) {
	m, a, _, _ := markerModel(t)
	if m.unreadBarVisible() {
		t.Errorf("bar visible with no marker")
	}
	// Marker set but no loaded messages → hidden (nothing renderable/ackable).
	m.findBuffer(a).MarkerID = seqUUID(100)
	if m.unreadBarVisible() {
		t.Errorf("bar visible with empty buffer")
	}
	m.handleWSEvent(msgEvent(a, seqUUID(100)))
	if !m.unreadBarVisible() {
		t.Errorf("bar hidden with marker + loaded messages")
	}
}

// A server that predates marker_id reports unread counts but never a marker.
// The bar must still show so the badge stays clearable (version-skew fallback).
func TestUnreadBarVisibleWithoutMarkerWhenUnread(t *testing.T) {
	m, a, _, _ := markerModel(t)
	m.handleWSEvent(msgEvent(a, seqUUID(100)))
	m.findBuffer(a).MarkerID = uuid.Nil
	m.unread[a] = 3
	if !m.unreadBarVisible() {
		t.Errorf("bar hidden with unread > 0 and no marker (old-server fallback)")
	}
	m.unread[a] = 0
	if m.unreadBarVisible() {
		t.Errorf("bar visible with no marker and no unread")
	}
}

func TestUnreadBarShowsCount(t *testing.T) {
	m, a, _, _ := markerModel(t)
	m.handleWSEvent(msgEvent(a, seqUUID(100)))
	bar := m.renderUnreadBar(60)
	if !strings.Contains(bar, "1 new message") || strings.Contains(bar, "messages") {
		t.Errorf("bar = %q, want singular \"1 new message\"", bar)
	}
	m.handleWSEvent(msgEvent(a, seqUUID(101)))
	m.handleWSEvent(msgEvent(a, seqUUID(102)))
	if bar = m.renderUnreadBar(60); !strings.Contains(bar, "3 new messages") {
		t.Errorf("bar = %q, want \"3 new messages\"", bar)
	}
}

// Count is unreliable when the marker message is outside loaded history or
// the server cap (1000) is hit → fall back to "new since <t>".
func TestUnreadBarFallsBackToNewSince(t *testing.T) {
	m, a, _, _ := markerModel(t)
	m.handleWSEvent(msgEvent(a, seqUUID(100)))

	// Marker outside loaded history.
	buf := m.findBuffer(a)
	buf.MarkerID = seqUUID(50) // not in m.messages[a]
	buf.MarkerTS = "2026-01-02T15:04:05Z"
	if bar := m.renderUnreadBar(60); !strings.Contains(bar, "new since") {
		t.Errorf("bar = %q, want \"new since\" for unloaded marker", bar)
	}

	// At the server cap.
	buf.MarkerID = seqUUID(100) // loaded again
	m.unread[a] = 1000
	if bar := m.renderUnreadBar(60); !strings.Contains(bar, "new since") {
		t.Errorf("bar = %q, want \"new since\" at count cap", bar)
	}
}

func TestFormatMarkerTime(t *testing.T) {
	// Pin to local noon so the today/yesterday boundaries can't flake
	// around midnight.
	n := time.Now()
	now := time.Date(n.Year(), n.Month(), n.Day(), 12, 0, 0, 0, time.Local)
	today := now.Add(-time.Minute)
	if got := formatMarkerTime(today.Format(time.RFC3339)); got != today.Local().Format("15:04") {
		t.Errorf("today: got %q, want %q", got, today.Local().Format("15:04"))
	}
	yesterday := now.AddDate(0, 0, -1)
	want := "yesterday " + yesterday.Local().Format("15:04")
	if got := formatMarkerTime(yesterday.Format(time.RFC3339)); got != want {
		t.Errorf("yesterday: got %q, want %q", got, want)
	}
	older := now.AddDate(0, 0, -10)
	if got := formatMarkerTime(older.Format(time.RFC3339)); got != older.Local().Format("Jan 2 15:04") {
		t.Errorf("older: got %q, want %q", got, older.Local().Format("Jan 2 15:04"))
	}
	if got := formatMarkerTime("garbage"); got != "??:??" {
		t.Errorf("unparseable: got %q", got)
	}
}

// Left-click on the unread bar row (pinned between header and viewport)
// acks the active buffer.
func TestMouseClickUnreadBarAcks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	m := newModel(&Config{})
	netID, bufID := uuid.New(), seqUUID(1)
	m.networks = []networkDTO{{ID: netID, Name: "ircnet"}}
	m.buffers = []bufferDTO{{ID: bufID, NetworkID: netID, Name: "#a", Kind: "channel"}}
	m.rebuildSidebar()
	var sent []wsCmd
	m.sendWS = func(cmd wsCmd) error {
		sent = append(sent, cmd)
		return nil
	}
	mp := &m
	mp.activateBufferByID(bufID)
	msgID := seqUUID(100)
	mp.handleWSEvent(msgEvent(bufID, msgID))
	if !mp.unreadBarVisible() {
		t.Fatal("precondition: bar not visible")
	}

	updated, _ := m.Update(tea.MouseMsg{
		X: sidebarWidth + 5, Y: headerHeight,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	mm := updated.(model)
	if len(sent) != 1 || sent[0]["type"] != "mark_read" || sent[0]["message_id"] != msgID {
		t.Fatalf("sent = %v, want one mark_read for %v", sent, msgID)
	}
	if mm.findBuffer(bufID).MarkerID != uuid.Nil {
		t.Errorf("marker not cleared by bar click")
	}
	if mm.unread[bufID] != 0 {
		t.Errorf("unread = %d after bar click, want 0", mm.unread[bufID])
	}
}
