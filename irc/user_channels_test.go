package irc

import (
	"reflect"
	"testing"
)

func TestUserChannels_AddAndDrop(t *testing.T) {
	u := newUserChannels()
	u.addUser("Alice", "#a")
	u.addUser("Alice", "#b")
	u.addUser("Bob", "#a")

	channels, tracked := u.dropUser("alice") // case-insensitive
	if !tracked {
		t.Fatal("dropUser(alice) tracked=false")
	}
	if !reflect.DeepEqual(channels, []string{"#a", "#b"}) {
		t.Errorf("dropped channels = %v, want [#a #b]", channels)
	}
	// Reverse index must clear alice but keep bob.
	if _, stillThere := u.byChannel["#a"]["alice"]; stillThere {
		t.Errorf("byChannel[#a] still has alice after dropUser")
	}
	if len(u.byChannel["#a"]) != 1 {
		t.Errorf("byChannel[#a] = %v, want 1 entry (bob)", u.byChannel["#a"])
	}
}

func TestUserChannels_DropDistinguishesTracked(t *testing.T) {
	u := newUserChannels()
	_, tracked := u.dropUser("unknown")
	if tracked {
		t.Errorf("dropUser(unknown) tracked=true, want false")
	}

	var nilStore *userChannels
	_, tracked = nilStore.dropUser("alice")
	if tracked {
		t.Errorf("dropUser on nil store returned tracked=true; want false (distinguishes init bug)")
	}
}

// Regression: RFC1459 casemap must equate Foo[bar] with foo{bar} for
// nick keys. Plain strings.ToLower used to miss this, so a netsplit user
// with `[` in the name would never rematch on rejoin.
func TestUserChannels_RFC1459Casemap(t *testing.T) {
	u := newUserChannels()
	u.addUser("Foo[bar]", "#a")
	channels, tracked := u.dropUser("foo{bar}")
	if !tracked || len(channels) != 1 || channels[0] != "#a" {
		t.Errorf("RFC1459 casemap miss: tracked=%v channels=%v", tracked, channels)
	}
}

func TestUserChannels_RenameUser(t *testing.T) {
	u := newUserChannels()
	u.addUser("alice", "#a")
	u.renameUser("alice", "Alice2")
	channels, tracked := u.dropUser("alice2")
	if !tracked || channels[0] != "#a" {
		t.Errorf("renameUser dropped tracking: tracked=%v channels=%v", tracked, channels)
	}
}

func TestUserChannels_DropChannel(t *testing.T) {
	u := newUserChannels()
	u.addUser("alice", "#a")
	u.addUser("bob", "#a")
	u.addUser("alice", "#b")
	u.dropChannel("#a")
	if _, ok := u.byChannel["#a"]; ok {
		t.Errorf("dropChannel(#a) left reverse index entry")
	}
	// alice still in #b
	channels, _ := u.dropUser("alice")
	if !reflect.DeepEqual(channels, []string{"#b"}) {
		t.Errorf("alice channels after dropChannel(#a) = %v, want [#b]", channels)
	}
	// bob no longer tracked anywhere
	if _, tracked := u.dropUser("bob"); tracked {
		t.Errorf("bob still tracked after dropChannel(#a) removed his only channel")
	}
}

func TestUserChannels_SyncChannelPurgesStale(t *testing.T) {
	u := newUserChannels()
	u.addUser("alice", "#a")
	u.addUser("bob", "#a")
	u.addUser("carol", "#a")
	// New NAMES list: alice + dave (no bob, no carol).
	u.syncChannel("#a", []string{"alice", "dave"})

	channelsAlice, _ := u.dropUser("alice")
	if channelsAlice[0] != "#a" {
		t.Errorf("alice should still be in #a, got %v", channelsAlice)
	}
	channelsDave, daveTracked := u.dropUser("dave")
	if !daveTracked || channelsDave[0] != "#a" {
		t.Errorf("dave should be in #a after sync, tracked=%v channels=%v", daveTracked, channelsDave)
	}
	if _, bobTracked := u.dropUser("bob"); bobTracked {
		t.Errorf("bob should be purged after sync")
	}
}
