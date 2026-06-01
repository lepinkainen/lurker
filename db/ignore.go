package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// CreateIgnore adds an ignore mask for a network. Silently ignores duplicates.
func CreateIgnore(ctx context.Context, d *sql.DB, networkID uuid.UUID, mask string) error {
	id := newID()
	return controldb.New(d).CreateIgnore(ctx, controldb.CreateIgnoreParams{
		ID:        id[:],
		NetworkID: networkID[:],
		Mask:      mask,
		CreatedAt: Now(),
	})
}

// DeleteIgnore removes an ignore mask for a network.
func DeleteIgnore(ctx context.Context, d *sql.DB, networkID uuid.UUID, mask string) error {
	return controldb.New(d).DeleteIgnore(ctx, controldb.DeleteIgnoreParams{
		NetworkID: networkID[:],
		Mask:      mask,
	})
}

// ListIgnores returns all ignore masks for a network.
func ListIgnores(ctx context.Context, d *sql.DB, networkID uuid.UUID) ([]string, error) {
	return controldb.New(d).ListIgnores(ctx, networkID[:])
}
