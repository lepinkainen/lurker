package irc

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// NetsplitMeta is the wire annotation identifying which collapsed netsplit a
// quit/join message belongs to. Clients group messages by ID instead of
// re-deriving netsplit clustering themselves.
type NetsplitMeta struct {
	ID      string `json:"id"`
	ServerA string `json:"server_a"`
	ServerB string `json:"server_b"`
}

// netsplitMetaID derives a stable group id from the server pair and the
// stored timestamp of the cluster's first quit. Both the live tracker and
// the serve-time recompute (api history/state) use stored millisecond
// timestamps, so the same cluster gets the same id on every path.
func netsplitMetaID(key string, splitTS time.Time) string {
	return fmt.Sprintf("%s|%d", key, splitTS.UnixMilli())
}

// MetaForNetsplit builds the wire annotation for a serve-time computed
// group (see GroupPresence).
func MetaForNetsplit(g *NetsplitGroup) NetsplitMeta {
	return NetsplitMeta{
		ID:      netsplitMetaID(netsplitKey(g.ServerA, g.ServerB), g.SplitTS),
		ServerA: g.ServerA,
		ServerB: g.ServerB,
	}
}

// netsplitTracker incrementally clusters live quit/join events per buffer so
// published messages carry their netsplit annotation. It mirrors the batch
// rules in GroupPresence: quits sharing a hostname-pair reason within
// NetsplitQuitWindow form a cluster, a cluster is a netsplit once it has
// NetsplitMinQuits quits, and joins by the same nicks within
// NetsplitRejoinWindow count as rejoins.
type netsplitTracker struct {
	mu       sync.Mutex
	clusters map[uuid.UUID]map[string]*liveNetsplit // bufferID -> pair key -> cluster
}

type liveNetsplit struct {
	meta    NetsplitMeta
	startTS time.Time
	nicks   map[string]struct{}
	// pending holds quit message ids stored before the cluster reached
	// NetsplitMinQuits; they were published without annotation and need a
	// retroactive netsplit event once the cluster qualifies.
	pending []uuid.UUID
	quits   int
}

func newNetsplitTracker() *netsplitTracker {
	return &netsplitTracker{clusters: map[uuid.UUID]map[string]*liveNetsplit{}}
}

func (c *liveNetsplit) qualified() bool { return c.quits >= NetsplitMinQuits }

// OnQuit records a stored quit message. meta is non-nil when the message
// belongs to a qualified netsplit (annotate the outgoing event); retro holds
// previously-published message ids that need a retroactive netsplit event
// (non-empty exactly on the quit that qualifies the cluster).
func (t *netsplitTracker) OnQuit(bufferID, msgID uuid.UUID, sender, reason string, ts time.Time) (meta *NetsplitMeta, retro []uuid.UUID) {
	a, b, ok := IsNetsplitReason(reason)
	if !ok {
		return nil, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := netsplitKey(a, b)
	perBuf := t.clusters[bufferID]
	if perBuf == nil {
		perBuf = map[string]*liveNetsplit{}
		t.clusters[bufferID] = perBuf
	}
	c := perBuf[key]
	if c != nil && ts.Sub(c.startTS) > NetsplitQuitWindow {
		c = nil
	}
	if c == nil {
		c = &liveNetsplit{
			meta:    NetsplitMeta{ID: netsplitMetaID(key, ts), ServerA: a, ServerB: b},
			startTS: ts,
			nicks:   map[string]struct{}{},
		}
		perBuf[key] = c
	}
	c.quits++
	c.nicks[caseFoldNick(sender)] = struct{}{}
	if !c.qualified() {
		c.pending = append(c.pending, msgID)
		return nil, nil
	}
	retro = c.pending
	c.pending = nil
	m := c.meta
	return &m, retro
}

// OnJoin matches a stored join message against open netsplits in the buffer.
// Returns the annotation when the join is a rejoin, mirroring
// bestClusterForJoin: newest qualifying cluster wins, and a nick rejoins a
// given cluster at most once.
func (t *netsplitTracker) OnJoin(bufferID uuid.UUID, sender string, ts time.Time) *NetsplitMeta {
	t.mu.Lock()
	defer t.mu.Unlock()
	perBuf := t.clusters[bufferID]
	if len(perBuf) == 0 {
		return nil
	}
	nick := caseFoldNick(sender)
	var best *liveNetsplit
	for key, c := range perBuf {
		if ts.Sub(c.startTS) > NetsplitRejoinWindow {
			delete(perBuf, key) // expired; no join can ever match again
			continue
		}
		if !c.qualified() || ts.Before(c.startTS) {
			continue
		}
		if _, ok := c.nicks[nick]; !ok {
			continue
		}
		if best == nil || c.startTS.After(best.startTS) {
			best = c
		}
	}
	if best == nil {
		return nil
	}
	delete(best.nicks, nick)
	m := best.meta
	return &m
}

// parseStoredTS parses a stored message timestamp (db.FormatTime layout,
// RFC3339 with milliseconds). Falls back to fallback on parse failure so a
// malformed row degrades to "not part of a netsplit" instead of panicking.
func parseStoredTS(s string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(s)); err == nil {
		return t
	}
	return fallback
}
