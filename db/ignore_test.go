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

	if err := CreateIgnore(ctx, ms.Control, n.ID, "spammer!*@*", IgnoreLevelHide); err != nil {
		t.Fatal(err)
	}
	if err := CreateIgnore(ctx, ms.Control, n.ID, "troll!*@*", IgnoreLevelHide); err != nil {
		t.Fatal(err)
	}

	entries, err := ListIgnores(ctx, ms.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want 2 entries", entries)
	}

	// Ignores are scoped per network.
	otherEntries, err := ListIgnores(ctx, ms.Control, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherEntries) != 0 {
		t.Fatalf("other network entries = %v, want none", otherEntries)
	}

	if err := DeleteIgnore(ctx, ms.Control, n.ID, "spammer!*@*"); err != nil {
		t.Fatal(err)
	}
	entries, err = ListIgnores(ctx, ms.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Mask != "troll!*@*" || entries[0].Level != IgnoreLevelHide {
		t.Fatalf("entries after delete = %v, want [{troll!*@* hide}]", entries)
	}

	// Deleting a non-existent mask is a no-op, not an error.
	if err := DeleteIgnore(ctx, ms.Control, n.ID, "ghost!*@*"); err != nil {
		t.Fatalf("DeleteIgnore(missing) = %v, want nil", err)
	}
}

// TestCreateIgnoreUpsertsLevel verifies re-adding an existing mask promotes
// or demotes its level instead of erroring or leaving a duplicate row.
func TestCreateIgnoreUpsertsLevel(t *testing.T) {
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

	if err := CreateIgnore(ctx, ms.Control, n.ID, "weatherbot!*@*", IgnoreLevelHide); err != nil {
		t.Fatal(err)
	}
	if err := CreateIgnore(ctx, ms.Control, n.ID, "weatherbot!*@*", IgnoreLevelMute); err != nil {
		t.Fatal(err)
	}

	entries, err := ListIgnores(ctx, ms.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Mask != "weatherbot!*@*" || entries[0].Level != IgnoreLevelMute {
		t.Fatalf("entries = %v, want a single mute-level entry (upsert, not duplicate)", entries)
	}
}

func TestIgnoreLevelForPrecedence(t *testing.T) {
	// Matching is against the bare nick (mask glob-matched case-insensitively
	// against the nick only, not a full nick!user@host mask).
	entries := []IgnoreEntry{
		{Mask: "weatherbot", Level: IgnoreLevelMute},
		{Mask: "spam*", Level: IgnoreLevelHide},
	}

	// Matches only the mute mask.
	if got := IgnoreLevelFor(entries, "WeatherBot"); got != IgnoreLevelMute {
		t.Fatalf("IgnoreLevelFor(weatherbot) = %q, want mute", got)
	}
	// No match at all.
	if got := IgnoreLevelFor(entries, "alice"); got != "" {
		t.Fatalf("IgnoreLevelFor(alice) = %q, want empty", got)
	}

	// A nick matching both a hide and a mute mask: hide wins.
	both := []IgnoreEntry{
		{Mask: "bot*", Level: IgnoreLevelMute},
		{Mask: "bot*", Level: IgnoreLevelHide},
	}
	if got := IgnoreLevelFor(both, "botty"); got != IgnoreLevelHide {
		t.Fatalf("IgnoreLevelFor(botty) with overlapping masks = %q, want hide", got)
	}
	// Order independence: hide still wins when listed first.
	bothReversed := []IgnoreEntry{
		{Mask: "bot*", Level: IgnoreLevelHide},
		{Mask: "bot*", Level: IgnoreLevelMute},
	}
	if got := IgnoreLevelFor(bothReversed, "botty"); got != IgnoreLevelHide {
		t.Fatalf("IgnoreLevelFor(botty) reversed order = %q, want hide", got)
	}
}
