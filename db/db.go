// Package db opens the SQLite store and applies embedded migrations.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed control_migrations/*.sql
var controlMigrationsFS embed.FS

//go:embed log_migrations/*.sql
var logMigrationsFS embed.FS

// OpenControl opens the control-plane SQLite database and applies the control
// migration set independently from the log-store migration set.
func OpenControl(path string) (*sql.DB, error) {
	return openAndMigrate(path, controlMigrationsFS, "control_migrations")
}

// OpenLog opens a per-network log database and applies the log migration set.
func OpenLog(path string) (*sql.DB, error) {
	return openAndMigrate(path, logMigrationsFS, "log_migrations")
}

func openAndMigrate(path string, migrationFS embed.FS, migrationDir string) (*sql.DB, error) {
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := d.Ping(); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(d, migrationFS, migrationDir); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return nil
}

func migrate(d *sql.DB, migrationFS fs.FS, migrationDir string) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationFS, migrationDir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists int
		if err := d.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version=?`, name).Scan(&exists); err != nil {
			return err
		}
		if exists == 1 {
			continue
		}
		sqlBytes, err := fs.ReadFile(migrationFS, filepath.ToSlash(filepath.Join(migrationDir, name)))
		if err != nil {
			return err
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		slog.Info("migration applied", "version", name, "dir", migrationDir)
	}
	return nil
}
