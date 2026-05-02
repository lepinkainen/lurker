package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
)

type bufferSettingsPatchDTO struct {
	ShowEmbeds             *bool `json:"show_embeds"`
	ShowPresenceEvents     *bool `json:"show_presence_events"`
	CollapsePresenceEvents *bool `json:"collapse_presence_events"`
	Pinned                 *bool `json:"pinned"`
}

type bufferSettingsEvent struct {
	Type                   string    `json:"type"`
	ID                     uuid.UUID `json:"id"`
	ShowEmbeds             bool      `json:"show_embeds"`
	ShowPresenceEvents     bool      `json:"show_presence_events"`
	CollapsePresenceEvents bool      `json:"collapse_presence_events"`
	Pinned                 bool      `json:"pinned"`
}

func (s *Server) patchBufferSettings(w http.ResponseWriter, r *http.Request) {
	bufferID, ok := parsePathUUID(w, r, "id", "bad buffer id")
	if !ok {
		return
	}
	var in bufferSettingsPatchDTO
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	settings, err := ircdb.UpdateBufferSettings(r.Context(), s.Stores.Control, bufferID, ircdb.BufferSettingsPatch{
		ShowEmbeds:             in.ShowEmbeds,
		ShowPresenceEvents:     in.ShowPresenceEvents,
		CollapsePresenceEvents: in.CollapsePresenceEvents,
		Pinned:                 in.Pinned,
	})
	if err != nil {
		switch {
		case errors.Is(err, ircdb.ErrBufferNotFound):
			http.Error(w, "unknown buffer", http.StatusNotFound)
		case errors.Is(err, ircdb.ErrBufferSettingsUnsupported):
			http.Error(w, "settings are only supported for channel buffers", http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	event := bufferSettingsEvent{
		Type: "buffer_settings", ID: settings.BufferID,
		ShowEmbeds: settings.ShowEmbeds, ShowPresenceEvents: settings.ShowPresenceEvents,
		CollapsePresenceEvents: settings.CollapsePresenceEvents, Pinned: settings.Pinned,
	}
	if s.Hub != nil {
		s.Hub.Publish(event)
	}
	writeJSON(w, http.StatusOK, event)
}
