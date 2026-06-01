package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// LookupBufferRegistry resolves a global buffer registry row by ID.
func LookupBufferRegistry(ctx context.Context, d *sql.DB, bufferID uuid.UUID) (networkID uuid.UUID, name, kind string, err error) {
	row, err := controldb.New(d).LookupBufferRegistry(ctx, bufferID[:])
	if err != nil {
		return uuid.Nil, "", "", err
	}
	nid, err := parseUUID(row.NetworkID)
	if err != nil {
		return uuid.Nil, "", "", err
	}
	return nid, row.Name, row.Kind, nil
}
