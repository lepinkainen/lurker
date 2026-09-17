package irc

import (
	"testing"
	"time"

	"github.com/lepinkainen/lurker/hub"
	"github.com/lrstanley/girc"
)

// metadataTestFixture wires a handler fixture with a real avatarTracker —
// newTestHandlerFixture leaves avatars nil (unused by most handler tests),
// but the metadata handlers need one to record into.
func metadataTestFixture(t *testing.T) (*testHandlerFixture, <-chan any, func()) {
	t.Helper()
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	f.Handler.avatars = newAvatarTracker()
	events, _, unsub := h.Subscribe(16)
	return f, events, unsub
}

func avatarEvents(evs []any) []*AvatarEvent {
	var out []*AvatarEvent
	for _, ev := range evs {
		if ae, ok := ev.(*AvatarEvent); ok {
			out = append(out, ae)
		}
	}
	return out
}

func TestOnMetadataSetsAvatarAndPublishesEvent(t *testing.T) {
	f, events, unsub := metadataTestFixture(t)
	defer unsub()

	f.Handler.onMetadata(nil, mustEvent(t, ":irc.example METADATA alice avatar * :https://example.com/a.png"))

	if url, ok := f.Handler.avatars.get("alice"); !ok || url != "https://example.com/a.png" {
		t.Fatalf("avatars.get(alice) = (%q, %v), want (https://example.com/a.png, true)", url, ok)
	}
	got := avatarEvents(drainEvents(events))
	if len(got) != 1 {
		t.Fatalf("published %d AvatarEvents, want 1: %+v", len(got), got)
	}
	if got[0].Nick != "alice" || !got[0].HasAvatar {
		t.Fatalf("event = %+v, want nick=alice has_avatar=true", got[0])
	}
}

func TestOnKeyValueNumericSetsAvatarAndPublishesEvent(t *testing.T) {
	f, events, unsub := metadataTestFixture(t)
	defer unsub()

	// RPL_KEYVALUE (761): "<client> <target> <key> <visibility> :<value>".
	f.Handler.onKeyValue(nil, mustEvent(t, ":irc.example 761 tester alice avatar * :https://example.com/a.png"))

	if url, ok := f.Handler.avatars.get("alice"); !ok || url != "https://example.com/a.png" {
		t.Fatalf("avatars.get(alice) = (%q, %v), want (https://example.com/a.png, true)", url, ok)
	}
	got := avatarEvents(drainEvents(events))
	if len(got) != 1 || got[0].Nick != "alice" || !got[0].HasAvatar {
		t.Fatalf("events = %+v, want one nick=alice has_avatar=true", got)
	}
}

func TestOnKeyNotSetClearsAvatarAndPublishesEvent(t *testing.T) {
	f, events, unsub := metadataTestFixture(t)
	defer unsub()

	f.Handler.avatars.set("alice", "https://example.com/a.png")

	// RPL_KEYNOTSET (766): "<client> <target> <key> :<message>".
	f.Handler.onKeyNotSet(nil, mustEvent(t, ":irc.example 766 tester alice avatar :no such key"))

	if f.Handler.avatars.has("alice") {
		t.Fatal("avatar still tracked after RPL_KEYNOTSET")
	}
	got := avatarEvents(drainEvents(events))
	if len(got) != 1 || got[0].Nick != "alice" || got[0].HasAvatar {
		t.Fatalf("events = %+v, want one nick=alice has_avatar=false", got)
	}
}

func TestOnMetadataEmptyValueClearsAvatar(t *testing.T) {
	f, events, unsub := metadataTestFixture(t)
	defer unsub()

	f.Handler.avatars.set("alice", "https://example.com/a.png")
	drainEvents(events)

	f.Handler.onMetadata(nil, mustEvent(t, ":irc.example METADATA alice avatar * :"))

	if f.Handler.avatars.has("alice") {
		t.Fatal("avatar still tracked after empty-value METADATA")
	}
	got := avatarEvents(drainEvents(events))
	if len(got) != 1 || got[0].Nick != "alice" || got[0].HasAvatar {
		t.Fatalf("events = %+v, want one nick=alice has_avatar=false", got)
	}
}

