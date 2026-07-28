package irc

import (
	"hash/fnv"
	"log/slog"
	"strings"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lrstanley/girc"
)

func (h *handler) onQuit(c *girc.Client, e girc.Event) {
	if e.Source == nil {
		h.storeEvent(e, "", ircdb.BufferStatus, "quit", "", e.Last())
		return
	}
	reason := e.Last()
	nick := e.Source.Name
	// Self-quit: girc's built-in handler returns early and never reaches us
	// in production, but guard anyway. Use nickFn (the canonical current
	// nick) when girc.Client is nil — test harnesses pass nil here.
	if h.isSelfNick(c, nick) {
		h.storeEvent(e, "", ircdb.BufferStatus, "quit", "", reason)
		return
	}
	channels, tracked := h.userChannels.dropUser(nick)
	if !tracked {
		// Unknown user (or userChannels store missing) — keep the
		// status-buffer record for ops visibility.
		h.storeEvent(e, "", ircdb.BufferStatus, "quit", "", reason)
		return
	}
	for _, channel := range channels {
		h.publishRemotePresence(channel, "quit", e.Source)
		h.storeEvent(e, channel, ircdb.BufferChannel, "quit", "", reason)
	}
}

func (h *handler) isSelfNick(c *girc.Client, nick string) bool {
	if c != nil && strings.EqualFold(nick, c.GetNick()) {
		return true
	}
	if h.nickFn != nil {
		if own := h.nickFn(); own != "" && strings.EqualFold(nick, own) {
			return true
		}
	}
	return false
}

func (h *handler) onNick(c *girc.Client, e girc.Event) {
	newNick := e.Last()
	if c != nil && e.Source != nil && strings.EqualFold(e.Source.Name, c.GetNick()) && h.connectedHook != nil {
		h.connectedHook(newNick)
	}
	if h.hub != nil && e.Source != nil {
		h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, Nick: e.Source.Name, State: "nick", Target: newNick})
	}
	if e.Source == nil {
		h.storeEvent(e, "", ircdb.BufferStatus, "nick", newNick, "")
		return
	}
	// Snapshot shared channels before the rename rewrites the index.
	channels, tracked := h.userChannels.channelsFor(e.Source.Name)
	h.userChannels.renameUser(e.Source.Name, newNick)
	h.fanOutPresence(e, channels, tracked, "nick", newNick, "")
}

func (h *handler) onInvite(_ *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	h.touchChannelBuffer(e.Params[1], "ensure invite buffer")
	h.storeEvent(e, e.Params[1], ircdb.BufferChannel, "invite", e.Params[0], "")
}

func (h *handler) onAway(_ *girc.Client, e girc.Event) {
	if e.Source == nil {
		return
	}
	state := "back"
	message := ""
	if e.Last() != "" {
		state = "away"
		message = e.Last()
	}
	if h.hub != nil {
		h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, Nick: e.Source.Name, State: state})
	}
	channels, tracked := h.userChannels.channelsFor(e.Source.Name)
	h.fanOutPresence(e, channels, tracked, state, e.Source.Name, message)
}

func (h *handler) onAccount(_ *girc.Client, e girc.Event) {
	if e.Source == nil {
		return
	}
	account := e.Last()
	if account == "*" {
		account = ""
	}
	channels, tracked := h.userChannels.channelsFor(e.Source.Name)
	h.fanOutPresence(e, channels, tracked, "account", e.Source.Name, account)
}

func (h *handler) onChghost(_ *girc.Client, e girc.Event) {
	if e.Source == nil || len(e.Params) < 2 {
		return
	}
	channels, tracked := h.userChannels.channelsFor(e.Source.Name)
	h.fanOutPresence(e, channels, tracked, "chghost", e.Source.Name, strings.Join(e.Params[:2], " "))
}

// fanOutPresence writes a presence-ish event (away/back/account/chghost/nick)
// to every channel the source shares with us, mirroring the QUIT fan-out.
// Untracked nicks fall back to the status buffer so nothing is silently lost.
func (h *handler) fanOutPresence(e girc.Event, channels []string, tracked bool, kind, target, content string) {
	if !tracked || len(channels) == 0 {
		h.storeEvent(e, "", ircdb.BufferStatus, kind, target, content)
		return
	}
	for _, channel := range channels {
		h.storeEvent(e, channel, ircdb.BufferChannel, kind, target, content)
	}
}
func (h *handler) publishRemotePresence(channel, state string, source *girc.Source) {
	if h.hub == nil || source == nil {
		return
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure presence buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, BufferID: globalBufID, Nick: source.Name, State: state})
}
func (h *handler) publishMemberList(c *girc.Client, channel string) {
	if c == nil || channel == "" {
		return
	}
	members := buildChannelMembers(c, channel)
	if members == nil {
		return
	}
	if h.memberListHook != nil {
		h.memberListHook(channel)
	}
	if h.hub == nil {
		return
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure names buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	if h.memberListUnchanged(channel, members) {
		return
	}
	h.hub.Publish(&MemberListEvent{Type: "member_list", NetworkID: h.networkID, BufferID: globalBufID, Channel: channel, Members: members})
}

func (h *handler) memberListUnchanged(channel string, members []ChannelUser) bool {
	hash := hashMemberList(members)
	if h.lastMemberListHash == nil {
		h.lastMemberListHash = map[string]uint64{}
	}
	if prev, ok := h.lastMemberListHash[channel]; ok && prev == hash {
		return true
	}
	h.lastMemberListHash[channel] = hash
	return false
}

func hashMemberList(members []ChannelUser) uint64 {
	h := fnv.New64a()
	var sep = []byte{0}
	for _, m := range members {
		_, _ = h.Write([]byte(m.Nick))
		_, _ = h.Write(sep)
		_, _ = h.Write([]byte(m.Prefix))
		_, _ = h.Write(sep)
		_, _ = h.Write([]byte(m.Realname))
		_, _ = h.Write(sep)
		if m.Away {
			_, _ = h.Write([]byte{1})
		} else {
			_, _ = h.Write([]byte{0})
		}
		if m.Self {
			_, _ = h.Write([]byte{1})
		} else {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write(sep)
	}
	return h.Sum64()
}
