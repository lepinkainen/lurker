package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func archiveTestFixture() (*model, uuid.UUID, map[string]uuid.UUID) {
	m := testModel()
	netID := uuid.New()
	ids := map[string]uuid.UUID{
		"status": uuid.New(), "#beta": uuid.New(), "#alpha": uuid.New(),
		"zoe": uuid.New(), "#old": uuid.New(), "spammer": uuid.New(),
	}
	m.networks = []networkDTO{{ID: netID, Name: "net"}}
	m.buffers = []bufferDTO{
		{ID: ids["status"], NetworkID: netID, Name: "net", Kind: "status"},
		{ID: ids["#beta"], NetworkID: netID, Name: "#Beta", Kind: "channel", Joined: true},
		{ID: ids["#alpha"], NetworkID: netID, Name: "#alpha", Kind: "channel", Joined: true},
		{ID: ids["zoe"], NetworkID: netID, Name: "zoe", Kind: "query"},
		{ID: ids["#old"], NetworkID: netID, Name: "#old", Kind: "channel", Archived: true},
		{ID: ids["spammer"], NetworkID: netID, Name: "spammer", Kind: "query", Archived: true},
	}
	return m, netID, ids
}

func sidebarLabels(m *model) []string {
	labels := make([]string, 0, len(m.sidebarItems))
	for _, it := range m.sidebarItems {
		labels = append(labels, it.label)
	}
	return labels
}

// The sidebar groups per network: status, active channels (name-sorted,
// case-insensitive), queries, then a folded Archives toggle.
func TestRebuildSidebarGroupsAndSorts(t *testing.T) {
	m, _, _ := archiveTestFixture()
	m.rebuildSidebar()

	want := []string{"net", "(status)", "#alpha", "#Beta", "zoe", "▸ Archives (2)"}
	got := sidebarLabels(m)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sidebar = %v, want %v", got, want)
	}
}

func TestArchivesToggleShowsDimmedRowsAndKeepsSelection(t *testing.T) {
	m, netID, ids := archiveTestFixture()
	m.rebuildSidebar()

	// Select the toggle row as a mouse click would, then activate.
	for i, it := range m.sidebarItems {
		if it.isArchiveToggle {
			m.sidebarSel = i
		}
	}
	m.activateSidebarSel()

	want := []string{"net", "(status)", "#alpha", "#Beta", "zoe", "▾ Archives (2)", "#old", "spammer"}
	got := sidebarLabels(m)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("open sidebar = %v, want %v", got, want)
	}
	if !m.sidebarItems[m.sidebarSel].isArchiveToggle {
		t.Fatalf("selection left the toggle row: idx=%d", m.sidebarSel)
	}
	for _, it := range m.sidebarItems {
		if (it.bufferID == ids["#old"] || it.bufferID == ids["spammer"]) && !it.dim {
			t.Fatalf("archived row %q not dimmed", it.label)
		}
	}

	// Toggling again folds it back.
	m.activateSidebarSel()
	if m.archivesOpen[netID] {
		t.Fatal("second activation did not close the fold")
	}
	if len(sidebarLabels(m)) != 6 {
		t.Fatalf("folded sidebar = %v", sidebarLabels(m))
	}
}

func TestMoveSidebarSkipsArchiveToggle(t *testing.T) {
	m, netID, _ := archiveTestFixture()
	m.rebuildSidebar()
	// Start on "zoe" (last row before the toggle) and move down: the toggle
	// must be skipped (it would otherwise auto-toggle) and selection wraps.
	for i, it := range m.sidebarItems {
		if it.label == "zoe" {
			m.sidebarSel = i
		}
	}
	m.moveSidebar(1)
	if m.sidebarItems[m.sidebarSel].isArchiveToggle {
		t.Fatal("moveSidebar landed on the Archives toggle")
	}
	if m.archivesOpen[netID] {
		t.Fatal("moveSidebar toggled the fold")
	}
}

func TestBufferDeletedRemovesStateAndReselects(t *testing.T) {
	m, _, ids := archiveTestFixture()
	m.rebuildSidebar()
	doomed := ids["spammer"]
	m.messages[doomed] = []messageDTO{{ID: uuid.New(), BufferID: doomed}}
	m.unread[doomed] = 3
	m.activeBuffer = m.findBuffer(doomed)

	m.handleWSEvent(wsEvent{Type: "buffer_deleted", ID: doomed})

	if m.findBuffer(doomed) != nil {
		t.Fatal("buffer still present after buffer_deleted")
	}
	if _, ok := m.messages[doomed]; ok {
		t.Fatal("messages not dropped")
	}
	if _, ok := m.unread[doomed]; ok {
		t.Fatal("unread not dropped")
	}
	if m.activeBuffer == nil || m.activeBuffer.ID == doomed {
		t.Fatalf("active buffer not reselected: %+v", m.activeBuffer)
	}
	for _, it := range m.sidebarItems {
		if it.bufferID == doomed {
			t.Fatal("deleted buffer still in sidebar")
		}
	}
}

func TestBufferUpdateArchivedMovesBufferIntoFold(t *testing.T) {
	m, _, ids := archiveTestFixture()
	m.rebuildSidebar()
	archived := true
	m.applyBufferUpdate(wsEvent{Type: "buffer_update", ID: ids["#alpha"], Archived: &archived})

	got := sidebarLabels(m)
	want := []string{"net", "(status)", "#Beta", "zoe", "▸ Archives (3)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sidebar = %v, want %v", got, want)
	}
}

func TestSlashDeleteGuards(t *testing.T) {
	joined := &bufferDTO{ID: uuid.New(), NetworkID: uuid.New(), Name: "#chan", Kind: "channel", Joined: true}
	if _, ok := parseSlash("/delete", joined); ok {
		t.Fatal("/delete allowed on non-archived channel")
	}
	status := &bufferDTO{ID: uuid.New(), Name: "net", Kind: "status", Archived: true}
	if _, ok := parseSlash("/delete", status); ok {
		t.Fatal("/delete allowed on status buffer")
	}
	arch := &bufferDTO{ID: uuid.New(), Name: "#old", Kind: "channel", Archived: true}
	cmd, ok := parseSlash("/delete", arch)
	if !ok || cmd["type"] != "delete_buffer" || cmd["buffer_id"] != arch.ID {
		t.Fatalf("/delete on archived channel = %v ok=%v", cmd, ok)
	}
	query := &bufferDTO{ID: uuid.New(), Name: "bob", Kind: "query"}
	if cmd, ok := parseSlash("/archive", query); !ok || cmd["type"] != "archive_buffer" {
		t.Fatalf("/archive = %v ok=%v", cmd, ok)
	}
	if cmd, ok := parseSlash("/unarchive", query); !ok || cmd["type"] != "unarchive_buffer" {
		t.Fatalf("/unarchive = %v ok=%v", cmd, ok)
	}
}
