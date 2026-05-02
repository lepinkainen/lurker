package db

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"
)

// ListNetworkConnectCommands returns raw IRC connect commands in saved order.
func ListNetworkConnectCommands(ctx context.Context, d *sql.DB, networkID uuid.UUID) ([]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT command FROM network_connect_commands WHERE network_id = ? ORDER BY position`, networkID[:])
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var command string
		if err := rows.Scan(&command); err != nil {
			return nil, err
		}
		out = append(out, command)
	}
	return out, rows.Err()
}

// SetNetworkConnectCommands replaces raw IRC connect commands for a network.
// Commands are trimmed and blank lines are ignored.
func SetNetworkConnectCommands(ctx context.Context, d *sql.DB, networkID uuid.UUID, commands []string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM network_connect_commands WHERE network_id = ?`, networkID[:])
	if err != nil {
		return err
	}
	if _, err := res.RowsAffected(); err != nil {
		return err
	}

	position := 0
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO network_connect_commands(network_id, position, command) VALUES (?, ?, ?)`, networkID[:], position, command); err != nil {
			return err
		}
		position++
	}
	return tx.Commit()
}
