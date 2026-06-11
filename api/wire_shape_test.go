package api

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/irc"
	"github.com/lepinkainen/lurker/mirc"
	"github.com/lepinkainen/lurker/preview"
)

// coreWireKeys is the canonical JSON key set of irc.MessageCore. MessageEvent
// must serialize as "type" + these keys, messageDTO as these keys +
// "previews". Guards against tag drift when fields move or get added.
var coreWireKeys = []string{
	"id", "network_id", "buffer_id", "msgid", "ts", "sender", "userhost",
	"account", "kind", "target", "content",
	"display_kind", "is_self", "mentions_me", "counts_as_unread",
	"sender_color", "target_color", "highlight", "highlight_pattern",
	"netsplit", "segments",
}

// fullCore returns a MessageCore with every field non-zero so no omitzero /
// omitempty tag can hide a key from the marshaled output.
func fullCore() irc.MessageCore {
	color := 3
	return irc.MessageCore{
		ID: uuid.New(), NetworkID: uuid.New(), BufferID: uuid.New(),
		MsgID: "m1", TS: "2026-01-01T00:00:00Z", Sender: "alice",
		Userhost: "alice@host", Account: "alice", Kind: "privmsg",
		Target: "#go", Content: "hello",
		DisplayKind: "privmsg", IsSelf: true, MentionsMe: true, CountsAsUnread: true,
		SenderColor: &color, TargetColor: &color,
		Highlight: true, HighlightPattern: "go",
		Netsplit: &irc.NetsplitMeta{},
		Segments: []mirc.Segment{{Text: "hello"}},
	}
}

func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func sortedWith(base []string, extra ...string) []string {
	out := append(slices.Clone(base), extra...)
	slices.Sort(out)
	return out
}

func TestMessageEventWireKeySet(t *testing.T) {
	ev := irc.MessageEvent{Type: "message", MessageCore: fullCore()}
	got := jsonKeys(t, ev)
	want := sortedWith(coreWireKeys, "type")
	if !slices.Equal(got, want) {
		t.Fatalf("MessageEvent JSON keys = %v\nwant %v", got, want)
	}
}

func TestMessageDTOWireKeySet(t *testing.T) {
	dto := messageDTO{
		MessageCore: fullCore(),
		Previews:    []preview.ResolvedPreview{{URL: "https://example.test"}},
	}
	got := jsonKeys(t, dto)
	want := sortedWith(coreWireKeys, "previews")
	if !slices.Equal(got, want) {
		t.Fatalf("messageDTO JSON keys = %v\nwant %v", got, want)
	}
}
