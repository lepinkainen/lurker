package irc

import (
	"testing"

	"github.com/lepinkainen/lurker/hub"
)

func TestNickPublishesPresenceAndUpdatesSelfNick(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)
	var currentNick string
	f.Handler.connectedHook = func(nick string) { currentNick = nick }

	f.Handler.onNick(client, mustEvent(t, ":tester!u@h NICK newtester"))

	if currentNick != "newtester" {
		t.Fatalf("current nick = %q, want newtester", currentNick)
	}
	if !hasPresence(events, "tester", "nick") {
		t.Fatal("missing nick presence event")
	}
	msg := lastHandlerMessage(t, f)
	if msg.Kind != "nick" || msg.Target != "newtester" {
		t.Fatalf("message = %+v", msg)
	}
}

// Untracked nick: away/back fall back to the status buffer.
func TestAwayStoresAwayAndBack(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onAway(nil, mustEvent(t, ":alice!u@h AWAY :lunch"))
	f.Handler.onAway(nil, mustEvent(t, ":alice!u@h AWAY"))

	rows, err := f.LogStore.DB.Query(`SELECT kind, target, content FROM messages ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			t.Fatalf("close rows: %v", cerr)
		}
	}()
	var got []handlerMessage
	for rows.Next() {
		var msg handlerMessage
		if err := rows.Scan(&msg.Kind, &msg.Target, &msg.Content); err != nil {
			t.Fatal(err)
		}
		got = append(got, msg)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("messages = %+v, want 2", got)
	}
	if got[0].Kind != "away" || got[0].Target != "alice" || got[0].Content != "lunch" {
		t.Fatalf("away message = %+v", got[0])
	}
	if got[1].Kind != "back" || got[1].Target != "alice" || got[1].Content != "" {
		t.Fatalf("back message = %+v", got[1])
	}
}

// Away-notify fan-out: a tracked user's away/back events land in every
// shared channel, not the status buffer (which they used to flood).
func TestAwayFansOutToSharedChannels(t *testing.T) {
	f := newTestHandlerFixture(t)
	f.Handler.userChannels.addUser("alice", "#a")
	f.Handler.userChannels.addUser("alice", "#b")

	f.Handler.onAway(nil, mustEvent(t, ":alice!u@h AWAY :lunch"))
	f.Handler.onAway(nil, mustEvent(t, ":alice!u@h AWAY"))

	rows, err := f.LogStore.DB.Query(`
		SELECT b.name, b.kind, m.kind, m.target, m.content
		FROM messages m JOIN buffers b ON b.id = m.buffer_id
		ORDER BY m.id, b.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type row struct{ Buf, BufKind, Kind, Target, Content string }
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Buf, &r.BufKind, &r.Kind, &r.Target, &r.Content); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 channel rows (away+back × #a,#b), got %d: %+v", len(got), got)
	}
	for i, r := range got {
		wantKind := "away"
		wantContent := "lunch"
		if i >= 2 {
			wantKind = "back"
			wantContent = ""
		}
		if r.BufKind != "channel" {
			t.Errorf("row %d buffer kind=%q, want channel", i, r.BufKind)
		}
		if r.Kind != wantKind || r.Target != "alice" || r.Content != wantContent {
			t.Errorf("row %d = %+v, want kind=%q target=alice content=%q", i, r, wantKind, wantContent)
		}
	}
}

