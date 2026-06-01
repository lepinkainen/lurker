package db

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// newID generates a new UUIDv7 (time-ordered).
func newID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// ErrCorruptUUID indicates a stored BLOB value was not a valid 16-byte UUID.
// Surfaced when scanning required ID columns; do not use scanUUID for those
// because it silently coerces malformed values to uuid.Nil.
var ErrCorruptUUID = errors.New("db: corrupt UUID in stored row")

// scanUUID is for nullable BLOB columns (e.g. buffers.last_seen_id) where the
// canonical "missing" value is uuid.Nil. It returns uuid.Nil for nil/empty
// input or any non-16-byte slice without error. Do NOT use for required ID
// columns: use parseUUID instead so corruption surfaces loudly.
func scanUUID(b []byte) uuid.UUID {
	if len(b) != 16 {
		return uuid.Nil
	}
	var out uuid.UUID
	copy(out[:], b)
	return out
}

// parseUUID converts a 16-byte BLOB to uuid.UUID, returning ErrCorruptUUID for
// any other length. Use for required ID columns where uuid.Nil is not a valid
// value (e.g. networks.id, buffer_registry.id, buffers.id).
func parseUUID(b []byte) (uuid.UUID, error) {
	if len(b) != 16 {
		return uuid.Nil, fmt.Errorf("%w: got %d bytes", ErrCorruptUUID, len(b))
	}
	var out uuid.UUID
	copy(out[:], b)
	return out, nil
}
