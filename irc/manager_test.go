package irc

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

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

	var count int
	if qerr := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE msgid=?`, msgid).Scan(&count); qerr != nil {
		t.Fatal(qerr)
	}
	if count != 1 {
		t.Fatalf("expected dedup via msgid, got %d rows", count)
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

func TestPublishMemberListUsesTrackedMembers(t *testing.T) {
	h := hub.New()
	fixture := newTestHandlerFixture(t, withTestHandlerHub(h))
	client := newTestClient(t)
	handler := fixture.Handler
	handler.register(client)

	runClientEvent(client, mustEvent(t, ":tester!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":fake 353 tester = #test :tester"))

	events, unsub := h.Subscribe(16)
	defer unsub()
	handler.onEndOfNames(client, mustEvent(t, ":fake 366 tester #test :End of NAMES"))

	var memberList *MemberListEvent
	for range 8 {
		select {
		case ev := <-events:
			ml, ok := ev.(*MemberListEvent)
			if ok && ml.Channel == "#test" {
				memberList = ml
			}
		default:
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

	events, unsub := h.Subscribe(16)
	defer unsub()

	handler.onEndOfNames(client, mustEvent(t, ":fake 366 tester #test :End of NAMES"))
	handler.onEndOfNames(client, mustEvent(t, ":fake 366 tester #test :End of NAMES"))

	count := 0
	for range 16 {
		select {
		case ev := <-events:
			if ml, ok := ev.(*MemberListEvent); ok && ml.Channel == "#test" {
				count++
			}
		default:
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

	events, unsub := h.Subscribe(16)
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

	events, unsub := h.Subscribe(16)
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
	m := NewManager(stores, nil)
	f := &fakeConnector{waitForClose: true}
	m.connector = f.connect

	n1, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "NetOne", Host: "127.0.0.1", Port: 6667, TLS: false, Nick: "a"})
	if err != nil {
		t.Fatal(err)
	}
	n2, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "NetTwo", Host: "127.0.0.1", Port: 6668, TLS: false, Nick: "b"})
	if err != nil {
		t.Fatal(err)
	}

	if err := m.StartNetwork(ctx, n1.ID, NetworkConfig{Name: n1.Name, Servers: []ServerConfig{{Host: "127.0.0.1", Port: 6667}}, Nick: "a", User: "a", Realname: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := m.StartNetwork(ctx, n2.ID, NetworkConfig{Name: n2.Name, Servers: []ServerConfig{{Host: "127.0.0.1", Port: 6668}}, Nick: "b", User: "b", Realname: "b"}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, time.Second, func() bool {
		return f.callCount() >= 2
	}, "manager never attempted both network starts")

	if err := m.StopNetwork(n1.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		state := m.StateSnapshot()
		return state[n1.ID] == StateDisconnected.String() && state[n2.ID] == StateConnecting.String()
	}, "stop network affected wrong runtime state")

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

	m := NewManager(stores, nil)
	f := &fakeConnector{returnErr: errors.New("boom")}
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

	waitFor(t, 2500*time.Millisecond, func() bool {
		return f.callCount() >= 2
	}, "manager never attempted failover to second server")

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
	m := NewManager(stores, nil)
	f := &fakeConnector{returnErr: errors.New("tls handshake failed")}
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
	waitFor(t, time.Second, func() bool {
		return f.callCount() >= 2
	}, "manager never retried with TLS 1.2")
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

	m := NewManager(stores, nil)
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

func waitFor(t *testing.T, timeout time.Duration, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal(msg)
}

type fakeConnector struct {
	mu           sync.Mutex
	calls        []ServerConfig
	returnErr    error
	waitForClose bool
}

func (f *fakeConnector) connect(ctx context.Context, _ *girc.Client, server ServerConfig) error {
	f.mu.Lock()
	f.calls = append(f.calls, server)
	f.mu.Unlock()
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

var _ = sql.ErrNoRows
