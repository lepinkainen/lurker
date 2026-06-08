package main

import (
	"errors"
	"testing"

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
