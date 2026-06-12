package main

import (
	"errors"
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