// Nick fan-out: rename lands in shared channels and userChannels tracking
// follows the new nick.
func TestNickFansOutToSharedChannels(t *testing.T) {
	f := newTestHandlerFixture(t)
	f.Handler.userChannels.addUser("alice", "#a")
	f.Handler.userChannels.addUser("alice", "#b")

	f.Handler.onNick(nil, mustEvent(t, ":alice!u@h NICK bob"))

	rows, err := f.LogStore.DB.Query(`
		SELECT b.name, b.kind, m.kind, m.target
		FROM messages m JOIN buffers b ON b.id = m.buffer_id
		ORDER BY b.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type row struct{ Buf, BufKind, Kind, Target string }
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Buf, &r.BufKind, &r.Kind, &r.Target); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 channel rows, got %d: %+v", len(got), got)
	}
	for _, r := range got {
		if r.BufKind != "channel" || r.Kind != "nick" || r.Target != "bob" {
			t.Errorf("row = %+v, want channel nick→bob", r)
		}
	}
	if channels, tracked := f.Handler.userChannels.channelsFor("bob"); !tracked || len(channels) != 2 {
		t.Fatalf("bob channels = %v tracked=%v, want [#a #b] true", channels, tracked)
	}
}

func TestAccountStarStoresEmptyAccount(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onAccount(nil, mustEvent(t, ":alice!u@h ACCOUNT *"))

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "account" || msg.Target != "alice" || msg.Content != "" {
		t.Fatalf("message = %+v", msg)
	}
}

// Regression for the per-channel QUIT fan-out: a remote user that we
// share two channels with must produce one quit row in each channel and
// no status-buffer row. Status-only quits are reserved for unknown nicks.
func TestQuitFansOutToSharedChannels(t *testing.T) {
	f := newTestHandlerFixture(t)
	// Seed userChannels: alice is in #a and #b.
	f.Handler.userChannels.addUser("alice", "#a")
	f.Handler.userChannels.addUser("alice", "#b")

	f.Handler.onQuit(nil, mustEvent(t, ":alice!u@h QUIT :irc.cc.tut.fi irc2.inet.fi"))

	rows, err := f.LogStore.DB.Query(`
		SELECT b.name, b.kind, m.kind, m.content
		FROM messages m JOIN buffers b ON b.id = m.buffer_id
		ORDER BY b.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type row struct{ Buf, BufKind, Kind, Content string }
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Buf, &r.BufKind, &r.Kind, &r.Content); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 channel rows, got %d: %+v", len(got), got)
	}
	for _, r := range got {
		if r.Kind != "quit" {
			t.Errorf("row kind=%q, want quit", r.Kind)
		}
		if r.BufKind != "channel" {
			t.Errorf("row buffer kind=%q, want channel (status fallback indicates fan-out missed)", r.BufKind)
		}
		if r.Content != "irc.cc.tut.fi irc2.inet.fi" {
			t.Errorf("row content=%q, want netsplit reason preserved", r.Content)
		}
	}
}

// Regression for the userChannels store-missing branch: when a quit
// arrives for a nick we don't know about, the row must still be written
// to the status buffer so netadmins keep cross-network visibility.
func TestQuitForUntrackedNickFallsBackToStatus(t *testing.T) {
	f := newTestHandlerFixture(t)
	f.Handler.onQuit(nil, mustEvent(t, ":stranger!u@h QUIT :Ping timeout"))

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "quit" || msg.BufferKind != "status" || msg.Content != "Ping timeout" {
		t.Fatalf("status fallback row = %+v", msg)
	}
}

func TestChghostStoresIdentAndHost(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onChghost(nil, mustEvent(t, ":alice!u@old.host CHGHOST newident new.host"))

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "chghost" || msg.Target != "alice" || msg.Content != "newident new.host" {
		t.Fatalf("message = %+v", msg)
	}
}

// Bug 1 regression: a departing user's tracked avatar must be cleared, and
// clients told, so a nick reuse never inherits the previous holder's
// avatar.
func TestQuitClearsTrackedAvatarAndPublishesEvent(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	f.Handler.avatars = newAvatarTracker()
	f.Handler.avatars.set("alice", "https://example.com/a.png")
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onQuit(nil, mustEvent(t, ":alice!u@h QUIT :bye"))

	if f.Handler.avatars.has("alice") {
		t.Fatal("avatar still tracked for alice after QUIT")
	}
	got := avatarEvents(drainEvents(events))
	if len(got) != 1 || got[0].Nick != "alice" || got[0].HasAvatar {
		t.Fatalf("events = %+v, want one nick=alice has_avatar=false", got)
	}
}

