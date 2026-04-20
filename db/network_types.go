package db

// Network is the minimal network row used at startup.
type Network struct {
	ID       int64
	Name     string
	Host     string
	Port     int
	TLS      bool
	Nick     string
	Realname string
	SASLUser string
	SASLPass string
}

// BufferKind enumerates the rows allowed in buffers.kind.
const (
	BufferChannel = "channel"
	BufferQuery   = "query"
	BufferStatus  = "status"
)

// Buffer is the DTO returned for newly-created buffers so the API can
// push a buffer_created event without a second query.
type Buffer = bufferRow

// StoredMessage mirrors the messages table for API responses.
type StoredMessage = messageRow
