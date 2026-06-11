package db

import (
	"context"
	"database/sql"
	"strings"

	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// ListHighlights returns all global highlight patterns in creation order.
func ListHighlights(ctx context.Context, d *sql.DB) ([]string, error) {
	return controldb.New(d).ListHighlights(ctx)
}

// SetHighlights replaces the global highlight pattern list. Patterns are
// trimmed and blank entries are ignored.
func SetHighlights(ctx context.Context, d *sql.DB, patterns []string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	q := controldb.New(tx)
	if err := q.DeleteAllHighlights(ctx); err != nil {
		return err
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		id := newID()
		if err := q.InsertHighlight(ctx, controldb.InsertHighlightParams{
			ID:        id[:],
			Pattern:   pattern,
			CreatedAt: Now(),
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
