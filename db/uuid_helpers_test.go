package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestParseUUIDRejectsMalformedLength(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"short", make([]byte, 15)},
		{"long", make([]byte, 17)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseUUID(c.in)
			if !errors.Is(err, ErrCorruptUUID) {
				t.Fatalf("parseUUID(%q) err = %v, want ErrCorruptUUID", c.name, err)
			}
		})
	}
}

func TestParseUUIDRoundTrip(t *testing.T) {
	in := uuid.Must(uuid.NewV7())
	got, err := parseUUID(in[:])
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("parseUUID round-trip = %s, want %s", got, in)
	}
}

// TestScanUUIDStaysSoftForNullableColumns locks in the contract that scanUUID
// returns uuid.Nil silently for malformed/missing input. Nullable BLOB columns
// (e.g. buffers.last_seen_id) depend on this — corruption-loud behavior would
// regress them. parseUUID is the corruption-loud counterpart.
func TestScanUUIDStaysSoftForNullableColumns(t *testing.T) {
	for _, in := range [][]byte{nil, {}, make([]byte, 8)} {
		if got := scanUUID(in); got != uuid.Nil {
			t.Fatalf("scanUUID(%v) = %s, want uuid.Nil", in, got)
		}
	}
}

// openControlWithoutLengthCheck opens a control DB and strips the
// `length(id) = 16` CHECK from networks so a corruption test can inject a
// malformed id. Returns the re-opened *sql.DB (the original handle must be
// closed and reopened so connections re-parse the mutated schema).
func openControlWithoutLengthCheck(t *testing.T, path string) *sql.DB {
	t.Helper()
	ctx := t.Context()
	d, err := OpenControl(path)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	steps := []string{
		`PRAGMA writable_schema = ON`,
		`UPDATE sqlite_master SET sql = replace(sql, ' CHECK (length(id) = 16)', '') WHERE type='table' AND name='networks'`,
		`PRAGMA writable_schema = OFF`,
	}
	for _, s := range steps {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			t.Fatalf("strip check %q: %v", s, err)
		}
	}
	_ = conn.Close()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = OpenControl(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestListNetworksRejectsCorruptID locks in that required-ID columns surface
// corruption rather than silently becoming uuid.Nil. Regression guard for the
// refactor where scanUUID was wrongly used for required IDs.
func TestListNetworksRejectsCorruptID(t *testing.T) {
	ctx := t.Context()
	d := openControlWithoutLengthCheck(t, filepath.Join(t.TempDir(), "control.db"))
	defer func() { _ = d.Close() }()

	if _, err := d.ExecContext(ctx,
		`INSERT INTO networks(id, name, name_ci, kind, host, port, tls, nick, sort_order, created_at)
		 VALUES (?, ?, ?, 'irc', ?, ?, 1, ?, 0, ?)`,
		[]byte{1, 2, 3, 4, 5, 6, 7, 8}, "Libera", "libera", "irc.libera.chat", 6697, "tester", Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := ListNetworks(ctx, d); !errors.Is(err, ErrCorruptUUID) {
		t.Fatalf("ListNetworks err = %v, want ErrCorruptUUID", err)
	}
}

// TestListNetworksWithSASLRejectsCorruptID covers the export path that also
// scans required IDs.
func TestListNetworksWithSASLRejectsCorruptID(t *testing.T) {
	ctx := t.Context()
	d := openControlWithoutLengthCheck(t, filepath.Join(t.TempDir(), "control.db"))
	defer func() { _ = d.Close() }()

	if _, err := d.ExecContext(ctx,
		`INSERT INTO networks(id, name, name_ci, kind, host, port, tls, nick, sort_order, created_at, disabled)
		 VALUES (?, ?, ?, 'irc', ?, ?, 1, ?, 0, ?, 0)`,
		[]byte{1, 2, 3}, "Libera", "libera", "irc.libera.chat", 6697, "tester", Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := ListNetworksWithSASL(ctx, d); !errors.Is(err, ErrCorruptUUID) {
		t.Fatalf("ListNetworksWithSASL err = %v, want ErrCorruptUUID", err)
	}
}

// TestListNetworkConnectCommandsReturnsEmptySliceNotNil locks in the sqlc
// emit_empty_slices: true contract. The web client tolerates either shape
// (`data.commands ?? []`), but downstream contracts depend on the JSON
// response being `[]` not `null`. Flipping the sqlc flag back to false would
// silently break this; this test catches it.
func TestListNetworkConnectCommandsReturnsEmptySliceNotNil(t *testing.T) {
	ctx := t.Context()
	d, err := OpenControl(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	n, err := CreateNetwork(ctx, d, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := ListNetworkConnectCommands(ctx, d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ListNetworkConnectCommands returned nil; want non-nil empty slice (regression: sqlc emit_empty_slices)")
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

func TestListIgnoresReturnsEmptySliceNotNil(t *testing.T) {
	ctx := t.Context()
	d, err := OpenControl(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	n, err := CreateNetwork(ctx, d, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ListIgnores(ctx, d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ListIgnores returned nil; want non-nil empty slice (regression: sqlc emit_empty_slices)")
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

// TestInsertMessagePreviewLinksBatchInsertsAll locks in the batch-insert
// behavior of the prepared-statement loop kept for perf reasons. Regression
// test that a future drift to per-row generated calls (or breaking the loop)
// would catch.
func TestInsertMessagePreviewLinksBatchInsertsAll(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "log.db")
	d, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	bufID := uuid.Must(uuid.NewV7())
	if _, err := d.ExecContext(ctx,
		`INSERT INTO buffers(id, name, kind, created_at) VALUES (?, ?, 'channel', ?)`,
		bufID[:], "#test", Now()); err != nil {
		t.Fatal(err)
	}

	// 3 distinct messages × 2 preview URLs = 6 rows.
	var msgIDs []uuid.UUID
	for i := 0; i < 3; i++ {
		mid := uuid.Must(uuid.NewV7())
		msgIDs = append(msgIDs, mid)
		if _, err := d.ExecContext(ctx,
			`INSERT INTO messages(id, buffer_id, ts, sender, kind, content, raw)
			 VALUES (?, ?, ?, 'tester', 'privmsg', '', '')`,
			mid[:], bufID[:], Now()); err != nil {
			t.Fatal(err)
		}
	}

	links := make([]MessagePreviewLink, 0, 6)
	for _, mid := range msgIDs {
		links = append(links,
			MessagePreviewLink{MessageID: mid, URL: "https://a.example/" + mid.String(), Position: 0},
			MessagePreviewLink{MessageID: mid, URL: "https://b.example/" + mid.String(), Position: 1},
		)
	}
	if err := InsertMessagePreviewLinks(ctx, d, links); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_previews`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("inserted rows = %d, want 6", count)
	}

	// Re-running is idempotent (INSERT OR IGNORE).
	if err := InsertMessagePreviewLinks(ctx, d, links); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_previews`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("rows after re-insert = %d, want 6 (INSERT OR IGNORE)", count)
	}
}
