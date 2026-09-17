package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

func (m *model) pinnedSidebarItems() []sidebarItem {
	enabled := make(map[uuid.UUID]bool, len(m.networks))
	for _, n := range m.networks {
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
		if pinned[i].PinOrder != pinned[j].PinOrder {
			return pinned[i].PinOrder < pinned[j].PinOrder
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

// rebuildSidebar regenerates the row list from networks/buffers. The
// selection follows the row's identity (buffer id, or network id for a
// header), not its index, so a reorder or insert elsewhere doesn't move the
// highlight onto a different row.
func (m *model) rebuildSidebar() {
	var selBuf, selNet uuid.UUID
	if m.sidebarSel >= 0 && m.sidebarSel < len(m.sidebarItems) {
		sel := m.sidebarItems[m.sidebarSel]
		selBuf, selNet = sel.bufferID, sel.networkID
	}
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
		items = append(items, m.networkSidebarItems(n.ID, bufsByNet[n.ID])...)
	}
	m.sidebarItems = items
	for i, item := range items {
		if (selBuf != uuid.Nil && item.bufferID == selBuf) ||
			(selBuf == uuid.Nil && item.isHeader && selNet != uuid.Nil && item.networkID == selNet) {
			m.sidebarSel = i
			return
		}
	}
	if m.sidebarSel >= len(items) {
		m.sidebarSel = 0
	}
}

// networkSidebarItems renders one network's rows: status, active channels,
// queries, then the folded Archives section.
func (m *model) networkSidebarItems(netID uuid.UUID, bufs []bufferDTO) []sidebarItem {
	channels, queries, archived, status := groupBuffers(bufs)
	items := []sidebarItem{}
	for _, b := range status {
		items = append(items, sidebarItem{label: "(status)", bufferID: b.ID, networkID: netID})
	}
	for _, b := range append(channels, queries...) {
		items = append(items, sidebarItem{label: b.Name, bufferID: b.ID, networkID: netID})
	}
	if len(archived) == 0 {
		return items
	}
	caret := "▸"
	if m.archivesOpen[netID] {
		caret = "▾"
	}
	items = append(items, sidebarItem{
		label:           fmt.Sprintf("%s Archives (%d)", caret, len(archived)),
		networkID:       netID,
		isArchiveToggle: true,
	})
	if m.archivesOpen[netID] {
		for _, b := range archived {
			items = append(items, sidebarItem{label: b.Name, bufferID: b.ID, networkID: netID, dim: true})
		}
	}
	return items
}

func (m *model) moveSidebar(delta int) {
	n := len(m.sidebarItems)
	if n == 0 {
		return
	}
	idx := m.sidebarSel + delta
	for range n {
		idx = (idx + n) % n
		// Skip headers and the Archives fold row: moveSidebar auto-activates
		// its landing spot, and scrolling past must not toggle the fold.
		// Keyboard fold access is the /archives slash command.
		if !m.sidebarItems[idx].isHeader && !m.sidebarItems[idx].isArchiveToggle {
			break
		}
		idx += delta
	}
	m.sidebarSel = (idx + n) % n
	m.activateSidebarSel()
}

// toggleArchives flips a network's Archives fold and keeps the selection on
// the fold row so a click/enter toggles in place.
func (m *model) toggleArchives(networkID uuid.UUID) {
	if m.archivesOpen == nil {
		m.archivesOpen = make(map[uuid.UUID]bool)
	}
	m.archivesOpen[networkID] = !m.archivesOpen[networkID]
	m.rebuildSidebar()
	for i, item := range m.sidebarItems {
		if item.isArchiveToggle && item.networkID == networkID {
			m.sidebarSel = i
			break
		}
	}
}

func (m *model) activateSidebarSel() {
	if m.sidebarSel >= len(m.sidebarItems) {
		return
	}
	item := m.sidebarItems[m.sidebarSel]
	if item.isHeader {
		return
	}
	if item.isArchiveToggle {
		m.toggleArchives(item.networkID)
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
