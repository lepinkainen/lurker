package db

import (
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestDeleteNetworkPreservesLogDBFile(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()
	ms, err := OpenMultiStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, err := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	logPath, err := LogDBPath(dataDir, n.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log db missing before delete: %v", err)
	}

	if err := ms.DeleteNetwork(ctx, n.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := GetNetwork(ctx, ms.Control, n.ID); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("GetNetwork after delete = %v, want ErrNetworkNotFound", err)
	}
	if _, err := ms.LogStore(n.ID); err == nil {
		t.Fatal("LogStore still open after DeleteNetwork")
	}
	// Invariant: deleting a network must NOT delete its log DB file.
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log db file deleted with network: %v", err)
	}
}

func TestDeleteNetworkUnknownIDFails(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	if err := ms.DeleteNetwork(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("DeleteNetwork(unknown) = %v, want ErrNetworkNotFound", err)
	}
}

func TestCloseNetworkClosesLogStoreAndIsIdempotent(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, err := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ms.CloseNetwork(n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.LogStore(n.ID); err == nil {
		t.Fatal("LogStore should be unavailable after CloseNetwork")
	}
	// Second close (and unknown ID) are no-ops.
	if err := ms.CloseNetwork(n.ID); err != nil {
		t.Fatalf("repeat CloseNetwork = %v, want nil", err)
	}
	if err := ms.CloseNetwork(uuid.Must(uuid.NewV7())); err != nil {
		t.Fatalf("CloseNetwork(unknown) = %v, want nil", err)
	}
}

func TestUpdateNetworkPreservesUnsetFields(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	orig, err := ms.UpsertNetwork(ctx, Network{
		Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true,
		Nick: "tester", Realname: "Test User",
		SASLUser: "tester", SASLPass: "hunter2",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := UpdateNetwork(ctx, ms.Control, orig.ID, Network{Host: "irc.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Host != "irc.example.org" {
		t.Fatalf("host = %q, want irc.example.org", updated.Host)
	}
	if updated.Name != "Libera" || updated.Nick != "tester" || updated.Realname != "Test User" {
		t.Fatalf("unset fields not preserved: %+v", updated)
	}
	if updated.SASLUser != "tester" || updated.SASLPass != "hunter2" {
		t.Fatalf("SASL credentials not preserved: user=%q pass=%q", updated.SASLUser, updated.SASLPass)
	}

	if _, err := UpdateNetwork(ctx, ms.Control, uuid.Must(uuid.NewV7()), Network{Host: "x"}); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("UpdateNetwork(unknown) = %v, want ErrNetworkNotFound", err)
	}
}

func TestUpdateNetworkSASLMergeSemantics(t *testing.T) {
	ctx := t.Context()

	newStore := func(t *testing.T) *MultiStore {
		t.Helper()
		ms, err := OpenMultiStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ms.Close() })
		return ms
	}
	seed := func(t *testing.T, ms *MultiStore) Network {
		t.Helper()
		orig, err := ms.UpsertNetwork(ctx, Network{
			Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true,
			Nick: "tester", SASLUser: "tester", SASLPass: "hunter2",
		})
		if err != nil {
			t.Fatal(err)
		}
		return orig
	}

	// (1) blank user + blank pass keeps both existing (partial-update safety,
	// e.g. connectedHook passing only Nick).
	t.Run("blank both keeps existing", func(t *testing.T) {
		ms := newStore(t)
		orig := seed(t, ms)
		got, err := UpdateNetwork(ctx, ms.Control, orig.ID, Network{Nick: "renamed"})
		if err != nil {
			t.Fatal(err)
		}
		if got.SASLUser != "tester" || got.SASLPass != "hunter2" {
			t.Fatalf("blank both: user=%q pass=%q, want tester/hunter2", got.SASLUser, got.SASLPass)
		}
	})

	// (2) new pass + blank user keeps existing user and saves the new pass.
	t.Run("new pass blank user", func(t *testing.T) {
		ms := newStore(t)
		orig := seed(t, ms)
		got, err := UpdateNetwork(ctx, ms.Control, orig.ID, Network{SASLPass: "newsecret"})
		if err != nil {
			t.Fatal(err)
		}
		if got.SASLUser != "tester" || got.SASLPass != "newsecret" {
			t.Fatalf("new pass blank user: user=%q pass=%q, want tester/newsecret", got.SASLUser, got.SASLPass)
		}
	})

	// (3) new user + blank pass keeps existing pass.
	t.Run("new user blank pass", func(t *testing.T) {
		ms := newStore(t)
		orig := seed(t, ms)
		got, err := UpdateNetwork(ctx, ms.Control, orig.ID, Network{SASLUser: "newuser"})
		if err != nil {
			t.Fatal(err)
		}
		if got.SASLUser != "newuser" || got.SASLPass != "hunter2" {
			t.Fatalf("new user blank pass: user=%q pass=%q, want newuser/hunter2", got.SASLUser, got.SASLPass)
		}
	})
}

func TestSetNetworkDisabledRoundtrip(t *testing.T) {
	ctx := t.Context()
	ms, err := OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()

	n, err := ms.UpsertNetwork(ctx, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}

	if err := SetNetworkDisabled(ctx, ms.Control, n.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := GetNetwork(ctx, ms.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Disabled {
		t.Fatal("network not marked disabled")
	}

	if err := SetNetworkDisabled(ctx, ms.Control, n.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err = GetNetwork(ctx, ms.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disabled {
		t.Fatal("network still disabled after re-enable")
	}

	if err := SetNetworkDisabled(ctx, ms.Control, uuid.Must(uuid.NewV7()), true); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("SetNetworkDisabled(unknown) = %v, want ErrNetworkNotFound", err)
	}
}
