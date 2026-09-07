package main

import (
	"bytes"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// applyNetworkEvent handles the network configuration broadcasts another
// client's REST mutation produced (create/update/delete/reorder).
func (m *model) applyNetworkEvent(ev wsEvent) {
	switch ev.Type {
	case "network_created", "network_updated":
		if ev.Network == nil {
			return
		}
		if n := m.findNetwork(ev.Network.ID); n != nil {
			*n = *ev.Network
		} else {
			m.networks = append(m.networks, *ev.Network)
		}
	case "network_deleted":
		var gone []uuid.UUID
		for _, b := range m.buffers {
			if b.NetworkID == ev.ID {
				gone = append(gone, b.ID)
			}
		}
		for _, id := range gone {
			m.removeBuffer(id)
		}
		m.networks = slices.DeleteFunc(m.networks, func(n networkDTO) bool { return n.ID == ev.ID })
		delete(m.networkStates, ev.ID)
		m.refreshActiveBuffer()
	case "network_reorder":
		for _, entry := range ev.Networks {
			if n := m.findNetwork(entry.ID); n != nil {
				n.SortOrder = entry.SortOrder
			}
		}
	}
	m.sortNetworks()
	m.rebuildSidebar()
}

func (m *model) handleWSEvent(ev wsEvent) {
	switch ev.Type {
	case "message":
		m.applyMessageEvent(ev)
	case "buffer_update":
		m.applyBufferUpdate(ev)
	case "buffer_settings":
		m.applyBufferSettings(ev)
	case "pinned_reorder":
		for _, entry := range ev.Buffers {
			if b := m.findBuffer(entry.ID); b != nil {
				b.PinOrder = entry.PinOrder
			}
		}
		m.rebuildSidebar()
	case "buffer_reorder":
		for _, entry := range ev.Buffers {
			if b := m.findBuffer(entry.ID); b != nil {
				b.SortOrder = entry.SortOrder
			}
		}
		m.rebuildSidebar()
	case "buffer_deleted":
		m.removeBuffer(ev.ID)
	case "network_state":
		m.networkStates[ev.NetworkID] = ev.State
	case "network_created", "network_updated", "network_deleted", "network_reorder":
		m.applyNetworkEvent(ev)
	case "error":
		m.status = "Server error: " + ev.Message
	case "buffer_created":
		if m.findBuffer(ev.ID) != nil {
			return // already known (e.g. replayed after a snapshot that has it)
		}
		// Match backend defaults (db/buffer_settings.go newBufferSettings):
		// ShowPresenceEvents=true, CollapsePresenceEvents=false. Without
		// these defaults, presenceMode would treat the freshly created
		// buffer as "hide presence" until the next /api/state reload.
		m.buffers = append(m.buffers, bufferDTO{
			ID: ev.ID, NetworkID: ev.NetworkID, Name: ev.Name, Kind: ev.Kind,
			SortOrder: ev.SortOrder, ShowPresenceEvents: true,
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
	case "history_backfill":
		m.requestBackfillRefetch(ev)
	case "channel_list":
		m.applyChannelList(ev)
	}
}

// requestBackfillRefetch reloads the recent window after a history_backfill
// event: the server inserted replayed messages (CHATHISTORY) without live
// message events, so the client refetches and applyHistoryResult merges them
// into place. Buffers never loaded just see the rows on their first load.
func (m *model) requestBackfillRefetch(ev wsEvent) {
	bufID := ev.BufferID
	if bufID == uuid.Nil || len(m.messages[bufID]) == 0 {
		return
	}
	m.sendCmdAsync(wsCmd{
		"type":      "history",
		"buffer_id": bufID,
		"limit":     min(500, ev.Count+100),
	})
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

// maxPendingEvents bounds the sync-time queue. On overflow the queue is
// discarded and the caller must start a new snapshot fetch: the
// dropped state is persisted server-side and lands in that fresh snapshot.
// History responses cannot be recovered from a snapshot; applyState releases
// their loading flags so pagination can resume after synchronization.
// ponytail: a sustained >1000-events-per-snapshot-latency flood would loop
// on resync; raise the cap if that ever happens in practice.
const maxPendingEvents = 1000

// queuePendingEvent appends ev; returns true if the queue overflowed and was
// reset, meaning the in-flight snapshot is no longer sufficient.
func (m *model) queuePendingEvent(ev wsEvent) bool {
	if len(m.pendingEvents) >= maxPendingEvents {
		m.pendingEvents = nil
		return true
	}
	m.pendingEvents = append(m.pendingEvents, ev)
	return false
}

// snapshotMessageBoundary returns, per buffer, the newest message id in the
// snapshot window. Queued message events at or below it are already
// accounted for by the snapshot (in the window, or older than it and folded
// into its unread totals) and must not be replayed.
func snapshotMessageBoundary(s *stateResponse) map[uuid.UUID]uuid.UUID {
	out := map[uuid.UUID]uuid.UUID{}
	for key, msgs := range s.InitialMessages {
		id, err := uuid.Parse(key)
		if err != nil {
			continue
		}
		var maxID uuid.UUID
		for _, msg := range msgs {
			if !uuidLTE(msg.ID, maxID) {
				maxID = msg.ID
			}
		}
		if maxID != uuid.Nil {
			out[id] = maxID
		}
	}
	return out
}

func (m *model) applyMessageEvent(ev wsEvent) {
	// Already reflected by the last snapshot (see snapshotBoundary). Live ids
	// are time-ordered and always newer than any snapshot, so this only ever
	// drops events that raced the snapshot fetch.
	if maxID, ok := m.snapshotBoundary[ev.BufferID]; ok && uuidLTE(ev.ID, maxID) {
		return
	}
	// Publications may arrive out of ID order, or after a history response
	// already loaded the row. Keep the list ordered with a logarithmic lookup.
	pos, found := slices.BinarySearchFunc(m.messages[ev.BufferID], ev.ID, func(msg messageDTO, id uuid.UUID) int {
		return bytes.Compare(msg.ID[:], id[:])
	})
	if found {
		return
	}
	parsed, _ := time.Parse(time.RFC3339Nano, ev.TS)
	msg := messageDTO{
		ID: ev.ID, NetworkID: ev.NetworkID, BufferID: ev.BufferID,
		TS: ev.TS, Sender: ev.Sender, Kind: ev.Kind, Target: ev.Target, Content: ev.Content,
		MentionsMe: ev.MentionsMe, Highlight: ev.Highlight, CountsAsUnread: ev.CountsAsUnread,
		Muted:    ev.Muted,
		IsSelf:   ev.IsSelf,
		TSParsed: parsed,
	}
	atBottom := m.viewport.AtBottom()
	m.messages[ev.BufferID] = slices.Insert(m.messages[ev.BufferID], pos, msg)

	// Unread accounting applies to every buffer, active included — there is
	// no "actively watching" suppression and no auto-ack. Self-authored
	// messages never count and never anchor; the id > LastSeenID guard keeps
	// history replays from spawning a marker.
	b := m.findBuffer(ev.BufferID)
	// Muted senders still badge mentions but never count as unread or anchor
	// the marker (same rule as the server's tallyUnread).
	if b != nil && ev.CountsAsUnread && !ev.IsSelf && !uuidLTE(ev.ID, b.LastSeenID) {
		if ev.MentionsMe || ev.Highlight {
			m.mentions[ev.BufferID]++
		}
		if !ev.Muted {
			m.unread[ev.BufferID]++
			if b.MarkerID == uuid.Nil || !uuidLTE(b.MarkerID, ev.ID) {
				b.MarkerID = ev.ID
				b.MarkerTS = ev.TS
			}
		}
	}
	if m.activeBuffer != nil && ev.BufferID == m.activeBuffer.ID {
		m.refreshViewport()
		if atBottom {
			m.viewport.GotoBottom()
		}
	}
}

func (m *model) applyBufferUpdate(ev wsEvent) {
	bufID := ev.ID // backend uses "id" for buffer_update, not "buffer_id"
	b := m.findBuffer(bufID)
	if b == nil {
		return
	}
	if ev.Topic != nil {
		b.Topic = *ev.Topic
	}
	if ev.TopicSetBy != nil {
		b.TopicSetBy = *ev.TopicSetBy
	}
	if ev.Joined != nil {
		b.Joined = *ev.Joined
	}
	if ev.Archived != nil {
		b.Archived = *ev.Archived
		m.rebuildSidebar()
	}
	// mark_read variant (discriminated by last_seen_id): counts and marker
	// are taken verbatim — a nil MarkerID means caught up and clears the
	// marker even on the active buffer (a remote ack dismisses everywhere).
	// Equal-position echoes are stale unless a local optimistic ack still
	// needs its authoritative residual counts and marker. Older positions
	// must never roll back a newer acknowledgement.
	if ev.LastSeenID != uuid.Nil && (!uuidLTE(ev.LastSeenID, b.LastSeenID) ||
		(m.optimisticRead[bufID] && ev.LastSeenID == b.LastSeenID)) {
		delete(m.optimisticRead, bufID)
		b.LastSeenID = ev.LastSeenID
		m.unread[bufID] = ev.Unread
		m.mentions[bufID] = ev.Mentions
		if ev.MarkerID != nil {
			b.MarkerID = *ev.MarkerID
		} else {
			b.MarkerID = uuid.Nil
		}
		if ev.MarkerTS != nil {
			b.MarkerTS = *ev.MarkerTS
		} else {
			b.MarkerTS = ""
		}
		if m.activeBuffer != nil && m.activeBuffer.ID == bufID {
			m.refreshViewport()
		}
	}
}

func (m *model) applyBufferSettings(ev wsEvent) {
	if b := m.findBuffer(ev.ID); b != nil {
		b.ShowPresenceEvents = ev.ShowPresenceEvents
		b.CollapsePresenceEvents = ev.CollapsePresenceEvents
		needsRebuild := false
		if b.Pinned != ev.Pinned || b.PinOrder != ev.PinOrder {
			b.Pinned = ev.Pinned
			b.PinOrder = ev.PinOrder
			needsRebuild = true
		}
		if ev.Archived != nil && b.Archived != *ev.Archived {
			b.Archived = *ev.Archived
			needsRebuild = true
		}
		if needsRebuild {
			m.rebuildSidebar()
		}
		if m.activeBuffer != nil && m.activeBuffer.ID == ev.ID {
			m.activeBuffer = b
			m.refreshViewport()
		}
	}
}

// removeBuffer handles a buffer_deleted broadcast: drop the buffer and all
// its per-buffer state; if it was active, fall back like at startup.
func (m *model) removeBuffer(id uuid.UUID) {
	idx := -1
	for i := range m.buffers {
		if m.buffers[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	wasActive := m.activeBuffer != nil && m.activeBuffer.ID == id
	m.buffers = append(m.buffers[:idx], m.buffers[idx+1:]...)
	// The slice just shrank; any pointer into it is stale.
	m.refreshActiveBuffer()
	delete(m.messages, id)
	delete(m.members, id)
	delete(m.unread, id)
	delete(m.mentions, id)
	delete(m.optimisticRead, id)
	delete(m.historyLoading, id)
	delete(m.historyExhaust, id)
	if wasActive {
		m.activeBuffer = nil
		if next := pickStartupBuffer(m.networks, m.buffers, uuid.Nil); next != uuid.Nil {
			m.activeBuffer = m.findBuffer(next)
		}
	}
	m.rebuildSidebar()
	m.refreshViewport()
}

func (m *model) applyHistoryResult(ev wsEvent) {
	bufID := ev.BufferID
	wasLoading := m.historyLoading[bufID]
	m.historyLoading[bufID] = false
	if len(ev.Messages) == 0 {
		// "No older history" is only provable for a scroll-up request (which
		// set historyLoading); a backfill refetch says nothing about the top.
		if wasLoading {
			m.historyExhaust[bufID] = true
		}
		return
	}
	existing := m.messages[bufID]
	known := make(map[uuid.UUID]struct{}, len(existing))
	for i := range existing {
		known[existing[i].ID] = struct{}{}
	}
	fresh := make([]messageDTO, 0, len(ev.Messages))
	for i := range ev.Messages {
		if _, ok := known[ev.Messages[i].ID]; ok {
			continue
		}
		ev.Messages[i].TSParsed, _ = time.Parse(time.RFC3339Nano, ev.Messages[i].TS)
		fresh = append(fresh, ev.Messages[i])
	}
	if len(fresh) == 0 {
		return
	}
	// Merge-sort by id rather than prepend: scroll-up pages are strictly
	// older than everything known (sort is a no-op), but a history_backfill
	// refetch delivers rows that belong *between* existing messages (the
	// disconnect gap) and must slot into place.
	combined := slices.Concat(fresh, existing)
	slices.SortFunc(combined, func(a, b messageDTO) int {
		return bytes.Compare(a.ID[:], b.ID[:])
	})
	m.messages[bufID] = combined
	m.countHistoryUnread(bufID, fresh)
	if m.activeBuffer != nil && m.activeBuffer.ID == bufID {
		m.refreshViewport()
	}
}

// countHistoryUnread applies unread/marker bookkeeping to freshly merged
// history rows. Backfilled gap messages are unread (their ids sort after
// last_seen), mirroring applyMessageEvent; scroll-up pages are older than
// last_seen and never match.
func (m *model) countHistoryUnread(bufID uuid.UUID, fresh []messageDTO) {
	b := m.findBuffer(bufID)
	if b == nil {
		return
	}
	for _, msg := range fresh {
		if !msg.CountsAsUnread || msg.IsSelf || uuidLTE(msg.ID, b.LastSeenID) {
			continue
		}
		m.unread[bufID]++
		if msg.MentionsMe || msg.Highlight {
			m.mentions[bufID]++
		}
		if b.MarkerID == uuid.Nil || bytes.Compare(msg.ID[:], b.MarkerID[:]) < 0 {
			b.MarkerID = msg.ID
			b.MarkerTS = msg.TS
		}
	}
}
