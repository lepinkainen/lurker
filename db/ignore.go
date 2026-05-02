package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// CreateIgnore adds an ignore mask for a network. Silently ignores duplicates.
func CreateIgnore(ctx context.Context, d *sql.DB, networkID uuid.UUID, mask string) error {
	id := newID()
	_, err := d.ExecContext(ctx,
		`INSERT OR IGNORE INTO ignores (id, network_id, mask, created_at) VALUES (?, ?, ?, ?)`,
		id[:], networkID[:], mask, Now())
	return err
}

// DeleteIgnore removes an ignore mask for a network.
func DeleteIgnore(ctx context.Context, d *sql.DB, networkID uuid.UUID, mask string) error {
	_, err := d.ExecContext(ctx,
		`DELETE FROM ignores WHERE network_id = ? AND mask = ?`,
		networkID[:], mask)
	return err
}

// ListIgnores returns all ignore masks for a network.
func ListIgnores(ctx context.Context, d *sql.DB, networkID uuid.UUID) ([]string, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT mask FROM ignores WHERE network_id = ? ORDER BY created_at`,
		networkID[:])
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var masks []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		masks = append(masks, m)
	}
	return masks, rows.Err()
}
