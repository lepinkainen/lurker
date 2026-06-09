package db

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestMultiStoreOpensDistinctNetworkDBs(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()

	ms, err := OpenMultiStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	first, err := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ms.UpsertNetwork(ctx, Network{Name: "OFTC-test", Host: "irc.oftc.net", Port: 6697, TLS: true, Nick: "b"})
	if err != nil {
		t.Fatal(err)
	}

	wantIDs := []uuid.UUID{first.ID, second.ID}
	slices.SortFunc(wantIDs, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })

	gotIDs := ms.NetworkIDs()
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("network ids = %v, want %v", gotIDs, wantIDs)
	}

	for _, path := range []string{
		filepath.Join(dataDir, "control.db"),
		filepath.Join(dataDir, "libera.db"),
		filepath.Join(dataDir, "oftc-test.db"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
	}

	if _, err := ms.LogStore(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.LogStore(second.ID); err != nil {
		t.Fatal(err)
	}

	if err := ms.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and verify persistence: same IDs, log DBs still openable.
	ms2, err := OpenMultiStore(dataDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = ms2.Close() }()

	gotIDs2 := ms2.NetworkIDs()
	if !slices.Equal(gotIDs2, wantIDs) {
		t.Fatalf("network ids after reopen = %v, want %v", gotIDs2, wantIDs)
	}
	if _, err := ms2.LogStore(first.ID); err != nil {
		t.Fatalf("reopen LogStore(first): %v", err)
	}
	if _, err := ms2.LogStore(second.ID); err != nil {
		t.Fatalf("reopen LogStore(second): %v", err)
	}
}
