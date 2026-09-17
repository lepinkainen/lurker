package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type persistedState struct {
	LastBufferID string `json:"last_buffer_id"`
}

// stateDir overrides the directory holding tui-state.json when non-empty (set
// by the -state-dir flag). Verification runs point it at a scratch directory so
// they never clobber the user's real last-viewed buffer.
var stateDir string

func stateFilePath() (string, error) {
	if stateDir != "" {
		return filepath.Join(stateDir, "tui-state.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lurker", "tui-state.json"), nil
}

func loadPersistedBufferID() uuid.UUID {
	path, err := stateFilePath()
	if err != nil {
		return uuid.Nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return uuid.Nil
	}
	var s persistedState
	if err = json.Unmarshal(data, &s); err != nil {
		return uuid.Nil
	}
	id, err := uuid.Parse(s.LastBufferID)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func savePersistedBufferID(id uuid.UUID) error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return mkErr
	}
	data, err := json.Marshal(persistedState{LastBufferID: id.String()})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// pickStartupBuffer resolves the active buffer on startup using this order:
//  1. Persisted "last viewed" buffer if it still exists and its network is not disabled.
//  2. First pinned channel, ordered by (pin_order, channel name) — matching the
//     sidebar's Pinned section.
//  3. Per network in sort_order: active channels by (sort_order, name) →
//     queries by name → archived buffers by name → status. Status is the
//     absolute last resort.
//
// Returns uuid.Nil if no buffer is available.
func pickStartupBuffer(networks []networkDTO, buffers []bufferDTO, persisted uuid.UUID) uuid.UUID {
	if len(buffers) == 0 {
		return uuid.Nil
	}
	enabled := networkLookup(networks)

	if id := resolvePersisted(buffers, enabled, persisted); id != uuid.Nil {
		return id
	}
	if id := firstPinnedChannel(buffers, enabled); id != uuid.Nil {
		return id
	}
	return firstByNetwork(networks, buffers)
}

func networkLookup(networks []networkDTO) (enabled map[uuid.UUID]bool) {
	enabled = make(map[uuid.UUID]bool, len(networks))
	for _, n := range networks {
		enabled[n.ID] = !n.Disabled
	}
	return enabled
}

func resolvePersisted(buffers []bufferDTO, enabled map[uuid.UUID]bool, id uuid.UUID) uuid.UUID {
	if id == uuid.Nil {
		return uuid.Nil
	}
	for _, b := range buffers {
		if b.ID == id && enabled[b.NetworkID] {
			return id
		}
	}
	return uuid.Nil
}

func firstPinnedChannel(buffers []bufferDTO, enabled map[uuid.UUID]bool) uuid.UUID {
	pinned := []bufferDTO{}
	for _, b := range buffers {
		if b.Kind == "channel" && b.Pinned && enabled[b.NetworkID] {
			pinned = append(pinned, b)
		}
	}
	if len(pinned) == 0 {
		return uuid.Nil
	}
	sort.Slice(pinned, func(i, j int) bool {
		if pinned[i].PinOrder != pinned[j].PinOrder {
			return pinned[i].PinOrder < pinned[j].PinOrder
		}
		return strings.ToLower(pinned[i].Name) < strings.ToLower(pinned[j].Name)
	})
	return pinned[0].ID
}

func firstByNetwork(networks []networkDTO, buffers []bufferDTO) uuid.UUID {
	bufsByNet := make(map[uuid.UUID][]bufferDTO)
	for _, b := range buffers {
		bufsByNet[b.NetworkID] = append(bufsByNet[b.NetworkID], b)
	}
	for _, n := range networks {
		if n.Disabled {
			continue
		}
		if id := firstInGroups(bufsByNet[n.ID]); id != uuid.Nil {
			return id
		}
	}
	return uuid.Nil
}

// groupBuffers splits one network's buffers into the sidebar groups. Active
// channels sort by their manual order and then case-insensitive name; queries,
// archived buffers (any kind — the persisted archived flag, not joined state),
// and status buffers stay alphabetically sorted.
func groupBuffers(bufs []bufferDTO) (channels, queries, archived, status []bufferDTO) {
	channels = filterBufs(bufs, func(b bufferDTO) bool { return b.Kind == "channel" && !b.Archived })
	queries = filterBufs(bufs, func(b bufferDTO) bool { return b.Kind == "query" && !b.Archived })
	archived = filterBufs(bufs, func(b bufferDTO) bool { return b.Kind != "status" && b.Archived })
	status = filterBufs(bufs, func(b bufferDTO) bool { return b.Kind == "status" })
	sort.Slice(channels, func(i, j int) bool {
		if channels[i].SortOrder != channels[j].SortOrder {
			return channels[i].SortOrder < channels[j].SortOrder
		}
		return strings.ToLower(channels[i].Name) < strings.ToLower(channels[j].Name)
	})
	for _, g := range [][]bufferDTO{queries, archived, status} {
		sort.Slice(g, func(i, j int) bool { return strings.ToLower(g[i].Name) < strings.ToLower(g[j].Name) })
	}
	return channels, queries, archived, status
}

func firstInGroups(bufs []bufferDTO) uuid.UUID {
	channels, queries, archived, status := groupBuffers(bufs)
	for _, g := range [][]bufferDTO{channels, queries, archived, status} {
		if len(g) > 0 {
			return g[0].ID
		}
	}
	return uuid.Nil
}

func filterBufs(bufs []bufferDTO, pred func(bufferDTO) bool) []bufferDTO {
	out := []bufferDTO{}
	for _, b := range bufs {
		if pred(b) {
			out = append(out, b)
		}
	}
	return out
}
