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
	Archived               *bool `json:"archived"`
}

type bufferSettingsEvent struct {
	Type                   string    `json:"type"`
	ID                     uuid.UUID `json:"id"`
	ShowEmbeds             bool      `json:"show_embeds"`
	ShowPresenceEvents     bool      `json:"show_presence_events"`
	CollapsePresenceEvents bool      `json:"collapse_presence_events"`
	Pinned                 bool      `json:"pinned"`
	Archived               bool      `json:"archived"`
}

func bufferSettingsEventFrom(s ircdb.BufferSettings) bufferSettingsEvent {
	return bufferSettingsEvent{
		Type: "buffer_settings", ID: s.BufferID,
		ShowEmbeds: s.ShowEmbeds, ShowPresenceEvents: s.ShowPresenceEvents,
		CollapsePresenceEvents: s.CollapsePresenceEvents, Pinned: s.Pinned,
		Archived: s.Archived,
	}
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
		Archived:               in.Archived,
	})
	if err != nil {
		switch {
		case errors.Is(err, ircdb.ErrBufferNotFound):
			http.Error(w, "unknown buffer", http.StatusNotFound)
		case errors.Is(err, ircdb.ErrBufferSettingsUnsupported):
			http.Error(w, "settings are only supported for channel and query buffers", http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	event := bufferSettingsEventFrom(settings)
	if s.Hub != nil {
		s.Hub.Publish(event)
	}
	writeJSON(w, http.StatusOK, event)
}