// A quitting IRCCloud user has no tracker entry (that fallback is derived,
// never stored) but nick-keyed web clients still need the clear so a nick
// reuse doesn't inherit the derived avatar either.
func TestQuitEmitsClearForIRCCloudDerivedAvatar(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	f.Handler.avatars = newAvatarTracker()
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onQuit(nil, mustEvent(t, ":alice!uid123@id.irccloud.com QUIT :bye"))

	got := avatarEvents(drainEvents(events))
	if len(got) != 1 || got[0].Nick != "alice" || got[0].HasAvatar {
		t.Fatalf("events = %+v, want one nick=alice has_avatar=false", got)
	}
}

// A quit for a user with neither a tracked nor a derivable avatar must not
// publish a spurious AvatarEvent.
func TestQuitWithNoAvatarPublishesNoAvatarEvent(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	f.Handler.avatars = newAvatarTracker()
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onQuit(nil, mustEvent(t, ":alice!u@h QUIT :bye"))

	if got := avatarEvents(drainEvents(events)); len(got) != 0 {
		t.Fatalf("events = %+v, want none", got)
	}
}

// Bug 2 regression: a nick change must move the tracked avatar and tell
// nick-keyed web clients about both the old (cleared) and new (set) nick,
// not just rewrite the tracker silently.
func TestNickMovesTrackedAvatarAndPublishesEvents(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	f.Handler.avatars = newAvatarTracker()
	f.Handler.avatars.set("alice", "https://example.com/a.png")
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onNick(nil, mustEvent(t, ":alice!u@h NICK bob"))

	if f.Handler.avatars.has("alice") {
		t.Fatal("old nick still has an avatar after rename")
	}
	url, ok := f.Handler.avatars.get("bob")
	if !ok || url != "https://example.com/a.png" {
		t.Fatalf("avatars.get(bob) = (%q, %v), want (https://example.com/a.png, true)", url, ok)
	}
	got := avatarEvents(drainEvents(events))
	if len(got) != 2 {
		t.Fatalf("published %d AvatarEvents, want 2 (clear old, set new): %+v", len(got), got)
	}
	if got[0].Nick != "alice" || got[0].HasAvatar {
		t.Fatalf("first event = %+v, want nick=alice has_avatar=false", got[0])
	}
	if got[1].Nick != "bob" || !got[1].HasAvatar {
		t.Fatalf("second event = %+v, want nick=bob has_avatar=true", got[1])
	}
}

// An IRCCloud-derived avatar isn't in the tracker, so a rename must still
// emit clear(old)+set(new) purely from the (unchanged) hostmask.
func TestNickEmitsAvatarEventsForIRCCloudDerivedAvatar(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	f.Handler.avatars = newAvatarTracker()
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onNick(nil, mustEvent(t, ":alice!uid123@id.irccloud.com NICK bob"))

	got := avatarEvents(drainEvents(events))
	if len(got) != 2 {
		t.Fatalf("published %d AvatarEvents, want 2 (clear old, set new): %+v", len(got), got)
	}
	if got[0].Nick != "alice" || got[0].HasAvatar {
		t.Fatalf("first event = %+v, want nick=alice has_avatar=false", got[0])
	}
	if got[1].Nick != "bob" || !got[1].HasAvatar {
		t.Fatalf("second event = %+v, want nick=bob has_avatar=true", got[1])
	}
}

// A nick change for a user with neither a tracked nor a derivable avatar
// must not publish spurious AvatarEvents.
func TestNickWithNoAvatarPublishesNoAvatarEvent(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	f.Handler.avatars = newAvatarTracker()
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onNick(nil, mustEvent(t, ":alice!u@h NICK bob"))

	if got := avatarEvents(drainEvents(events)); len(got) != 0 {
		t.Fatalf("events = %+v, want none", got)
	}
}
