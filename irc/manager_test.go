package irc

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lrstanley/girc"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

func TestManagerPersistsMessages(t *testing.T) {
	fixture := newTestHandlerFixture(t)
	h := fixture.Handler
	logStore := fixture.LogStore

	h.onJoin(nil, mustEvent(t, ":tester!~u@h JOIN #test"))
	h.onPrivmsg(nil, mustEvent(t, ":alice!~u@h PRIVMSG #test :hello from fake"))

	var n int
	if qerr := logStore.DB.QueryRow(`SELECT COUNT(*) FROM buffers WHERE name='#test'`).Scan(&n); qerr != nil {
		t.Fatal(qerr)
	}
	if n != 1 {
		t.Fatalf("channel buffer count = %d, want 1", n)
	}

	if qerr := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE kind='privmsg' AND content='hello from fake'`).Scan(&n); qerr != nil {
		t.Fatal(qerr)
	}
	if n != 1 {
		t.Fatalf("persisted privmsg count = %d, want 1", n)
	}

	var hit string
	err := logStore.DB.QueryRow(`SELECT content FROM messages_fts WHERE messages_fts MATCH 'fake' LIMIT 1`).Scan(&hit)
	if err != nil || hit != "hello from fake" {
		t.Fatalf("fts hit = %q, err = %v", hit, err)
	}
}

func TestSelfJoinDoesNotPersistJoinMessage(t *testing.T) {
	fixture := newTestHandlerFixture(t)
	logStore := fixture.LogStore
	client := newTestClient(t)
	h := fixture.Handler

	h.onJoin(client, mustEvent(t, ":tester!~u@h JOIN #test"))
	h.onJoin(client, mustEvent(t, ":alice!~u@h JOIN #test"))

	var n int
	if qerr := logStore.DB.QueryRow(`SELECT COUNT(*) FROM buffers WHERE name='#test'`).Scan(&n); qerr != nil {
		t.Fatal(qerr)
	}
	if n != 1 {
		t.Fatalf("channel buffer count = %d, want 1", n)
	}
	if qerr := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE kind='join' AND sender='tester'`).Scan(&n); qerr != nil {
		t.Fatal(qerr)
	}
	if n != 0 {
		t.Fatalf("self join count = %d, want 0", n)
	}
	if qerr := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE kind='join' AND sender='alice'`).Scan(&n); qerr != nil {
		t.Fatal(qerr)
	}
	if n != 1 {
		t.Fatalf("remote join count = %d, want 1", n)
	}
}

func TestMsgIDDedupAndServerTime(t *testing.T) {
	fixture := newTestHandlerFixture(t)
	logStore := fixture.LogStore
	h := fixture.Handler

	h.onJoin(nil, mustEvent(t, ":tester!~u@h JOIN #test"))

	const msgid = "mid-42"
	const serverTime = "2025-01-02T03:04:05.678Z"
	e := mustEvent(t, "@time="+serverTime+";msgid="+msgid+" :alice!~u@h PRIVMSG #test :once")
	h.onPrivmsg(nil, e)
	h.onPrivmsg(nil, e)

	// Same text + ts but distinct msgid must NOT dedup — proves msgid is the discriminator.
	e2 := mustEvent(t, "@time="+serverTime+";msgid=mid-43 :alice!~u@h PRIVMSG #test :once")
	h.onPrivmsg(nil, e2)

	var count int
	if qerr := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE msgid=?`, msgid).Scan(&count); qerr != nil {
		t.Fatal(qerr)
	}
	if count != 1 {
		t.Fatalf("expected dedup via msgid, got %d rows", count)
	}
	var total int
	if qerr := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE content='once'`).Scan(&total); qerr != nil {
		t.Fatal(qerr)
	}
	if total != 2 {
		t.Fatalf("expected 2 rows (distinct msgids), got %d — msgid is not the dedup key", total)
	}

	var gotTS string
	if qerr := logStore.DB.QueryRow(`SELECT ts FROM messages WHERE msgid=?`, msgid).Scan(&gotTS); qerr != nil {
		t.Fatal(qerr)
	}
	gotParsed, err := time.Parse("2006-01-02T15:04:05.000Z", gotTS)
	if err != nil {
		t.Fatalf("unparseable ts %q: %v", gotTS, err)
	}
	want, _ := time.Parse(time.RFC3339Nano, serverTime)
	if !gotParsed.Equal(want) {
		t.Fatalf("ts mismatch: got %s, want %s", gotParsed, want)
	}
}

func TestSyntheticClientEventsDoNotPersistToStatusBuffer(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	netrow, err := stores.UpsertNetwork(t.Context(), ircdb.Network{Name: "fake", Host: "127.0.0.1", Port: 6667, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(netrow.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{stores: stores, db: logStore.DB, networkID: netrow.ID, networkName: "fake"}

	h.onUnhandledEvent(girc.Event{Command: girc.UPDATE_GENERAL})
	h.onUnhandledEvent(girc.Event{Command: girc.UPDATE_STATE})

	var count int
	if err := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("message count = %d, want 0", count)
	}
}

func TestTopicWhoTimeAndCreationTimeSuppressed(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	netrow, err := stores.UpsertNetwork(t.Context(), ircdb.Network{Name: "fake", Host: "127.0.0.1", Port: 6667, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(netrow.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{stores: stores, db: logStore.DB, networkID: netrow.ID, networkName: "fake"}

	// 333 has a registered handler, 329 is suppressed outright — both must be
	// in isExplicitlyHandledEvent so onAllEvent never writes a status notice
	// (forgetting the switch entry double-stores registered numerics).
	for _, cmd := range []string{girc.RPL_TOPICWHOTIME, girc.RPL_CREATIONTIME} {
		if !isExplicitlyHandledEvent(cmd) {
			t.Fatalf("isExplicitlyHandledEvent(%s) = false, want true", cmd)
		}
	}

	h.onAllEvent(nil, mustEvent(t, ":irc.example 333 tester #test alice!u@h 1700000000"))
	h.onAllEvent(nil, mustEvent(t, ":irc.example 329 tester #test 1600000000"))

	var count int
	if err := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("message count = %d, want 0 (333/329 must not leak to status buffer)", count)
	}
}

// TestListStartAndNoTopicSuppressed pins that the /LIST header (321) and the
// on-join "no topic" reply (331) never reach the status buffer: 321 is dropped
// outright, 331 only clears topic state (see TestNoTopicReplyClearsStaleTopic).
func TestListStartAndNoTopicSuppressed(t *testing.T) {
	f := newTestHandlerFixture(t)

	for _, cmd := range []string{girc.RPL_LISTSTART, girc.RPL_NOTOPIC} {
		if !isExplicitlyHandledEvent(cmd) {
			t.Fatalf("isExplicitlyHandledEvent(%s) = false, want true", cmd)
		}
	}

	f.Handler.onAllEvent(nil, mustEvent(t, ":irc.example 321 tester Channel :Users  Name"))
	f.Handler.onAllEvent(nil, mustEvent(t, ":irc.example 331 tester #test :No topic is set"))

	if got := handlerMessageCount(t, f); got != 0 {
		t.Fatalf("message count = %d, want 0 (321/331 must not leak to status buffer)", got)
	}
}

func TestUnhandledServerReplyPersistsToStatusBuffer(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	netrow, err := stores.UpsertNetwork(t.Context(), ircdb.Network{Name: "fake", Host: "127.0.0.1", Port: 6667, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(netrow.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{stores: stores, db: logStore.DB, networkID: netrow.ID, networkName: "fake"}

	h.onUnhandledEvent(mustEvent(t, ":irc.example 251 tester :There are 42 users and 10 servers"))

	var kind, sender, content string
	if err := logStore.DB.QueryRow(`SELECT kind, sender, content FROM messages ORDER BY id DESC LIMIT 1`).Scan(&kind, &sender, &content); err != nil {
		t.Fatal(err)
	}
	if kind != "notice" {
		t.Fatalf("kind = %q, want notice", kind)
	}
	if sender != "irc.example" {
		t.Fatalf("sender = %q, want irc.example", sender)
	}
	if content != "251 There are 42 users and 10 servers" {
		t.Fatalf("content = %q", content)
	}
}

func TestUnhandledChannelErrorPersistsToChannelBuffer(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	netrow, err := stores.UpsertNetwork(t.Context(), ircdb.Network{Name: "fake", Host: "127.0.0.1", Port: 6667, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(netrow.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{stores: stores, db: logStore.DB, networkID: netrow.ID, networkName: "fake"}

	h.onUnhandledEvent(mustEvent(t, ":irc.example 482 tester #test :You're not channel operator"))

	var bufName, bufKind, kind, sender, content string
	if err := logStore.DB.QueryRow(`
		SELECT b.name, b.kind, m.kind, m.sender, m.content
		FROM messages m
		JOIN buffers b ON b.id = m.buffer_id
		ORDER BY m.id DESC
		LIMIT 1`).Scan(&bufName, &bufKind, &kind, &sender, &content); err != nil {
		t.Fatal(err)
	}
	if bufName != "#test" {
		t.Fatalf("buffer name = %q, want #test", bufName)
	}
	if bufKind != ircdb.BufferChannel {
		t.Fatalf("buffer kind = %q, want %q", bufKind, ircdb.BufferChannel)
	}
	if kind != "error" {
		t.Fatalf("kind = %q, want error", kind)
	}
	if sender != "irc.example" {
		t.Fatalf("sender = %q, want irc.example", sender)
	}
	if content != "You're not channel operator" {
		t.Fatalf("content = %q", content)
	}
}

func TestBanlistRepliesPersistToStatusBuffer(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	netrow, err := stores.UpsertNetwork(t.Context(), ircdb.Network{Name: "fake", Host: "127.0.0.1", Port: 6667, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(netrow.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{stores: stores, db: logStore.DB, networkID: netrow.ID, networkName: "fake"}

	h.onUnhandledEvent(mustEvent(t, ":irc.example 367 tester #test bad!*@* oper 1714410000"))
	h.onUnhandledEvent(mustEvent(t, ":irc.example 368 tester #test :End of Channel Ban List"))

	rows, err := logStore.DB.Query(`SELECT kind, content FROM messages ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			t.Fatalf("close rows: %v", cerr)
		}
	}()
	var got []string
	for rows.Next() {
		var kind, content string
		if err := rows.Scan(&kind, &content); err != nil {
			t.Fatal(err)
		}
		got = append(got, kind+":"+content)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"notice:367 #test bad!*@* oper 1714410000",
		"notice:368 #test End of Channel Ban List",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestMOTDLinesRenderAsBox(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	netrow, err := stores.UpsertNetwork(t.Context(), ircdb.Network{Name: "fake", Host: "127.0.0.1", Port: 6667, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(netrow.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{stores: stores, db: logStore.DB, networkID: netrow.ID, networkName: "fake"}

	h.onUnhandledEvent(mustEvent(t, ":irc.example 375 tester :- irc.example Message of the Day -"))
	h.onUnhandledEvent(mustEvent(t, ":irc.example 372 tester :- Open to all users on 6667"))
	h.onUnhandledEvent(mustEvent(t, ":irc.example 372 tester :-"))
	h.onUnhandledEvent(mustEvent(t, ":irc.example 376 tester :End of MOTD command."))

	rows, err := logStore.DB.Query(`SELECT kind, sender, content FROM messages ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			t.Fatalf("close rows: %v", cerr)
		}
	}()
	type row struct{ kind, sender, content string }
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.kind, &r.sender, &r.content); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d rows, want 4: %+v", len(got), got)
	}

	start, body, empty, end := got[0], got[1], got[2], got[3]

	if !strings.HasPrefix(start.content, "╭─ Message of the Day") {
		t.Fatalf("start content = %q, want prefix %q", start.content, "╭─ Message of the Day")
	}
	if strings.Contains(start.content, "375") {
		t.Fatalf("start content = %q, must not contain raw numeric 375", start.content)
	}
	if start.kind != "notice" {
		t.Fatalf("start kind = %q, want notice", start.kind)
	}
	if start.sender != "irc.example" {
		t.Fatalf("start sender = %q, want irc.example", start.sender)
	}

	if body.content != "│ Open to all users on 6667" {
		t.Fatalf("body content = %q, want %q", body.content, "│ Open to all users on 6667")
	}
	if strings.Contains(body.content, "372") {
		t.Fatalf("body content = %q, must not contain raw numeric 372", body.content)
	}

	if empty.content != "│" {
		t.Fatalf("empty body content = %q, want %q", empty.content, "│")
	}

	if !strings.HasPrefix(end.content, "╰─ End of MOTD") {
		t.Fatalf("end content = %q, want prefix %q", end.content, "╰─ End of MOTD")
	}
	if strings.Contains(end.content, "376") {
		t.Fatalf("end content = %q, must not contain raw numeric 376", end.content)
	}
}

func TestPublishMemberListUsesTrackedMembers(t *testing.T) {
	h := hub.New()
	fixture := newTestHandlerFixture(t, withTestHandlerHub(h))
	client := newTestClient(t)
	handler := fixture.Handler
	handler.register(client)

	runClientEvent(client, mustEvent(t, ":tester!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":fake 353 tester = #test :tester"))

	events, _, unsub := h.Subscribe(16)
	defer unsub()
	handler.onEndOfNames(client, mustEvent(t, ":fake 366 tester #test :End of NAMES"))

	var memberList *MemberListEvent
	deadline := time.After(200 * time.Millisecond)
drain1:
	for {
		select {
		case ev := <-events:
			if ml, ok := ev.(*MemberListEvent); ok && ml.Channel == "#test" {
				memberList = ml
				break drain1
			}
		case <-deadline:
			break drain1
		}
	}
	if memberList == nil {
		t.Fatal("member_list event never published")
	}
	if len(memberList.Members) != 1 || memberList.Members[0].Nick != "tester" || !memberList.Members[0].Self {
		t.Fatalf("member list = %+v, want self tester", memberList.Members)
	}
}

func TestMemberListUsesDisplayNickCaseFromUser(t *testing.T) {
	client := newTestClient(t)
	runClientEvent(client, mustEvent(t, ":Shrike!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":fake 353 tester = #test :shrike"))

	members := buildChannelMembers(client, "#test")
	if len(members) != 1 {
		t.Fatalf("len(members) = %d, want 1", len(members))
	}
	if members[0].Nick != "Shrike" {
		t.Fatalf("member nick = %q, want display-case nick %q", members[0].Nick, "Shrike")
	}
}

func TestMemberListStripsMircCodesFromRealname(t *testing.T) {
	client := newTestClient(t)
	runClientEvent(client, mustEvent(t, ":Shrike!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":fake 353 tester = #test :shrike"))
	runClientEvent(client, mustEvent(t, ":fake 352 tester #test u h fake Shrike H :0 \x02\x034,1Real\x03\x02 Name "))

	members := buildChannelMembers(client, "#test")
	if len(members) != 1 {
		t.Fatalf("len(members) = %d, want 1", len(members))
	}
	if members[0].Realname != "Real Name" {
		t.Fatalf("realname = %q, want mIRC codes stripped and trimmed %q", members[0].Realname, "Real Name")
	}
}

func TestHashMemberListIsOrderAndFieldSensitive(t *testing.T) {
	a := []ChannelUser{{Nick: "alice", Prefix: "@", Realname: "Alice"}, {Nick: "bob"}}
	b := []ChannelUser{{Nick: "alice", Prefix: "@", Realname: "Alice"}, {Nick: "bob"}}
	if hashMemberList(a) != hashMemberList(b) {
		t.Fatal("identical lists must hash equal")
	}
	c := []ChannelUser{{Nick: "bob"}, {Nick: "alice", Prefix: "@", Realname: "Alice"}}
	if hashMemberList(a) == hashMemberList(c) {
		t.Fatal("reordered list must hash differently")
	}
	d := []ChannelUser{{Nick: "alice", Prefix: "@", Realname: "Alice2"}, {Nick: "bob"}}
	if hashMemberList(a) == hashMemberList(d) {
		t.Fatal("realname change must hash differently")
	}
	e := []ChannelUser{{Nick: "alice", Prefix: "@", Realname: "Alice", Away: true}, {Nick: "bob"}}
	if hashMemberList(a) == hashMemberList(e) {
		t.Fatal("away change must hash differently")
	}
}

func TestPublishMemberListDedupSuppressesIdenticalRepublish(t *testing.T) {
	h := hub.New()
	fixture := newTestHandlerFixture(t, withTestHandlerHub(h))
	client := newTestClient(t)
	handler := fixture.Handler
	handler.register(client)

	runClientEvent(client, mustEvent(t, ":tester!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":fake 353 tester = #test :tester"))

	events, _, unsub := h.Subscribe(16)
	defer unsub()

	handler.onEndOfNames(client, mustEvent(t, ":fake 366 tester #test :End of NAMES"))
	handler.onEndOfNames(client, mustEvent(t, ":fake 366 tester #test :End of NAMES"))

	count := 0
	deadline := time.After(200 * time.Millisecond)
drain2:
	for {
		select {
		case ev := <-events:
			if ml, ok := ev.(*MemberListEvent); ok && ml.Channel == "#test" {
				count++
			}
		case <-deadline:
			break drain2
		}
	}
	if count != 1 {
		t.Fatalf("member_list publishes = %d, want 1 (dedup)", count)
	}
}

func TestOnEndOfWhoBackfillsRealname(t *testing.T) {
	h := hub.New()
	fixture := newTestHandlerFixture(t, withTestHandlerHub(h))
	client := newTestClient(t)
	handler := fixture.Handler
	handler.register(client)

	runClientEvent(client, mustEvent(t, ":tester!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":fake 353 tester = #test :tester"))
	handler.onEndOfNames(client, mustEvent(t, ":fake 366 tester #test :End of NAMES"))

	events, _, unsub := h.Subscribe(16)
	defer unsub()

	// girc auto-WHOs the joined channel; feed a RPL_WHOREPLY then RPL_ENDOFWHO.
	runClientEvent(client, mustEvent(t, ":fake 352 tester #test u host fake tester H :0 Real Name"))
	runClientEvent(client, mustEvent(t, ":fake 315 tester #test :End of WHO list"))

	var memberList *MemberListEvent
	for range 8 {
		select {
		case ev := <-events:
			if ml, ok := ev.(*MemberListEvent); ok && ml.Channel == "#test" {
				memberList = ml
			}
		default:
		}
	}
	if memberList == nil {
		t.Fatal("member_list never republished after RPL_ENDOFWHO")
	}
	if len(memberList.Members) != 1 || memberList.Members[0].Realname != "Real Name" {
		t.Fatalf("member list = %+v, want realname 'Real Name'", memberList.Members)
	}
}

func TestOnEndOfWhoNickTargetRepublishesChannels(t *testing.T) {
	h := hub.New()
	fixture := newTestHandlerFixture(t, withTestHandlerHub(h))
	client := newTestClient(t)
	handler := fixture.Handler
	handler.register(client)

	runClientEvent(client, mustEvent(t, ":tester!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":fake 353 tester = #test :tester alice"))
	handler.onEndOfNames(client, mustEvent(t, ":fake 366 tester #test :End of NAMES"))

	events, _, unsub := h.Subscribe(16)
	defer unsub()

	// girc per-nick WHO (remote JOIN path): target is the nick, not the channel.
	runClientEvent(client, mustEvent(t, ":fake 352 tester #test u host fake alice H :0 Alice Real"))
	runClientEvent(client, mustEvent(t, ":fake 315 tester alice :End of WHO list"))

	var memberList *MemberListEvent
	for range 8 {
		select {
		case ev := <-events:
			if ml, ok := ev.(*MemberListEvent); ok && ml.Channel == "#test" {
				memberList = ml
			}
		default:
		}
	}
	if memberList == nil {
		t.Fatal("per-nick RPL_ENDOFWHO did not trigger member_list republish")
	}
	var alice *ChannelUser
	for i := range memberList.Members {
		if memberList.Members[i].Nick == "alice" {
			alice = &memberList.Members[i]
		}
	}
	if alice == nil {
		t.Fatalf("alice missing from republished list: %+v", memberList.Members)
	}
	if alice.Realname != "Alice Real" {
		t.Fatalf("alice realname = %q, want 'Alice Real'", alice.Realname)
	}
}

func TestManagerStartAndStopNetworkIndividually(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	ctx := t.Context()
	h := hub.New()
	m := NewManager(t.Context(), stores, h)
	f := &fakeConnector{waitForClose: true, attempts: make(chan ServerConfig, 16)}
	m.connector = f.connect

	events, _, unsub := h.Subscribe(16)
	defer unsub()

	n1, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "NetOne", Host: "127.0.0.1", Port: 6667, TLS: false, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	n2, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "NetTwo", Host: "127.0.0.1", Port: 6668, TLS: false, Nick: "b"})
	if err != nil {
		t.Fatal(err)
	}

	if err := m.StartNetwork(n1.ID, NetworkConfig{Name: n1.Name, Servers: []ServerConfig{{Host: "127.0.0.1", Port: 6667}}, Nick: "a", User: "a", Realname: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := m.StartNetwork(n2.ID, NetworkConfig{Name: n2.Name, Servers: []ServerConfig{{Host: "127.0.0.1", Port: 6668}}, Nick: "b", User: "b", Realname: "b"}); err != nil {
		t.Fatal(err)
	}

	f.awaitCalls(t, 2, time.Second)

	if err := m.StopNetwork(n1.ID); err != nil {
		t.Fatal(err)
	}
	// Wait for the network_state events that describe the post-stop topology:
	// n1 must reach Disconnected and n2 must still be Connecting.
	awaitState(t, events, time.Second, n1.ID, StateDisconnected.String())
	if got := m.StateSnapshot()[n2.ID]; got != StateConnecting.String() {
		t.Fatalf("n2 state = %q, want %q", got, StateConnecting.String())
	}

	if err := m.StopNetwork(n2.ID); err != nil {
		t.Fatal(err)
	}
	m.Wait()
}

func TestYAMLStyleNetworkUsesOneLogicalConnectionConfigWithMultipleServers(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	m := NewManager(ctx, stores, nil)
	f := &fakeConnector{returnErr: errors.New("boom"), attempts: make(chan ServerConfig, 16)}
	m.connector = f.connect

	err = m.Start(ctx, []NetworkConfig{{
		Name: "Ircnet",
		Servers: []ServerConfig{
			{Host: "127.0.0.1", Port: 1, TLS: false},
			{Host: "127.0.0.1", Port: 2, TLS: false},
		},
		Nick: "tester", User: "tester", Realname: "tester",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, id := range m.stores.NetworkIDs() {
			_ = m.StopNetwork(id)
		}
		m.Wait()
	}()

	f.awaitCalls(t, 2, 2500*time.Millisecond)

	nets, err := ircdb.ListNetworks(t.Context(), stores.Control)
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 1 {
		t.Fatalf("networks len = %d, want 1", len(nets))
	}
	if nets[0].Host != "127.0.0.1" || nets[0].Port != 1 {
		t.Fatalf("stored primary server = %s:%d, want 127.0.0.1:1", nets[0].Host, nets[0].Port)
	}
}

func TestManagerFallsBackToTLS12AfterTLSError(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := NewManager(ctx, stores, nil)
	f := &fakeConnector{returnErr: errors.New("tls handshake failed"), attempts: make(chan ServerConfig, 16)}
	m.connector = f.connect

	err = m.Start(ctx, []NetworkConfig{{
		Name: "SlashNET",
		Servers: []ServerConfig{{
			Host: "irc.slashnet.org",
			Port: 6697,
			TLS:  true,
		}},
		Nick: "tester", User: "tester", Realname: "tester",
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.awaitCalls(t, 2, time.Second)
	cancel()
	m.Wait()

	calls := f.callsSnapshot()
	if calls[0].TLSMaxVersion != 0 {
		t.Fatalf("first TLS max version = %x, want Go default", calls[0].TLSMaxVersion)
	}
	if calls[1].TLSMaxVersion != tls.VersionTLS12 {
		t.Fatalf("fallback TLS max version = %x, want TLS 1.2", calls[1].TLSMaxVersion)
	}
}

func TestBuildClientConfiguresTLSInsecureSkipVerify(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	ctx := context.Background()
	netrow, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "ircnet", Host: "irc.example", Port: 6697, TLS: true, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}

	m := NewManager(t.Context(), stores, nil)
	client := m.buildClient(ctx, netrow.ID, NetworkConfig{Name: "ircnet", Nick: "tester", User: "tester", Realname: "tester"}, ServerConfig{Host: "irc.example", Port: 6697, TLS: true, TLSInsecure: true})
	if client.Config.TLSConfig == nil {
		t.Fatal("expected TLS config")
	}
	if !client.Config.TLSConfig.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify to be true")
	}
	if client.Config.TLSConfig.ServerName != "irc.example" {
		t.Fatalf("server name = %q, want irc.example", client.Config.TLSConfig.ServerName)
	}
	if client.Config.TLSConfig.MaxVersion != 0 {
		t.Fatalf("max TLS version = %x, want Go default", client.Config.TLSConfig.MaxVersion)
	}

	explicit := m.buildClient(ctx, netrow.ID, NetworkConfig{Name: "ircnet", Nick: "tester", User: "tester", Realname: "tester"}, ServerConfig{Host: "irc.example", Port: 6697, TLS: true, TLSMaxVersion: tls.VersionTLS13})
	if explicit.Config.TLSConfig.MaxVersion != tls.VersionTLS13 {
		t.Fatalf("explicit max TLS version = %x, want TLS 1.3", explicit.Config.TLSConfig.MaxVersion)
	}

	fallback := m.buildClient(ctx, netrow.ID, NetworkConfig{Name: "ircnet", Nick: "tester", User: "tester", Realname: "tester"}, ServerConfig{Host: "irc.example", Port: 6697, TLS: true, TLSMaxVersion: tls.VersionTLS12})
	if !slices.Contains(fallback.Config.TLSConfig.CipherSuites, tls.TLS_RSA_WITH_AES_128_GCM_SHA256) {
		t.Fatalf("TLS 1.2 fallback ciphers = %#v, want RSA AES-128-GCM", fallback.Config.TLSConfig.CipherSuites)
	}
}

func newTestClient(_ *testing.T) *girc.Client {
	const nick = "tester"
	return girc.New(girc.Config{Server: "test.invalid", Port: 6667, Nick: nick, User: nick, Name: nick})
}

func mustEvent(t *testing.T, raw string) girc.Event {
	t.Helper()
	e := girc.ParseEvent(raw)
	if e == nil {
		t.Fatalf("failed to parse event: %q", raw)
	}
	return *e
}

func runClientEvent(c *girc.Client, e girc.Event) {
	c.RunHandlers(&e)
}

type fakeConnector struct {
	mu           sync.Mutex
	calls        []ServerConfig
	returnErr    error
	waitForClose bool
	// attempts signals each connect attempt. Tests block on the channel
	// rather than polling callCount via time.Sleep loops.
	attempts chan ServerConfig
}

func (f *fakeConnector) connect(ctx context.Context, _ *girc.Client, server ServerConfig) error {
	f.mu.Lock()
	f.calls = append(f.calls, server)
	ch := f.attempts
	f.mu.Unlock()
	if ch != nil {
		select {
		case ch <- server:
		default:
		}
	}
	if f.waitForClose {
		<-ctx.Done()
		return nil
	}
	if f.returnErr != nil {
		return f.returnErr
	}
	return nil
}

func (f *fakeConnector) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeConnector) callsSnapshot() []ServerConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// awaitState blocks until a NetworkStateEvent for networkID with the given
// state is observed on the events channel, or fails the test on timeout.
func awaitState(t *testing.T, events <-chan any, timeout time.Duration, networkID uuid.UUID, state string) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case ev := <-events:
			ns, ok := ev.(*NetworkStateEvent)
			if !ok {
				continue
			}
			if ns.NetworkID == networkID && ns.State == state {
				return
			}
		case <-deadline.C:
			t.Fatalf("never observed state %q for network %s", state, networkID)
		}
	}
}

// awaitCalls blocks until f has observed at least n connect attempts. Uses the
// attempts channel rather than polling callCount on a sleep loop.
func (f *fakeConnector) awaitCalls(t *testing.T, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for f.callCount() < n {
		select {
		case <-f.attempts:
		case <-deadline.C:
			t.Fatalf("only %d connect attempts before timeout (wanted %d)", f.callCount(), n)
		}
	}
}

var _ = sql.ErrNoRows

// Regression for the connect-endpoint context bug: runtimes must be bound
// to the manager's base context, never a caller's short-lived one, and a
// runtime that exits must remove its m.runtime entry so a later
// StartNetwork isn't a silent no-op.
func TestRuntimeEntryRemovedAfterBaseContextCancel(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := stores.Close(); cerr != nil {
			t.Fatalf("close stores: %v", cerr)
		}
	}()

	baseCtx, cancel := context.WithCancel(t.Context())
	m := NewManager(baseCtx, stores, nil)
	f := &fakeConnector{waitForClose: true, attempts: make(chan ServerConfig, 16)}
	m.connector = f.connect

	n, err := stores.UpsertNetwork(t.Context(), ircdb.Network{Name: "NetOne", Host: "127.0.0.1", Port: 6667, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.StartNetwork(n.ID, NetworkConfig{Name: n.Name, Servers: []ServerConfig{{Host: "127.0.0.1", Port: 6667}}, Nick: "a", User: "a", Realname: "a"}); err != nil {
		t.Fatal(err)
	}
	f.awaitCalls(t, 1, time.Second)

	cancel()
	m.Wait()

	m.mu.Lock()
	entries := len(m.runtime)
	m.mu.Unlock()
	if entries != 0 {
		t.Fatalf("runtime entries after base ctx cancel = %d, want 0 (stale entry blocks restart)", entries)
	}
}

// Regression: markDisconnected from a stale (pre-restart) runtime must not
// clobber the conn/state the newer runtime installed for the same network.
func TestMarkDisconnectedStaleGenDoesNotClobberNewRuntime(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	m := NewManager(t.Context(), stores, nil)
	id := uuid.New()
	m.mu.Lock()
	m.runtimeGen = 2
	m.runtime[id] = networkRuntime{gen: 2, cancel: func() {}}
	m.conn[id] = &girc.Client{}
	m.state[id] = StateConnecting.String()
	m.mu.Unlock()

	m.markDisconnected(id, 1) // stale gen: exiting pre-restart runtime

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn[id] == nil {
		t.Error("stale markDisconnected deleted the new runtime's conn")
	}
	if m.state[id] != StateConnecting.String() {
		t.Errorf("state = %q, want %q", m.state[id], StateConnecting.String())
	}
}
