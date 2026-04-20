package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

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
