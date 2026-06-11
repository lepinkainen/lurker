package irc

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
)

// LoadFixtureRuntimeState derives a static IRC runtime snapshot from the
// seeded SQLite data. It is intended for browser/integration tests that run a
// real HTTP backend without live IRC sockets.
func (m *Manager) LoadFixtureRuntimeState(ctx context.Context) error {
	nets, err := ircdb.ListNetworks(ctx, m.stores.Control)
	if err != nil {
		return err
	}
	bufs, err := m.stores.ListAllBuffers(ctx)
	if err != nil {
		return err
	}

	nicks := map[uuid.UUID]string{}
	states := map[uuid.UUID]string{}
	joined := map[uuid.UUID]map[string]bool{}
	members := map[uuid.UUID]map[string][]ChannelUser{}
	for _, n := range nets {
		nicks[n.ID] = n.Nick
		states[n.ID] = StateConnected.String()
		if n.Disabled {
			states[n.ID] = StateDisconnected.String()
		}
	}
	for _, b := range bufs {
		if b.Kind != ircdb.BufferChannel || states[b.NetworkID] != StateConnected.String() {
			continue
		}
		if joined[b.NetworkID] == nil {
			joined[b.NetworkID] = map[string]bool{}
		}
		joined[b.NetworkID][b.Name] = true
		if members[b.NetworkID] == nil {
			members[b.NetworkID] = map[string][]ChannelUser{}
		}
		members[b.NetworkID][b.Name] = m.fixtureMembersForChannel(ctx, b.ID, nicks[b.NetworkID])
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for id, state := range states {
		m.state[id] = state
	}
	m.joined = joined
	m.fixtureMembers = members
	return nil
}

func (m *Manager) fixtureMembersForChannel(ctx context.Context, bufferID uuid.UUID, selfNick string) []ChannelUser {
	seen := map[string]bool{}
	if selfNick != "" {
		seen[strings.ToLower(selfNick)] = true
	}
	msgs, err := m.stores.RecentMessages(ctx, bufferID, 100)
	if err == nil {
		for _, msg := range msgs {
			if msg.Sender == "" || msg.Sender == "*" {
				continue
			}
			seen[strings.ToLower(msg.Sender)] = true
		}
	}

	out := make([]ChannelUser, 0, len(seen))
	for nick := range seen {
		member := ChannelUser{Nick: nick, Self: strings.EqualFold(nick, selfNick)}
		if member.Self {
			member.Nick = selfNick
			member.Prefix = "+"
		}
		out = append(out, member)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Nick) < strings.ToLower(out[j].Nick) })
	return out
}
