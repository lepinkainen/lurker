package irc

import "testing"

func TestAvatarTrackerSetClearGetHas(t *testing.T) {
	a := newAvatarTracker()

	if a.has("alice") {
		t.Fatal("alice has an avatar before set")
	}
	if !a.set("alice", "https://example.com/a.png") {
		t.Fatal("first set should report a change")
	}
	if a.set("alice", "https://example.com/a.png") {
		t.Fatal("repeated identical set should report no change")
	}
	if url, ok := a.get("ALICE"); !ok || url != "https://example.com/a.png" {
		t.Fatalf("get(ALICE) = (%q, %v), want (https://example.com/a.png, true) — case-fold mismatch", url, ok)
	}
	if !a.has("Alice") {
		t.Fatal("has should be case-insensitive")
	}
	if !a.clear("alice") {
		t.Fatal("clear should report the entry was removed")
	}
	if a.clear("alice") {
		t.Fatal("second clear should report no-op")
	}
	if a.has("alice") {
		t.Fatal("alice still has an avatar after clear")
	}
}

func TestAvatarTrackerSetClearGetIgnoreEmptyAndNil(t *testing.T) {
	var nilTracker *avatarTracker
	if nilTracker.set("alice", "url") {
		t.Fatal("nil tracker set should report false")
	}
	if nilTracker.clear("alice") {
		t.Fatal("nil tracker clear should report false")
	}
	if nilTracker.has("alice") {
		t.Fatal("nil tracker has should report false")
	}
	if url, ok := nilTracker.get("alice"); ok || url != "" {
		t.Fatalf("nil tracker get = (%q, %v), want (\"\", false)", url, ok)
	}

	a := newAvatarTracker()
	if a.set("", "url") {
		t.Fatal("set with empty nick should report false")
	}
	if a.set("alice", "") {
		t.Fatal("set with empty url should report false")
	}
}

func TestAvatarTrackerRename(t *testing.T) {
	a := newAvatarTracker()

	// No-op cases must return false and change nothing.
	if a.rename("", "bob") {
		t.Fatal("rename with empty oldNick should report false")
	}
	if a.rename("alice", "") {
		t.Fatal("rename with empty newNick should report false")
	}
	if a.rename("alice", "ALICE") {
		t.Fatal("rename to same case-folded nick should report false (no-op)")
	}
	if a.rename("alice", "bob") {
		t.Fatal("rename of an untracked nick should report false")
	}

	a.set("alice", "https://example.com/a.png")
	if !a.rename("alice", "bob") {
		t.Fatal("rename of a tracked nick should report true")
	}
	if a.has("alice") {
		t.Fatal("old nick should no longer have an avatar after rename")
	}
	url, ok := a.get("bob")
	if !ok || url != "https://example.com/a.png" {
		t.Fatalf("get(bob) after rename = (%q, %v), want (https://example.com/a.png, true)", url, ok)
	}

	// Renaming again from the now-empty old nick is a no-op.
	if a.rename("alice", "carol") {
		t.Fatal("rename of an already-vacated nick should report false")
	}
}

func TestAvatarTrackerNilRenameDoesNotPanic(t *testing.T) {
	var nilTracker *avatarTracker
	if nilTracker.rename("alice", "bob") {
		t.Fatal("nil tracker rename should report false")
	}
}
