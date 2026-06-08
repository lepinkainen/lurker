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

func stateFilePath() (string, error) {
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
//  2. First pinned channel, ordered by (network.sort_order, channel name).
//  3. Per network in sort_order: joined channels by name → queries by name →
//     parted channels by name → status. Status is the absolute last resort.
//
// Returns uuid.Nil if no buffer is available.
func pickStartupBuffer(networks []networkDTO, buffers []bufferDTO, persisted uuid.UUID) uuid.UUID {
	if len(buffers) == 0 {
		return uuid.Nil
	}
	enabled, netOrder := networkLookup(networks)

	if id := resolvePersisted(buffers, enabled, persisted); id != uuid.Nil {
		return id
	}
	if id := firstPinnedChannel(buffers, enabled, netOrder); id != uuid.Nil {
		return id
	}
	return firstByNetwork(networks, buffers)
}

func networkLookup(networks []networkDTO) (enabled map[uuid.UUID]bool, order map[uuid.UUID]int) {
	enabled = make(map[uuid.UUID]bool, len(networks))
	order = make(map[uuid.UUID]int, len(networks))
	for i, n := range networks {
		enabled[n.ID] = !n.Disabled
		order[n.ID] = i
	}
	return enabled, order
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

func firstPinnedChannel(buffers []bufferDTO, enabled map[uuid.UUID]bool, netOrder map[uuid.UUID]int) uuid.UUID {
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
		ni, nj := netOrder[pinned[i].NetworkID], netOrder[pinned[j].NetworkID]
		if ni != nj {
			return ni < nj
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

func firstInGroups(bufs []bufferDTO) uuid.UUID {
	groups := [][]bufferDTO{
		filterBufs(bufs, func(b bufferDTO) bool { return b.Kind == "channel" && b.Joined }),
		filterBufs(bufs, func(b bufferDTO) bool { return b.Kind == "query" }),
		filterBufs(bufs, func(b bufferDTO) bool { return b.Kind == "channel" && !b.Joined }),
		filterBufs(bufs, func(b bufferDTO) bool { return b.Kind == "status" }),
	}
	byName := func(a, b bufferDTO) bool { return strings.ToLower(a.Name) < strings.ToLower(b.Name) }
	for _, g := range groups {
		sort.Slice(g, func(i, j int) bool { return byName(g[i], g[j]) })
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
