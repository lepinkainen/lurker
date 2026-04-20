package db

import (
	"context"
	"database/sql"
)

func UpsertBufferRegistry(ctx context.Context, d *sql.DB, networkID int64, name, kind string) (id int64, created bool, buf Buffer, err error) {
	name, kind = normalizeBufferIdentity(name, kind)
	now := Now()
	return upsertBufferRow(
		ctx,
		d,
		`SELECT id FROM buffer_registry WHERE network_id = ? AND name = ?`,
		[]any{networkID, name},
		`INSERT INTO buffer_registry(network_id, name, kind, created_at) VALUES (?, ?, ?, ?)`,
		[]any{networkID, name, kind, now},
		bufferRow{NetworkID: networkID, Name: name, Kind: kind, CreatedAt: now},
	)
}

func LookupBufferRegistry(ctx context.Context, d *sql.DB, bufferID int64) (networkID int64, name, kind string, err error) {
	err = d.QueryRowContext(ctx,
		`SELECT network_id, name, kind FROM buffer_registry WHERE id = ?`, bufferID,
	).Scan(&networkID, &name, &kind)
	return
}
