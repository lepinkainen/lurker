package db

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// ListNetworkConnectCommands returns raw IRC connect commands in saved order.
func ListNetworkConnectCommands(ctx context.Context, d *sql.DB, networkID uuid.UUID) ([]string, error) {
	return controldb.New(d).ListNetworkConnectCommands(ctx, networkID[:])
}

// SetNetworkConnectCommands replaces raw IRC connect commands for a network.
// Commands are trimmed and blank lines are ignored.
func SetNetworkConnectCommands(ctx context.Context, d *sql.DB, networkID uuid.UUID, commands []string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	q := controldb.New(tx)
	if err := q.DeleteNetworkConnectCommands(ctx, networkID[:]); err != nil {
		return err
	}

	position := int64(0)
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if err := q.InsertNetworkConnectCommand(ctx, controldb.InsertNetworkConnectCommandParams{
			NetworkID: networkID[:],
			Position:  position,
			Command:   command,
		}); err != nil {
			return err
		}
		position++
	}
	return tx.Commit()
}
