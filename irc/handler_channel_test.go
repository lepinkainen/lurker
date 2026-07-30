package irc

import (
	"encoding/json"
	"testing"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

func TestSelfPartEmitsBufferUpdateAndPresence(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	drainEvents(events)
	f.Handler.onPart(client, mustEvent(t, ":tester!u@h PART #test :bye"))

	drained := drainEventsAfter(events)
	if !hasBufferUpdateIn(drained, false) {
		t.Fatal("missing buffer_update joined=false for self part")
	}
	if !hasPresenceIn(drained, "tester", "part") {
		t.Fatal("missing self-part presence for tester")
	}
}

func TestRemotePartPublishesPresenceOnly(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	drainEvents(events)
	f.Handler.onPart(client, mustEvent(t, ":alice!u@h PART #test :bye"))

	drained := drainEventsAfter(events)
	if !hasPresenceIn(drained, "alice", "part") {
		t.Fatal("missing remote part presence for alice")
	}
	if hasBufferUpdateIn(drained, false) {
		t.Fatal("unexpected buffer_update joined=false on remote part")
	}
}

// drainEventsAfter snapshots remaining events. Kept local so we can run the
// helpers below against the snapshot without re-draining the channel.
func drainEventsAfter(events <-chan any) []any {
	return drainEvents(events)
}

func hasBufferUpdateIn(evs []any, joined bool) bool {
	for _, ev := range evs {
		if bu, ok := ev.(*BufferUpdateEvent); ok && bu.Joined != nil && *bu.Joined == joined {
			return true
		}
	}
	return false
}

func hasPresenceIn(evs []any, nick, state string) bool {
	for _, ev := range evs {
		if p, ok := ev.(*PresenceEvent); ok && p.Nick == nick && p.State == state {
			return true
		}
	}
	return false
}

func TestTopicUpdatePersistsTopicAndPublishesBufferUpdate(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onTopic(nil, mustEvent(t, ":alice!u@h TOPIC #test :new topic"))

	var topic, setBy, setAt string
	if err := f.LogStore.DB.QueryRow(`
		SELECT topic, COALESCE(topic_set_by,''), COALESCE(topic_set_at,'')
		FROM buffers WHERE name='#test'`).Scan(&topic, &setBy, &setAt); err != nil {
		t.Fatal(err)
	}
	if topic != "new topic" {
		t.Fatalf("topic = %q, want new topic", topic)
	}
	if setBy != "alice" {
		t.Fatalf("topic_set_by = %q, want alice", setBy)
	}
	if setAt == "" {
		t.Fatal("topic_set_at not set on live TOPIC change")
	}
	drained := drainEventsAfter(events)
	found := false
	for _, ev := range drained {
		if bu, ok := ev.(*BufferUpdateEvent); ok && bu.Topic != nil && *bu.Topic == "new topic" {
			found = true
			if bu.TopicSetBy == nil || *bu.TopicSetBy != "alice" || bu.TopicSetAt == nil || *bu.TopicSetAt == "" {
				t.Fatalf("buffer_update meta = (%v, %v), want (alice, non-empty)", bu.TopicSetBy, bu.TopicSetAt)
			}
		}
	}
	if !found {
		t.Fatal("missing topic buffer_update")
	}
}

func TestTopicWhoTimePersistsMetaWithoutMessage(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onTopicWhoTime(nil, mustEvent(t, ":irc.example 333 tester #test alice!u@h 1700000000"))

	var setBy, setAt string
	if err := f.LogStore.DB.QueryRow(`
		SELECT COALESCE(topic_set_by,''), COALESCE(topic_set_at,'')
		FROM buffers WHERE name='#test'`).Scan(&setBy, &setAt); err != nil {
		t.Fatal(err)
	}
	if setBy != "alice" {
		t.Fatalf("topic_set_by = %q, want alice (hostmask stripped)", setBy)
	}
	if setAt != "2023-11-14T22:13:20.000Z" {
		t.Fatalf("topic_set_at = %q, want 2023-11-14T22:13:20.000Z", setAt)
	}
	if !hasTopicMetaUpdate(events, "alice", "2023-11-14T22:13:20.000Z") {
		t.Fatal("missing topic meta buffer_update")
	}
	if got := handlerMessageCount(t, f); got != 0 {
		t.Fatalf("message count = %d, want 0 (333 is metadata-only)", got)
	}
}

func TestTopicWhoTimeMalformedTimeIgnored(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onTopicWhoTime(nil, mustEvent(t, ":irc.example 333 tester #test alice notanumber"))

	var count int
	if err := f.LogStore.DB.QueryRow(`SELECT COUNT(*) FROM buffers WHERE name='#test'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("malformed 333 must not create the buffer or store meta")
	}
	if got := handlerMessageCount(t, f); got != 0 {
		t.Fatalf("message count = %d, want 0", got)
	}
}

func TestTopicReplyDoesNotClobberMeta(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onTopicWhoTime(nil, mustEvent(t, ":irc.example 333 tester #test alice 1700000000"))
	f.Handler.onTopicReply(nil, mustEvent(t, ":irc.example 332 tester #test :fresh topic"))

	var topic, setBy string
	if err := f.LogStore.DB.QueryRow(`
		SELECT COALESCE(topic,''), COALESCE(topic_set_by,'')
		FROM buffers WHERE name='#test'`).Scan(&topic, &setBy); err != nil {
		t.Fatal(err)
	}
	if topic != "fresh topic" {
		t.Fatalf("topic = %q, want fresh topic", topic)
	}
	if setBy != "alice" {
		t.Fatalf("topic_set_by = %q, want alice (332 must not clear meta)", setBy)
	}
}

func TestNoTopicReplyClearsStaleTopic(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onTopicReply(nil, mustEvent(t, ":irc.example 332 tester #test :stale topic"))
	f.Handler.onTopicWhoTime(nil, mustEvent(t, ":irc.example 333 tester #test alice 1700000000"))
	f.Handler.onNoTopicReply(nil, mustEvent(t, ":irc.example 331 tester #test :No topic is set"))

	var topic, setBy string
	if err := f.LogStore.DB.QueryRow(`
		SELECT COALESCE(topic,''), COALESCE(topic_set_by,'')
		FROM buffers WHERE name='#test'`).Scan(&topic, &setBy); err != nil {
		t.Fatal(err)
	}
	if topic != "" {
		t.Fatalf("topic = %q, want empty (331 must clear the stale topic)", topic)
	}
	if setBy != "alice" {
		t.Fatalf("topic_set_by = %q, want alice (331 must not clear meta)", setBy)
	}
	if got := handlerMessageCount(t, f); got != 0 {
		t.Fatalf("message count = %d, want 0 (331 is metadata only)", got)
	}
}

func TestNoTopicReplyIgnoresNonChannelTarget(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onNoTopicReply(nil, mustEvent(t, ":irc.example 331 tester tester :No topic is set"))

	var count int
	if err := f.LogStore.DB.QueryRow(`SELECT COUNT(*) FROM buffers WHERE name='tester'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("331 with a non-channel target must not create a buffer")
	}
}

// bufferUpdateJSONFields marshals a buffer_update and returns its JSON keys,
// so tests can assert on the actual wire shape (present vs omitted fields).
func bufferUpdateJSONFields(t *testing.T, bu *BufferUpdateEvent) map[string]any {
	t.Helper()
	raw, err := json.Marshal(bu)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestTopicClearReachesWire(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	f.Handler.onTopic(nil, mustEvent(t, ":alice!u@h TOPIC #test :old topic"))
	f.Handler.onTopic(nil, mustEvent(t, ":bob!u@h TOPIC #test :"))

	var last *BufferUpdateEvent
	for _, ev := range drainEventsAfter(events) {
		if bu, ok := ev.(*BufferUpdateEvent); ok {
			last = bu
		}
	}
	if last == nil {
		t.Fatal("missing buffer_update for topic clear")
	}
	fields := bufferUpdateJSONFields(t, last)
	topic, present := fields["topic"]
	if !present {
		t.Fatal("topic clear omitted from wire — clients would keep the old topic")
	}
	if topic != "" {
		t.Fatalf("topic = %v, want empty string", topic)
	}
}

func TestTopicUpdatesDoNotForceJoined(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()

	// 332/333 are not proof of membership (servers answer topic queries for
	// channels we're not in) — their updates must not carry a joined field.
	f.Handler.onTopicReply(nil, mustEvent(t, ":irc.example 332 tester #test :some topic"))
	f.Handler.onTopicWhoTime(nil, mustEvent(t, ":irc.example 333 tester #test alice!u@h 1700000000"))

	for _, ev := range drainEventsAfter(events) {
		bu, ok := ev.(*BufferUpdateEvent)
		if !ok {
			continue
		}
		if _, present := bufferUpdateJSONFields(t, bu)["joined"]; present {
			t.Fatal("topic buffer_update must not carry joined")
		}
	}
}

func TestTopicWhoTimeParsesNickAtHostSetter(t *testing.T) {
	f := newTestHandlerFixture(t)

	// Some ircds/services send the 333 setter as nick@host (no ident).
	f.Handler.onTopicWhoTime(nil, mustEvent(t, ":irc.example 333 tester #test alice@host 1700000000"))

	var setBy string
	if err := f.LogStore.DB.QueryRow(`
		SELECT COALESCE(topic_set_by,'') FROM buffers WHERE name='#test'`).Scan(&setBy); err != nil {
		t.Fatal(err)
	}
	if setBy != "alice" {
		t.Fatalf("topic_set_by = %q, want alice (nick@host form)", setBy)
	}
}

func TestModeRouting(t *testing.T) {
	f := newTestHandlerFixture(t)

	f.Handler.onMode(nil, mustEvent(t, ":alice!u@h MODE #test +o bob"))
	f.Handler.onMode(nil, mustEvent(t, ":irc.example MODE tester +i"))

	rows, err := f.LogStore.DB.Query(`
		SELECT b.name, b.kind, m.kind, COALESCE(m.target, ''), COALESCE(m.content, '')
		FROM messages m
		JOIN buffers b ON b.id = m.buffer_id
		ORDER BY m.id`)
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
		if err := rows.Scan(&msg.BufferName, &msg.BufferKind, &msg.Kind, &msg.Target, &msg.Content); err != nil {
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
	if got[0].BufferName != "#test" || got[0].BufferKind != ircdb.BufferChannel || got[0].Content != "+o bob" {
		t.Fatalf("channel mode message = %+v", got[0])
	}
	if got[1].BufferKind != ircdb.BufferStatus || got[1].Target != "tester" || got[1].Content != "+i" {
		t.Fatalf("user mode message = %+v", got[1])
	}
}

func TestModePublishesUpdatedMemberPrefixes(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	client := newTestClient(t)
	f.Handler.register(client)
	events, _, unsub := h.Subscribe(32)
	defer unsub()

	runClientEvent(client, mustEvent(t, ":tester!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":irc.example 353 tester = #test :tester bob"))
	runClientEvent(client, mustEvent(t, ":irc.example 366 tester #test :End of NAMES"))
	drainEvents(events)

	assertModePrefix := func(raw, want string) {
		t.Helper()
		runClientEvent(client, mustEvent(t, raw))
		for _, ev := range drainEvents(events) {
			memberList, ok := ev.(*MemberListEvent)
			if !ok || memberList.Channel != "#test" {
				continue
			}
			for _, member := range memberList.Members {
				if member.Nick == "bob" {
					if member.Prefix != want {
						t.Fatalf("%s: bob prefix = %q, want %q", raw, member.Prefix, want)
					}
					return
				}
			}
			t.Fatalf("%s: bob missing from member list: %+v", raw, memberList.Members)
		}
		t.Fatalf("%s: no member_list event published", raw)
	}

	assertModePrefix(":tester!u@h MODE #test +o bob", "@")
	assertModePrefix(":tester!u@h MODE #test -o bob", "")
}

func TestSelfKickClearsChannelState(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(32)
	defer unsub()
	client := newTestClient(t)

	var cleared []string
	f.Handler.clearMemberListHook = func(ch string) { cleared = append(cleared, ch) }
	var joinedStates []bool
	f.Handler.setJoinedHook = func(_ string, joined bool) { joinedStates = append(joinedStates, joined) }
	f.Handler.lastMemberListHash = map[string]uint64{"#test": 1}

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	f.Handler.onJoin(client, mustEvent(t, ":alice!u@h JOIN #test"))
	drainEvents(events)
	f.Handler.onKick(client, mustEvent(t, ":op!u@h KICK #test tester :flooding"))

	drained := drainEventsAfter(events)
	if !hasPresenceIn(drained, "tester", "kick") {
		t.Fatal("missing kick presence for tester")
	}
	if !hasBufferUpdateIn(drained, false) {
		t.Fatal("missing buffer_update joined=false on self kick")
	}
	if len(cleared) == 0 || cleared[len(cleared)-1] != "#test" {
		t.Fatalf("member list not cleared for #test, cleared=%v", cleared)
	}
	if len(joinedStates) == 0 || joinedStates[len(joinedStates)-1] != false {
		t.Fatalf("setJoinedHook states = %v, want trailing false", joinedStates)
	}
	if _, ok := f.Handler.lastMemberListHash["#test"]; ok {
		t.Fatal("lastMemberListHash entry for #test not deleted")
	}
	if len(f.Handler.userChannels.byNick[caseFoldNick("alice")]) != 0 {
		t.Fatal("userChannels still tracks #test after self kick")
	}

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "kick" || msg.Target != "tester" || msg.Content != "flooding" {
		t.Fatalf("stored message = %+v, want kick/tester/flooding", msg)
	}
}

func TestRemoteKickPublishesPresenceOnly(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(32)
	defer unsub()
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	f.Handler.onJoin(client, mustEvent(t, ":alice!u@h JOIN #test"))
	drainEvents(events)
	f.Handler.onKick(client, mustEvent(t, ":op!u@h KICK #test alice :bye"))

	drained := drainEventsAfter(events)
	if !hasPresenceIn(drained, "alice", "kick") {
		t.Fatal("missing kick presence for alice")
	}
	if hasBufferUpdateIn(drained, false) {
		t.Fatal("unexpected buffer_update joined=false on remote kick")
	}
	if len(f.Handler.userChannels.byNick[caseFoldNick("alice")]) != 0 {
		t.Fatal("alice still tracked in #test after kick")
	}

	msg := lastHandlerMessage(t, f)
	if msg.Kind != "kick" || msg.Target != "alice" || msg.Content != "bye" {
		t.Fatalf("stored message = %+v, want kick/alice/bye", msg)
	}
}

// TestEndOfWhoResolvesFoldedChannelName guards against phantom duplicate
// buffers. girc's auto-WHO on a remote joiner ends with RPL_ENDOFWHO targeting
// the joiner's nick; onEndOfWho then fans out over that user's ChannelList,
// which girc stores RFC1459-FOLDED ("#foo") while the tracked channel keeps its
// display casing ("#Foo"). ensureBuffer does not case-fold, so publishing the
// folded name would spawn a second buffer "#foo". The handler must resolve the
// folded name back to girc's canonical "#Foo" before publishing.
func TestEndOfWhoResolvesFoldedChannelName(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	events, _, unsub := h.Subscribe(16)
	defer unsub()
	client := newTestClient(t)

	// Populate girc's tracked state via a remote JOIN (a self-join would trip
	// girc's auto-WHO Send on an unconnected client). The channel is created
	// with display casing "#Foo"; the user's ChannelList holds folded "#foo".
	runClientEvent(client, mustEvent(t, ":remote!u@h JOIN #Foo"))

	user := client.LookupUser("remote")
	if user == nil || len(user.ChannelList) != 1 || user.ChannelList[0] != "#foo" {
		t.Fatalf("precondition: want remote.ChannelList=[#foo], got %+v", user)
	}
	if ch := client.LookupChannel("#foo"); ch == nil || ch.Name != "#Foo" {
		t.Fatalf("precondition: LookupChannel(#foo).Name should be #Foo, got %+v", ch)
	}

	f.Handler.onEndOfWho(client, mustEvent(t, ":irc.example 315 tester remote :End of WHO list"))

	names := channelBufferNames(t, f)
	if len(names) != 1 || names[0] != "#Foo" {
		t.Fatalf("channel buffers = %v, want [#Foo] with no phantom #foo", names)
	}

	var memberEv *MemberListEvent
	for _, ev := range drainEvents(events) {
		if ml, ok := ev.(*MemberListEvent); ok {
			memberEv = ml
		}
	}
	if memberEv == nil {
		t.Fatal("no member_list event published")
	}
	if memberEv.Channel != "#Foo" {
		t.Fatalf("member_list channel = %q, want #Foo", memberEv.Channel)
	}
}

func channelBufferNames(t *testing.T, f *testHandlerFixture) []string {
	t.Helper()
	rows, err := f.LogStore.DB.Query(`SELECT name FROM buffers WHERE kind = ? ORDER BY name`, ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			t.Fatalf("close rows: %v", cerr)
		}
	}()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

func TestKickWithoutReasonAndShortParams(t *testing.T) {
	h := hub.New()
	f := newTestHandlerFixture(t, withTestHandlerHub(h))
	client := newTestClient(t)

	f.Handler.onJoin(client, mustEvent(t, ":tester!u@h JOIN #test"))
	before := handlerMessageCount(t, f)
	// Malformed kick (no target param) must be ignored.
	f.Handler.onKick(client, mustEvent(t, ":op!u@h KICK #test"))
	if got := handlerMessageCount(t, f); got != before {
		t.Fatalf("message count = %d after malformed kick, want %d", got, before)
	}

	f.Handler.onKick(client, mustEvent(t, ":op!u@h KICK #test alice"))
	msg := lastHandlerMessage(t, f)
	if msg.Kind != "kick" || msg.Target != "alice" || msg.Content != "" {
		t.Fatalf("stored message = %+v, want kick/alice with empty reason", msg)
	}
}
