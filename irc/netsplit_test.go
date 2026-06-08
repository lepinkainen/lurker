package irc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Shared fixture loaded by Go and TS (web/tests/netsplit.test.ts) so the
// two implementations cannot silently drift on accepted/rejected reasons
// or casefold behavior.
func TestSharedFixture(t *testing.T) {
	path := filepath.Join("..", "testdata", "netsplit", "cases.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx struct {
		Reason []struct {
			In   string `json:"in"`
			OK   bool   `json:"ok"`
			A, B string
		} `json:"reason"`
		Casemap []struct {
			In  string `json:"in"`
			Out string `json:"out"`
		} `json:"casemap"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	for _, c := range fx.Reason {
		a, b, ok := IsNetsplitReason(c.In)
		if ok != c.OK || a != c.A || b != c.B {
			t.Errorf("reason %q: got (%q,%q,%v) want (%q,%q,%v)", c.In, a, b, ok, c.A, c.B, c.OK)
		}
	}
	for _, c := range fx.Casemap {
		if got := caseFoldNick(c.In); got != c.Out {
			t.Errorf("casefold %q: got %q want %q", c.In, got, c.Out)
		}
	}
}

func TestIsNetsplitReason(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		a, b string
	}{
		{"irc.cc.tut.fi irc2.inet.fi", true, "irc.cc.tut.fi", "irc2.inet.fi"},
		{"hub.example.org leaf.example.net", true, "hub.example.org", "leaf.example.net"},
		{"Ping timeout: 240 seconds", false, "", ""},
		{"Client Quit", false, "", ""},
		{"Quit: bye", false, "", ""},
		{"", false, "", ""},
		{"a.b c.d e.f", false, "", ""},
		{"foo bar", false, "", ""},
		{"a.b a.b", false, "", ""},
		{" a.b c.d", false, "", ""},
		{"a.b c.d ", false, "", ""},
		{"a.b\tc.d", false, "", ""},
		{"foo!bar baz.qux.example", false, "", ""},
		{"*.net *.split", false, "", ""},
		{"-.example.org x.example.org", true, "-.example.org", "x.example.org"},
		{"123.example.org x.example.org", true, "123.example.org", "x.example.org"},
		{"2001::1 fe80::1", false, "", ""}, // IPv6 literals (no dots) not supported; real netsplit reasons use hostnames
	}
	for _, c := range cases {
		a, b, ok := IsNetsplitReason(c.in)
		if ok != c.ok || a != c.a || b != c.b {
			t.Errorf("IsNetsplitReason(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, a, b, ok, c.a, c.b, c.ok)
		}
	}
}

func ts(sec int) time.Time { return time.Unix(int64(sec), 0).UTC() }

func quitEv(id, sender, reason string, sec int) PresenceEntry {
	return PresenceEntry{ID: id, Kind: "quit", Sender: sender, Content: reason, TS: ts(sec)}
}
func joinEv(id, sender string, sec int) PresenceEntry {
	return PresenceEntry{ID: id, Kind: "join", Sender: sender, TS: ts(sec)}
}

func TestGroupPresence_NetsplitBasic(t *testing.T) {
	reason := "irc.cc.tut.fi irc2.inet.fi"
	in := []PresenceEntry{
		quitEv("1", "alice", reason, 100),
		quitEv("2", "bob", reason, 102),
		quitEv("3", "carol", reason, 105),
		joinEv("4", "bob", 300),
		joinEv("5", "alice", 305),
	}
	groups := GroupPresence(in)
	if len(groups) != 1 || groups[0].Netsplit == nil {
		t.Fatalf("want 1 netsplit group, got %+v", groups)
	}
	ns := groups[0].Netsplit
	if len(ns.Quits) != 3 || len(ns.Rejoins) != 2 {
		t.Fatalf("quits=%d rejoins=%d", len(ns.Quits), len(ns.Rejoins))
	}
	if ns.ServerA != "irc.cc.tut.fi" || ns.ServerB != "irc2.inet.fi" {
		t.Errorf("servers=%q,%q", ns.ServerA, ns.ServerB)
	}
}

func TestGroupPresence_BelowMinFallsBack(t *testing.T) {
	reason := "a.example.org b.example.org"
	in := []PresenceEntry{quitEv("1", "alice", reason, 100)}
	groups := GroupPresence(in)
	if len(groups) != 1 || groups[0].Netsplit != nil || len(groups[0].Plain) != 1 {
		t.Fatalf("want 1 plain, got %+v", groups)
	}
}

func TestGroupPresence_NonNetsplitQuitStaysPlain(t *testing.T) {
	in := []PresenceEntry{
		quitEv("1", "alice", "Ping timeout: 240 seconds", 100),
		quitEv("2", "bob", "Ping timeout: 240 seconds", 101),
	}
	groups := GroupPresence(in)
	if len(groups) != 1 || groups[0].Netsplit != nil || len(groups[0].Plain) != 2 {
		t.Fatalf("want plain group of 2, got %+v", groups)
	}
}

func TestGroupPresence_PartialRejoin(t *testing.T) {
	reason := "a.example.org b.example.org"
	in := []PresenceEntry{
		quitEv("1", "alice", reason, 100),
		quitEv("2", "bob", reason, 102),
		quitEv("3", "carol", reason, 105),
		joinEv("4", "alice", 200),
	}
	groups := GroupPresence(in)
	if len(groups) != 1 || groups[0].Netsplit == nil {
		t.Fatalf("want netsplit, got %+v", groups)
	}
	ns := groups[0].Netsplit
	if len(ns.Rejoins) != 1 {
		t.Errorf("rejoins=%d want 1", len(ns.Rejoins))
	}
}

func TestGroupPresence_OverlappingSplits(t *testing.T) {
	reason1 := "a.example.org b.example.org"
	reason2 := "c.example.org d.example.org"
	in := []PresenceEntry{
		quitEv("1", "alice", reason1, 100),
		quitEv("2", "xavier", reason2, 101),
		quitEv("3", "bob", reason1, 102),
		quitEv("4", "yara", reason2, 103),
	}
	groups := GroupPresence(in)
	netsplitCount := 0
	for _, g := range groups {
		if g.Netsplit != nil {
			netsplitCount++
		}
	}
	if netsplitCount != 2 {
		t.Fatalf("netsplit count=%d want 2; groups=%+v", netsplitCount, groups)
	}
}

func TestGroupPresence_JoinOutsideRejoinWindow(t *testing.T) {
	reason := "a.example.org b.example.org"
	in := []PresenceEntry{
		quitEv("1", "alice", reason, 100),
		quitEv("2", "bob", reason, 102),
		joinEv("3", "alice", 100+int(NetsplitRejoinWindow.Seconds())+10),
	}
	groups := GroupPresence(in)
	if len(groups) != 2 {
		t.Fatalf("want 2 groups (netsplit + plain join), got %+v", groups)
	}
	if groups[0].Netsplit == nil || len(groups[0].Netsplit.Rejoins) != 0 {
		t.Errorf("group0 not netsplit-with-no-rejoin: %+v", groups[0])
	}
	if groups[1].Netsplit != nil || len(groups[1].Plain) != 1 {
		t.Errorf("group1 not plain join: %+v", groups[1])
	}
}

func TestGroupPresence_QuitWindowSplit(t *testing.T) {
	reason := "a.example.org b.example.org"
	in := []PresenceEntry{
		quitEv("1", "alice", reason, 100),
		quitEv("2", "bob", reason, 110),
		quitEv("3", "carol", reason, 500), // outside QuitWindow
		quitEv("4", "dave", reason, 510),
	}
	groups := GroupPresence(in)
	ns := 0
	for _, g := range groups {
		if g.Netsplit != nil {
			ns++
		}
	}
	if ns != 2 {
		t.Fatalf("want 2 netsplit groups, got %+v", groups)
	}
}