func TestOnMetadataIgnoresNonAvatarKey(t *testing.T) {
	f, events, unsub := metadataTestFixture(t)
	defer unsub()

	f.Handler.onMetadata(nil, mustEvent(t, ":irc.example METADATA alice color * :blue"))

	if f.Handler.avatars.has("alice") {
		t.Fatal("non-avatar key must not populate the avatar tracker")
	}
	if got := avatarEvents(drainEvents(events)); len(got) != 0 {
		t.Fatalf("events = %+v, want none for a non-avatar key", got)
	}
}

func TestOnMetadataIgnoresChannelTarget(t *testing.T) {
	f, events, unsub := metadataTestFixture(t)
	defer unsub()

	f.Handler.onMetadata(nil, mustEvent(t, ":irc.example METADATA #chan avatar * :https://example.com/c.png"))

	if f.Handler.avatars.has("#chan") {
		t.Fatal("channel target must not populate the avatar tracker")
	}
	if got := avatarEvents(drainEvents(events)); len(got) != 0 {
		t.Fatalf("events = %+v, want none for a channel target", got)
	}
}

func TestOnMetadataRepeatedIdenticalSetPublishesOnce(t *testing.T) {
	f, events, unsub := metadataTestFixture(t)
	defer unsub()

	raw := ":irc.example METADATA alice avatar * :https://example.com/a.png"
	f.Handler.onMetadata(nil, mustEvent(t, raw))
	f.Handler.onMetadata(nil, mustEvent(t, raw))

	got := avatarEvents(drainEvents(events))
	if len(got) != 1 {
		t.Fatalf("published %d AvatarEvents for a repeated identical set, want 1: %+v", len(got), got)
	}
}

func TestParseSyncLater(t *testing.T) {
	tests := []struct {
		name       string
		params     []string
		wantTarget string
		wantDelay  time.Duration
		wantOK     bool
	}{
		{
			name:       "no retry after",
			params:     []string{"lurker", "#big"},
			wantTarget: "#big",
			wantDelay:  0,
			wantOK:     true,
		},
		{
			name:       "numeric retry after",
			params:     []string{"lurker", "#big", "5"},
			wantTarget: "#big",
			wantDelay:  5 * time.Second,
			wantOK:     true,
		},
		{
			name:       "retry after clamped to max",
			params:     []string{"lurker", "#big", "99999"},
			wantTarget: "#big",
			wantDelay:  maxMetadataSyncRetry,
			wantOK:     true,
		},
		{
			name:   "missing target",
			params: []string{"lurker"},
			wantOK: false,
		},
		{
			name:       "empty target",
			params:     []string{"lurker", ""},
			wantTarget: "",
			wantOK:     false,
		},
		{
			name:       "non-numeric retry after sends immediately",
			params:     []string{"lurker", "#big", "soon"},
			wantTarget: "#big",
			wantDelay:  0,
			wantOK:     true,
		},
		{
			name:       "zero retry after sends immediately",
			params:     []string{"lurker", "#big", "0"},
			wantTarget: "#big",
			wantDelay:  0,
			wantOK:     true,
		},
		{
			name:       "negative retry after sends immediately",
			params:     []string{"lurker", "#big", "-5"},
			wantTarget: "#big",
			wantDelay:  0,
			wantOK:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, delay, ok := parseSyncLater(girc.Event{Command: rplMetadataSyncLater, Params: tt.params})
			if target != tt.wantTarget || delay != tt.wantDelay || ok != tt.wantOK {
				t.Fatalf("parseSyncLater(%v) = (%q, %v, %v), want (%q, %v, %v)",
					tt.params, target, delay, ok, tt.wantTarget, tt.wantDelay, tt.wantOK)
			}
		})
	}
}

// TestOnMetadataSyncLaterImmediateSendsRaw exercises the handler's thin
// wrapper for the delay=0 path (no RetryAfter): it should attempt
// "METADATA <target> SYNC" on the client synchronously. girc writes SendRaw
// straight to the connection, so a nil/disconnected client just errors —
// there's no socket to assert the line against, but this still proves the
// handler reaches the send call without panicking on a real girc.Client.
func TestOnMetadataSyncLaterImmediateSendsRaw(t *testing.T) {
	f, _, unsub := metadataTestFixture(t)
	defer unsub()

	c := girc.New(girc.Config{Server: "irc.example.invalid", Nick: "lurker"})
	// No network connection is made; SendRaw will error, which is exactly
	// the "harmless on a stale/disconnected client" path the handler is
	// documented to tolerate.
	f.Handler.onMetadataSyncLater(c, mustEvent(t, ":irc.example 774 lurker #big"))
}
