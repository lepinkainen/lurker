package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
)

type reorderBuffersRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

type bufferSortEntryDTO struct {
	ID        uuid.UUID `json:"id"`
	SortOrder int64     `json:"sort_order"`
}

type bufferReorderEvent struct {
	Type      string               `json:"type"`
	NetworkID uuid.UUID            `json:"network_id"`
	Buffers   []bufferSortEntryDTO `json:"buffers"`
}

func (s *Server) reorderNetworkBuffers(w http.ResponseWriter, r *http.Request) {
	networkID, ok := parseNetworkID(w, r)
	if !ok {
		return
	}
	if _, err := ircdb.GetNetwork(r.Context(), s.Stores.Control, networkID); err != nil {
		writeNetworkDBError(w, err, http.StatusInternalServerError)
		return
	}
	var req reorderBuffersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	entries, err := ircdb.ReorderNetworkBuffers(r.Context(), s.Stores.Control, networkID, req.IDs)
	if err != nil {
		if errors.Is(err, ircdb.ErrInvalidBufferReorder) {
			http.Error(w, "ids must be a non-empty set of the network's channel buffers", http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	event := bufferReorderEvent{Type: "buffer_reorder", NetworkID: networkID,
		Buffers: make([]bufferSortEntryDTO, 0, len(entries))}
	for _, e := range entries {
		event.Buffers = append(event.Buffers, bufferSortEntryDTO{ID: e.ID, SortOrder: e.SortOrder})
	}
	if s.Hub != nil {
		s.Hub.Publish(event)
	}
	writeJSON(w, http.StatusOK, event)
}
