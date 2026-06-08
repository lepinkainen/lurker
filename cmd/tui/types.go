package main

import (
	"context"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// API DTOs — mirror the shapes returned by /api/state and /api/stream.

type networkDTO struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	SortOrder int       `json:"sort_order"`
	Disabled  bool      `json:"disabled"`
	Nick      string    `json:"nick"`
}

type bufferDTO struct {
	ID         uuid.UUID `json:"id"`
	NetworkID  uuid.UUID `json:"network_id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Topic      string    `json:"topic"`
	Joined     bool      `json:"joined"`
	LastSeenID uuid.UUID `json:"last_seen_id"`
}

type messageDTO struct {
	ID        uuid.UUID `json:"id"`
	NetworkID uuid.UUID `json:"network_id"`
	BufferID  uuid.UUID `json:"buffer_id"`
	TS        string    `json:"ts"`
	Sender    string    `json:"sender"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"`
	Content   string    `json:"content"`
}

type stateResponse struct {
	Networks        []networkDTO            `json:"networks"`
	Buffers         []bufferDTO             `json:"buffers"`
	InitialMessages map[string][]messageDTO `json:"initial_messages"`
}

// wsEvent is a union of all event shapes from /api/stream.
// Fields not relevant to a given type are zero-valued.
type wsEvent struct {
	Type string `json:"type"`
	// message
	ID        uuid.UUID `json:"id"`
	NetworkID uuid.UUID `json:"network_id"`
	BufferID  uuid.UUID `json:"buffer_id"`
	TS        string    `json:"ts"`
	Sender    string    `json:"sender"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"`
	Content   string    `json:"content"`
	// buffer_update
	Topic  string `json:"topic"`
	Joined bool   `json:"joined"`
	// network_state
	State string `json:"state"`
	// buffer_created
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// tea messages

type stateLoadedMsg struct{ state *stateResponse }

type wsConnectedMsg struct {
	conn   *websocket.Conn
	ch     <-chan wsEvent
	cancel context.CancelFunc
}

type wsEventMsg wsEvent

type wsErrorMsg struct{ err error }

type errMsg struct{ err error }

// sidebarItem is one row in the left navigation pane.
type sidebarItem struct {
	label     string
	bufferID  uuid.UUID
	networkID uuid.UUID
	isHeader  bool // true for network-name rows
}
