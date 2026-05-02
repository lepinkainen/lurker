package db

import "github.com/google/uuid"

// newID generates a new UUIDv7 (time-ordered).
func newID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// uuidParam returns nil for the zero UUID, else the 16-byte slice. Use for
// nullable BLOB columns.
func uuidParam(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id[:]
}

// scanUUID converts a byte slice (typically from a nullable BLOB scan) to a
// uuid.UUID. Returns uuid.Nil for nil/empty input or any non-16-byte slice.
func scanUUID(b []byte) uuid.UUID {
	if len(b) != 16 {
		return uuid.Nil
	}
	var out uuid.UUID
	copy(out[:], b)
	return out
}
