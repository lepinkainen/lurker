package irc

import (
	"log/slog"
	"strings"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lrstanley/girc"
)

func (h *handler) onQuit(_ *girc.Client, e girc.Event) {
	// QUIT doesn't name the channels the user was in; we log it to the
	// status buffer for now. Per-channel fan-out is a later enhancement.
	h.storeEvent(e, "", ircdb.BufferStatus, "quit", "", e.Last())
}

func (h *handler) onNick(c *girc.Client, e girc.Event) {
	newNick := e.Last()
	if c != nil && e.Source != nil && strings.EqualFold(e.Source.Name, c.GetNick()) && h.connectedHook != nil {
		h.connectedHook(newNick)
	}
	if h.hub != nil && e.Source != nil {
		h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, Nick: e.Source.Name, State: "nick", Target: newNick})
	}
	h.storeEvent(e, "", ircdb.BufferStatus, "nick", newNick, "")
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
	h.storeEvent(e, "", ircdb.BufferStatus, state, e.Source.Name, message)
}

func (h *handler) onAccount(_ *girc.Client, e girc.Event) {
	if e.Source == nil {
		return
	}
	account := e.Last()
	if account == "*" {
		account = ""
	}
	h.storeEvent(e, "", ircdb.BufferStatus, "account", e.Source.Name, account)
}

func (h *handler) onChghost(_ *girc.Client, e girc.Event) {
	if e.Source == nil || len(e.Params) < 2 {
		return
	}
	h.storeEvent(e, "", ircdb.BufferStatus, "chghost", e.Source.Name, strings.Join(e.Params[:2], " "))
}
func (h *handler) publishRemotePresence(channel, state string, source *girc.Source) {
	if h.hub == nil || source == nil {
		return
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
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
	globalBufID, _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure names buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	out := make([]ChannelUser, 0, len(members))
	for _, member := range members {
		out = append(out, ChannelUser(member))
	}
	h.hub.Publish(&MemberListEvent{Type: "member_list", NetworkID: h.networkID, BufferID: globalBufID, Channel: channel, Members: out})
}
