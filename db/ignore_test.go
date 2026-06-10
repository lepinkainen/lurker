package db

import (
	"testing"
)

func TestIgnoreCRUDRoundtrip(t *testing.T) {
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
	other, err := ms.UpsertNetwork(ctx, Network{Name: "OFTC", Host: "irc.oftc.net", Port: 6697, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}

	if err := CreateIgnore(ctx, ms.Control, n.ID, "spammer!*@*"); err != nil {
		t.Fatal(err)
	}
	if err := CreateIgnore(ctx, ms.Control, n.ID, "troll!*@*"); err != nil {
		t.Fatal(err)
	}

	masks, err := ListIgnores(ctx, ms.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(masks) != 2 {
		t.Fatalf("masks = %v, want 2 entries", masks)
	}

	// Ignores are scoped per network.
	otherMasks, err := ListIgnores(ctx, ms.Control, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherMasks) != 0 {
		t.Fatalf("other network masks = %v, want none", otherMasks)
	}

	if err := DeleteIgnore(ctx, ms.Control, n.ID, "spammer!*@*"); err != nil {
		t.Fatal(err)
	}
	masks, err = ListIgnores(ctx, ms.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(masks) != 1 || masks[0] != "troll!*@*" {
		t.Fatalf("masks after delete = %v, want [troll!*@*]", masks)
	}

	// Deleting a non-existent mask is a no-op, not an error.
	if err := DeleteIgnore(ctx, ms.Control, n.ID, "ghost!*@*"); err != nil {
		t.Fatalf("DeleteIgnore(missing) = %v, want nil", err)
	}
}
