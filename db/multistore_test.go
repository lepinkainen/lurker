package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMultiStoreOpensDistinctNetworkDBs(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()

	ms, err := OpenMultiStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	first, err := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ms.UpsertNetwork(ctx, Network{Name: "OFTC-test", Host: "irc.oftc.net", Port: 6697, TLS: true, Nick: "b"})
	if err != nil {
		t.Fatal(err)
	}

	if got := ms.NetworkIDs(); len(got) != 2 {
		t.Fatalf("network ids len = %d, want 2", len(got))
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
}
