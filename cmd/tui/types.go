package main

import (
	"context"
	"time"

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
	ID                     uuid.UUID `json:"id"`
	NetworkID              uuid.UUID `json:"network_id"`
	Name                   string    `json:"name"`
	Kind                   string    `json:"kind"`
	Topic                  string    `json:"topic"`
	Joined                 bool      `json:"joined"`
	LastSeenID             uuid.UUID `json:"last_seen_id"`
	Unread                 int       `json:"unread"`
	Mentions               int       `json:"mentions"`
	ShowPresenceEvents     bool      `json:"show_presence_events"`
	CollapsePresenceEvents bool      `json:"collapse_presence_events"`
}

type messageDTO struct {
	ID             uuid.UUID `json:"id"`
	NetworkID      uuid.UUID `json:"network_id"`
	BufferID       uuid.UUID `json:"buffer_id"`
	TS             string    `json:"ts"`
	Sender         string    `json:"sender"`
	Kind           string    `json:"kind"`
	Target         string    `json:"target"`
	Content        string    `json:"content"`
	MentionsMe     bool      `json:"mentions_me"`
	CountsAsUnread bool      `json:"counts_as_unread"`
	// parsed-once cache: TS is RFC3339Nano. Parsing it on every viewport
	// refresh is wasted work — refreshViewport fires on every WS message.
	TSParsed time.Time `json:"-"`
}

type channelMember struct {
	Nick     string `json:"nick"`
	Prefix   string `json:"prefix"`
	Realname string `json:"realname"`
	Away     bool   `json:"away"`
	Self     bool   `json:"self"`
}

type stateResponse struct {
	Networks        []networkDTO               `json:"networks"`
	Buffers         []bufferDTO                `json:"buffers"`
	InitialMessages map[string][]messageDTO    `json:"initial_messages"`
	Members         map[string][]channelMember `json:"members"`
}

// wsEvent is a union of all event shapes from /api/stream.
// Fields not relevant to a given type are zero-valued.
type wsEvent struct {
	Type string `json:"type"`
	// message
	ID             uuid.UUID `json:"id"`
	NetworkID      uuid.UUID `json:"network_id"`
	BufferID       uuid.UUID `json:"buffer_id"`
	TS             string    `json:"ts"`
	Sender         string    `json:"sender"`
	Kind           string    `json:"kind"`
	Target         string    `json:"target"`
	Content        string    `json:"content"`
	MentionsMe     bool      `json:"mentions_me"`
	CountsAsUnread bool      `json:"counts_as_unread"`
	// buffer_update
	Topic      string    `json:"topic"`
	Joined     bool      `json:"joined"`
	LastSeenID uuid.UUID `json:"last_seen_id"`
	Unread     int       `json:"unread"`
	Mentions   int       `json:"mentions"`
	// buffer_settings
	ShowEmbeds             bool `json:"show_embeds"`
	ShowPresenceEvents     bool `json:"show_presence_events"`
	CollapsePresenceEvents bool `json:"collapse_presence_events"`
	Pinned                 bool `json:"pinned"`
	// network_state
	State string `json:"state"`
	// buffer_created
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	// member_list
	Channel string          `json:"channel"`
	Members []channelMember `json:"members"`
	// history_result
	ReqID    string       `json:"req_id"`
	Messages []messageDTO `json:"messages"`
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
