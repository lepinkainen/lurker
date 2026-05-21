package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func tableHasColumn(ctx context.Context, d *sql.DB, table, column string) (bool, error) {
	rows, err := d.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func TestOpenLogAppliesLogMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libera.db")
	d, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	assertTableExists(t, d, "schema_migrations")
	assertTableExists(t, d, "buffers")
	assertTableExists(t, d, "messages")
	assertTableExists(t, d, "messages_fts")
	assertTableMissing(t, d, "networks")

	assertColumnMissing(t, d, "buffers", "network_id")
	assertColumnMissing(t, d, "messages", "network_id")
}

func assertTableMissing(t *testing.T, d *sql.DB, name string) {
	t.Helper()
	var found string
	err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	if err == nil {
		t.Fatalf("table %s unexpectedly exists", name)
	}
}

func assertColumnMissing(t *testing.T, d *sql.DB, table, column string) {
	t.Helper()
	ok, err := tableHasColumn(t.Context(), d, table, column)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("column %s.%s unexpectedly exists", table, column)
	}
}
