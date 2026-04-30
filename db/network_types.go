package db

// Network is the minimal network row used at startup.
type Network struct {
	ID        int64
	Name      string
	Host      string
	Port      int
	TLS       bool
	Nick      string
	Realname  string
	SASLUser  string
	SASLPass  string
	SortOrder int
	Disabled  bool
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
	ID                     int64
	NetworkID              int64
	Name                   string
	Kind                   string
	Topic                  string
	LastSeenID             int64
	CreatedAt              string
	ShowEmbeds             bool
	ShowPresenceEvents     bool
	CollapsePresenceEvents bool
	Pinned                 bool
}

// StoredMessage is global/API-facing message view built from per-network log
// rows plus explicit global metadata.
type StoredMessage struct {
	ID        int64
	NetworkID int64
	BufferID  int64
	MsgID     string
	TS        string
	Sender    string
	Account   string
	Kind      string
	Target    string
	Content   string
}
