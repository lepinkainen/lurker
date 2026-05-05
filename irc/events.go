package irc

import "github.com/google/uuid"

// MessageEvent is published after a message row is successfully written.
// The JSON tags match the wire shape sent to WebSocket clients.
type MessageEvent struct {
	Type      string    `json:"type"`
	ID        uuid.UUID `json:"id"`
	NetworkID uuid.UUID `json:"network_id"`
	BufferID  uuid.UUID `json:"buffer_id"`
	MsgID     string    `json:"msgid,omitzero"`
	TS        string    `json:"ts"`
	Sender    string    `json:"sender"`
	Account   string    `json:"account,omitzero"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target,omitzero"`
	Content   string    `json:"content"`
	// Server-computed semantic flags so every connected client renders
	// identical mention/unread state without re-deriving from raw fields.
	DisplayKind    string `json:"display_kind"`
	IsSelf         bool   `json:"is_self,omitzero"`
	MentionsMe     bool   `json:"mentions_me,omitzero"`
	CountsAsUnread bool   `json:"counts_as_unread,omitzero"`
}

// WithSemantics fills the four semantic flag fields based on the event's
// kind/sender/content and the network's current nick. Returns the receiver
// for fluent use at the publish site.
func (e *MessageEvent) WithSemantics(nick string) *MessageEvent {
	sem := ComputeMessageSemantics(e.Kind, e.Sender, e.Content, nick)
	e.DisplayKind = sem.DisplayKind
	e.IsSelf = sem.IsSelf
	e.MentionsMe = sem.MentionsMe
	e.CountsAsUnread = sem.CountsAsUnread
	return e
}

// BufferCreatedEvent is published the first time we see activity in a
// buffer that didn't exist yet (autojoin, inbound PM, network status).
type BufferCreatedEvent struct {
	Type      string    `json:"type"`
	ID        uuid.UUID `json:"id"`
	NetworkID uuid.UUID `json:"network_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	CreatedAt string    `json:"created_at"`
}

// BufferUpdateEvent announces mutable buffer state changes.
type BufferUpdateEvent struct {
	Type       string    `json:"type"`
	ID         uuid.UUID `json:"id"`
	NetworkID  uuid.UUID `json:"network_id"`
	Topic      string    `json:"topic,omitzero"`
	Joined     bool      `json:"joined"`
	LastSeenID uuid.UUID `json:"last_seen_id,omitzero"`
}

// PresenceEvent is a lightweight stream event for user presence-ish changes.
type PresenceEvent struct {
	Type      string    `json:"type"`
	NetworkID uuid.UUID `json:"network_id"`
	BufferID  uuid.UUID `json:"buffer_id,omitzero"`
	Nick      string    `json:"nick,omitzero"`
	State     string    `json:"state"`
	Target    string    `json:"target,omitzero"`
}

// MemberListEvent publishes the full known member list for a channel.
type MemberListEvent struct {
	Type      string        `json:"type"`
	NetworkID uuid.UUID     `json:"network_id"`
	BufferID  uuid.UUID     `json:"buffer_id"`
	Channel   string        `json:"channel"`
	Members   []ChannelUser `json:"members"`
}

// ChannelUser is one user in a channel member list.
type ChannelUser struct {
	Nick   string `json:"nick"`
	Prefix string `json:"prefix,omitzero"`
	Away   bool   `json:"away"`
	Self   bool   `json:"self"`
}

// NetworkStateEvent announces connection state transitions.
type NetworkStateEvent struct {
	Type      string    `json:"type"`
	NetworkID uuid.UUID `json:"network_id"`
	State     string    `json:"state"` // "connecting" | "connected" | "disconnected"
}

// ChannelListEntry is one entry from a server LIST reply.
type ChannelListEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Topic string `json:"topic,omitzero"`
}

// ChannelListEvent delivers /LIST results. Streaming: one event per batch of
// entries, final event has Done=true.
type ChannelListEvent struct {
	Type      string             `json:"type"`
	NetworkID uuid.UUID          `json:"network_id"`
	Entries   []ChannelListEntry `json:"entries"`
	Done      bool               `json:"done"`
}

// PreviewEnqueuer is the narrow hook the handler uses to kick off URL
// previews for a newly stored message. It is satisfied by *preview.Service.
type PreviewEnqueuer interface {
	Enqueue(networkID, bufferID, messageID uuid.UUID, content string)
}
