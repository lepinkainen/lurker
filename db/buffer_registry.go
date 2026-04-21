package db

import (
	"context"
	"database/sql"
)

// UpsertBufferRegistry creates or looks up a global buffer registry row.
func UpsertBufferRegistry(ctx context.Context, d *sql.DB, networkID int64, name, kind string) (id int64, created bool, buf Buffer, err error) {
	name, kind = normalizeBufferIdentity(name, kind)
	now := Now()
	row := bufferRegistryRow{NetworkID: networkID, Name: name, Kind: kind, CreatedAt: now}
	id, created, stored, err := upsertBufferRegistryOrLogRow(
		ctx,
		d,
		`SELECT id FROM buffer_registry WHERE network_id = ? AND name = ?`,
		[]any{networkID, name},
		`INSERT INTO buffer_registry(network_id, name, kind, created_at) VALUES (?, ?, ?, ?)`,
		[]any{networkID, name, kind, now},
		row,
		func(row *bufferRegistryRow, id int64) { row.ID = id },
	)
	if err != nil {
		return 0, false, Buffer{}, err
	}
	return id, created, Buffer{
		ID: stored.ID, NetworkID: stored.NetworkID, Name: stored.Name, Kind: stored.Kind, CreatedAt: stored.CreatedAt,
	}, nil
}

// LookupBufferRegistry resolves a global buffer registry row by ID.
func LookupBufferRegistry(ctx context.Context, d *sql.DB, bufferID int64) (networkID int64, name, kind string, err error) {
	err = d.QueryRowContext(ctx,
		`SELECT network_id, name, kind FROM buffer_registry WHERE id = ?`, bufferID,
	).Scan(&networkID, &name, &kind)
	return
}
