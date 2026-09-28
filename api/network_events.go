package api

import (
	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
)

// Network configuration broadcasts. Mirrors the buffer_settings pattern: the
// REST handler answers the caller, then publishes so every other open client
// converges without a reload. network_state (connection status) is separate
// and comes from the IRC runtime.

type networkEvent struct {
	Type    string     `json:"type"` // "network_created" | "network_updated"
	Network networkDTO `json:"network"`
}

type networkDeletedEvent struct {
	Type string    `json:"type"`
	ID   uuid.UUID `json:"id"`
}

type networkSortEntryDTO struct {
	ID        uuid.UUID `json:"id"`
	SortOrder int       `json:"sort_order"`
}

type networkReorderEvent struct {
	Type     string                `json:"type"`
	Networks []networkSortEntryDTO `json:"networks"`
}

// HandleNickChanged broadcasts network_updated when the IRC runtime itself
// persists a new self nick (registration alt nick or a NICK on our own
// connection). This is the runtime-side equivalent of patchNetwork's
// broadcast: without it, clients keep showing the stale nick until restart
// because only the REST path used to publish network_updated. Wired as
// irc.Manager's nick-changed hook (irc cannot import api, so main.go sets
// this as the callback).
func (s *Server) HandleNickChanged(n ircdb.Network) {
	status := ""
	if s.Manager != nil {
		status = s.Manager.StateSnapshot()[n.ID]
	}
	s.publish(networkEvent{Type: "network_updated", Network: s.toNetworkDTO(n, status)})
}

// publish is a nil-safe Hub.Publish for handlers that tests may construct
// without a hub.
func (s *Server) publish(e any) {
	if s.Hub != nil {
		s.Hub.Publish(e)
	}
}
