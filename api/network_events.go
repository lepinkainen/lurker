package api

import "github.com/google/uuid"

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

// publish is a nil-safe Hub.Publish for handlers that tests may construct
// without a hub.
func (s *Server) publish(e any) {
	if s.Hub != nil {
		s.Hub.Publish(e)
	}
}
