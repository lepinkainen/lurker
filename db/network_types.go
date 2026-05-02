package db

import "github.com/google/uuid"

// Network is the minimal network row used at startup.
type Network struct {
	ID              uuid.UUID
	Name            string
	Host            string
	Port            int
	TLS             bool
	Nick            string
	Realname        string
	SASLUser        string
	SASLPass        string
	ConnectCommands []string
	SortOrder       int
	Disabled        bool
}

// BufferKind enumerates the rows allowed in buffers.kind.
const (
	BufferChannel = "channel"
	BufferQuery   = "query"
	BufferStatus  = "status"
)

// Buffer is global/API-facing buffer view built from control DB rows plus
// derived or enriched state from per-network log DBs.
type Buffer struct {
	ID                     uuid.UUID
	NetworkID              uuid.UUID
	Name                   string
	Kind                   string
	Topic                  string
	LastSeenID             uuid.UUID
	CreatedAt              string
	ShowEmbeds             bool
	ShowPresenceEvents     bool
	CollapsePresenceEvents bool
	Pinned                 bool
}

// StoredMessage is global/API-facing message view built from per-network log
// rows plus explicit global metadata.
type StoredMessage struct {
	ID        uuid.UUID
	NetworkID uuid.UUID
	BufferID  uuid.UUID
	MsgID     string
	TS        string
	Sender    string
	Account   string
	Kind      string
	Target    string
	Content   string
}
