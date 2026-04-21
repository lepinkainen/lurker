package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenControlAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "control.db")
	d, err := OpenControl(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	assertTableExists(t, d, "schema_migrations")
	assertTableExists(t, d, "networks")
	assertTableExists(t, d, "buffer_registry")

	var migrationCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount == 0 {
		t.Fatal("expected control migrations to be recorded")
	}
}

func TestControlNetworksNameCIUnique(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.db")
	d, err := OpenControl(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	_, err = d.Exec(`INSERT INTO networks(name, name_ci, host, port, tls, nick, autoconnect, sort_order, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Libera", "libera", "irc.libera.chat", 6697, 1, "tester", 1, 0, Now())
	if err != nil {
		t.Fatal(err)
	}

	_, err = d.Exec(`INSERT INTO networks(name, name_ci, host, port, tls, nick, autoconnect, sort_order, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"LIBERA", "libera", "irc.example.net", 6667, 0, "tester2", 1, 1, Now())
	if err == nil {
		t.Fatal("expected unique constraint error for networks.name_ci")
	}
}

func assertTableExists(t *testing.T, d *sql.DB, name string) {
	t.Helper()
	var found string
	if err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found); err != nil {
		t.Fatalf("table %s missing: %v", name, err)
	}
}
