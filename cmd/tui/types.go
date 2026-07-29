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
	ID         uuid.UUID `json:"id"`
	NetworkID  uuid.UUID `json:"network_id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Topic      string    `json:"topic"`
	TopicSetBy string    `json:"topic_set_by"`
	Joined     bool      `json:"joined"`
	LastSeenID uuid.UUID `json:"last_seen_id"`
	// Server-derived "New messages" marker (see
	// ai-docs/behaviors/new-messages-marker.md): id + RFC3339 ts of the
	// oldest counting unread message. Zero values mean no marker.
	MarkerID               uuid.UUID `json:"marker_id"`
	MarkerTS               string    `json:"marker_ts"`
	Unread                 int       `json:"unread"`
	Mentions               int       `json:"mentions"`
	ShowPresenceEvents     bool      `json:"show_presence_events"`
	CollapsePresenceEvents bool      `json:"collapse_presence_events"`
	Pinned                 bool      `json:"pinned"`
	Archived               bool      `json:"archived"`
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
	Highlight      bool      `json:"highlight"`
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
	Highlight      bool      `json:"highlight"`
	CountsAsUnread bool      `json:"counts_as_unread"`
	IsSelf         bool      `json:"is_self"`
	// buffer_update — pointers: fields absent from the wire mean "unchanged"
	// (e.g. the mark_read echo carries none of these), while a present empty
	// string means "set to empty" (cleared topic).
	Topic      *string `json:"topic"`
	TopicSetBy *string `json:"topic_set_by"`
	Joined     *bool   `json:"joined"`
	// Archived is shared by buffer_update (pointer semantics: absent =
	// unchanged) and buffer_settings (always present on the wire).
	Archived   *bool     `json:"archived"`
	LastSeenID uuid.UUID `json:"last_seen_id"`
	// marker_id/marker_ts belong to the mark_read variant of buffer_update
	// (discriminated by last_seen_id != Nil), which ALWAYS carries marker_id:
	// JSON null (or an omitted marker_ts) decodes to a nil pointer meaning
	// "caught up — clear the marker". On the topic/joined variant the keys
	// are absent and must be ignored.
	MarkerID *uuid.UUID `json:"marker_id"`
	MarkerTS *string    `json:"marker_ts"`
	Unread   int        `json:"unread"`
	Mentions int        `json:"mentions"`
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
	// channel_list (streamed; final batch has Done=true)
	Entries []channelListEntry `json:"entries"`
	Done    bool               `json:"done"`
}

type channelListEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Topic string `json:"topic"`
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
	// isArchiveToggle marks the per-network "Archives (n)" fold row.
	// Activating it flips the fold instead of switching buffers.
	isArchiveToggle bool
	// dim renders the row muted (archived buffers).
	dim bool
}
