package irc

import (
	"strings"
	"time"
)

// Netsplit grouping tunables. Mirrored in web/src/netsplit.ts — keep in sync.
const (
	NetsplitQuitWindow   = 60 * time.Second
	NetsplitRejoinWindow = 30 * time.Minute
	NetsplitMinQuits     = 2
)

// IsNetsplitReason reports whether content matches an IRC netsplit QUIT
// reason: exactly two whitespace-separated hostname-shaped tokens. Both
// tokens must contain at least one dot, differ from each other.
func IsNetsplitReason(content string) (a, b string, ok bool) {
	if content == "" {
		return "", "", false
	}
	parts := strings.Split(content, " ")
	if len(parts) != 2 {
		return "", "", false
	}
	a, b = parts[0], parts[1]
	if a == "" || b == "" {
		return "", "", false
	}
	if a == b {
		return "", "", false
	}
	if !looksLikeHost(a) || !looksLikeHost(b) {
		return "", "", false
	}
	return a, b, true
}

func looksLikeHost(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	if !strings.Contains(s, ".") {
		return false
	}
	// Restrict to LDH (letter-digit-hyphen) + dot, plus colon for IPv6
	// literals. Rejects punctuation that real netsplit reasons never
	// contain (`!`, `*`, `?`, `@`, `:` inside non-IPv6 — the colon check
	// here is permissive on purpose). Without this restriction, "foo!bar
	// baz.qux" would parse as a hostname pair.
	hasAlpha := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			hasAlpha = true
		case c >= '0' && c <= '9', c == '.', c == '-', c == ':':
			// allowed
		default:
			return false
		}
	}
	return hasAlpha
}

// PresenceEntry is one stored presence event used for grouping.
type PresenceEntry struct {
	ID      string
	Kind    string // "join" | "part" | "quit" | "nick"
	Sender  string
	Content string
	TS      time.Time
}

// PresenceGroup is one rendered group: either a plain run of presence
// events or a collapsed netsplit.
type PresenceGroup struct {
	Netsplit *NetsplitGroup
	Plain    []PresenceEntry
}

// NetsplitGroup collapses a server-split quit burst (and matched rejoins)
// into a single renderable group.
type NetsplitGroup struct {
	ServerA string
	ServerB string
	Quits   []PresenceEntry
	Rejoins []PresenceEntry
	SplitTS time.Time
	HealTS  time.Time // zero if no rejoins seen
}

type nsCluster struct {
	serverA, serverB string
	key              string
	quitIdx          []int
	startTS          time.Time
	nicks            map[string]struct{}
	rejoinIdx        []int
	healTS           time.Time
}

// GroupPresence walks presence events in timestamp order and collapses
// netsplits (≥NetsplitMinQuits quits sharing a hostname-pair reason within
// NetsplitQuitWindow) plus matched rejoins (joins by the same nicks within
// NetsplitRejoinWindow after the first quit) into single groups.
func GroupPresence(events []PresenceEntry) []PresenceGroup {
	if len(events) == 0 {
		return nil
	}
	clusterFor := make([]int, len(events))
	for i := range clusterFor {
		clusterFor[i] = -1
	}
	clusters := assignQuitClusters(events, clusterFor)
	keep := dropSmallClusters(clusters, clusterFor)
	matchRejoins(events, clusters, keep, clusterFor)
	return emitGroups(events, clusters, clusterFor)
}

func assignQuitClusters(events []PresenceEntry, clusterFor []int) []*nsCluster {
	var clusters []*nsCluster
	openByKey := map[string]*nsCluster{}
	for i, ev := range events {
		if ev.Kind != "quit" {
			continue
		}
		a, b, ok := IsNetsplitReason(ev.Content)
		if !ok {
			continue
		}
		key := netsplitKey(a, b)
		c, exists := openByKey[key]
		if exists && ev.TS.Sub(c.startTS) > NetsplitQuitWindow {
			delete(openByKey, key)
			exists = false
		}
		if !exists {
			c = &nsCluster{serverA: a, serverB: b, key: key, startTS: ev.TS, nicks: map[string]struct{}{}}
			clusters = append(clusters, c)
			openByKey[key] = c
		}
		c.quitIdx = append(c.quitIdx, i)
		c.nicks[caseFoldNick(ev.Sender)] = struct{}{}
		clusterFor[i] = len(clusters) - 1
	}
	return clusters
}

func dropSmallClusters(clusters []*nsCluster, clusterFor []int) []bool {
	keep := make([]bool, len(clusters))
	for ci, c := range clusters {
		keep[ci] = len(c.quitIdx) >= NetsplitMinQuits
	}
	for i, ci := range clusterFor {
		if ci >= 0 && !keep[ci] {
			clusterFor[i] = -1
		}
	}
	return keep
}

func matchRejoins(events []PresenceEntry, clusters []*nsCluster, keep []bool, clusterFor []int) {
	for i, ev := range events {
		if ev.Kind != "join" {
			continue
		}
		nick := caseFoldNick(ev.Sender)
		bestIdx := bestClusterForJoin(ev.TS, nick, clusters, keep)
		if bestIdx < 0 {
			continue
		}
		c := clusters[bestIdx]
		delete(c.nicks, nick)
		c.rejoinIdx = append(c.rejoinIdx, i)
		if ev.TS.After(c.healTS) {
			c.healTS = ev.TS
		}
		clusterFor[i] = bestIdx
	}
}

func bestClusterForJoin(ts time.Time, nick string, clusters []*nsCluster, keep []bool) int {
	bestIdx := -1
	var bestStart time.Time
	for ci, c := range clusters {
		if !keep[ci] {
			continue
		}
		if ts.Sub(c.startTS) > NetsplitRejoinWindow || ts.Before(c.startTS) {
			continue
		}
		if _, ok := c.nicks[nick]; !ok {
			continue
		}
		if bestIdx < 0 || c.startTS.After(bestStart) {
			bestIdx = ci
			bestStart = c.startTS
		}
	}
	return bestIdx
}

func emitGroups(events []PresenceEntry, clusters []*nsCluster, clusterFor []int) []PresenceGroup {
	var out []PresenceGroup
	var plain []PresenceEntry
	emitted := make([]bool, len(clusters))
	flushPlain := func() {
		if len(plain) == 0 {
			return
		}
		out = append(out, PresenceGroup{Plain: plain})
		plain = nil
	}
	for i, ev := range events {
		ci := clusterFor[i]
		if ci < 0 {
			plain = append(plain, ev)
			continue
		}
		if emitted[ci] {
			continue
		}
		flushPlain()
		out = append(out, PresenceGroup{Netsplit: buildNetsplit(events, clusters[ci])})
		emitted[ci] = true
	}
	flushPlain()
	return out
}

func buildNetsplit(events []PresenceEntry, c *nsCluster) *NetsplitGroup {
	ns := &NetsplitGroup{ServerA: c.serverA, ServerB: c.serverB, SplitTS: c.startTS, HealTS: c.healTS}
	for _, qi := range c.quitIdx {
		ns.Quits = append(ns.Quits, events[qi])
	}
	for _, ri := range c.rejoinIdx {
		ns.Rejoins = append(ns.Rejoins, events[ri])
	}
	return ns
}

func netsplitKey(a, b string) string {
	if a < b {
		return a + " " + b
	}
	return b + " " + a
}
