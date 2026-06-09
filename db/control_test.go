package db

import (
	"context"
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
	ctx := context.Background()

	// Drive uniqueness via the public CreateNetwork path so the test exercises
	// the same code production callers hit. The underlying schema constraint
	// on networks.name_ci is the safety net but is not tested directly.
	if _, err := CreateNetwork(ctx, d, Network{
		Name: "Libera",
		Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester",
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	if _, err := CreateNetwork(ctx, d, Network{
		Name: "LIBERA",
		Host: "irc.example.net", Port: 6667, Nick: "tester2",
	}); err == nil {
		t.Fatal("expected duplicate-name error for case-insensitive collision")
	}
}

func assertTableExists(t *testing.T, d *sql.DB, name string) {
	t.Helper()
	var found string
	if err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found); err != nil {
		t.Fatalf("table %s missing: %v", name, err)
	}
}
