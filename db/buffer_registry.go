package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// LookupBufferRegistry resolves a global buffer registry row by ID.
func LookupBufferRegistry(ctx context.Context, d *sql.DB, bufferID uuid.UUID) (networkID uuid.UUID, name, kind string, err error) {
	err = d.QueryRowContext(ctx,
		`SELECT network_id, name, kind FROM buffer_registry WHERE id = ?`, bufferID[:],
	).Scan(&networkID, &name, &kind)
	return
}
