package irc

import (
	"testing"

	"github.com/lepinkainen/lurker/hub"
)

func TestRPLListPublishesAccumulatedEntriesAndClears(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onRPLList(nil, mustEvent(t, ":irc.example 322 tester #go 42 :Go talk"))
	f.Handler.onRPLList(nil, mustEvent(t, ":irc.example 322 tester #rust 7 :Rust talk"))
	f.Handler.onRPLListEnd(nil, mustEvent(t, ":irc.example 323 tester :End of LIST"))

	var got *ChannelListEvent
	for _, ev := range drainEvents(events) {
		if list, ok := ev.(*ChannelListEvent); ok {
			got = list
		}
	}
	if got == nil {
		t.Fatal("missing channel_list event")
	}
	if !got.Done || len(got.Entries) != 2 {
		t.Fatalf("channel list = %+v, want done with 2 entries", got)
	}
	if got.Entries[0] != (ChannelListEntry{Name: "#go", Count: 42, Topic: "Go talk"}) {
		t.Fatalf("first entry = %+v", got.Entries[0])
	}
	if got.Entries[1] != (ChannelListEntry{Name: "#rust", Count: 7, Topic: "Rust talk"}) {
		t.Fatalf("second entry = %+v", got.Entries[1])
	}
	if len(f.Handler.listEntries) != 0 {
		t.Fatalf("list entries not cleared: %+v", f.Handler.listEntries)
	}
}

func TestRPLListEndWithoutHubClearsAccumulator(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onRPLList(nil, mustEvent(t, ":irc.example 322 tester #go 42 :Go talk"))
	f.Handler.onRPLListEnd(nil, mustEvent(t, ":irc.example 323 tester :End of LIST"))

	if len(f.Handler.listEntries) != 0 {
		t.Fatalf("list entries not cleared: %+v", f.Handler.listEntries)
	}
}
