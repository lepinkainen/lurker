package irc

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/mirc"
	"github.com/lrstanley/girc"
)

func (h *handler) onJoin(c *girc.Client, e girc.Event) {
	channel, ok := channelParam(e)
	if !ok {
		return
	}
	isSelf := c != nil && e.Source != nil && strings.EqualFold(e.Source.Name, c.GetNick())
	if isSelf {
		slog.Info("irc join", "network", h.networkName, "channel", channel, "nick", e.Source.Name)
	}
	h.updateChannelJoined(channel, true, "join", e.Source)
	if isSelf {
		return
	}
	if e.Source != nil {
		h.userChannels.addUser(e.Source.Name, channel)
	}
	h.storeEvent(e, channel, ircdb.BufferChannel, "join", "", "")
}

func (h *handler) onPart(c *girc.Client, e girc.Event) {
	channel, ok := channelParam(e)
	if !ok {
		return
	}
	reason := ""
	if len(e.Params) >= 2 {
		reason = e.Last()
	}
	if c != nil && e.Source != nil && strings.EqualFold(e.Source.Name, c.GetNick()) {
		h.updateChannelJoined(channel, false, "part", e.Source)
		h.userChannels.dropChannel(channel)
	} else {
		h.publishRemotePresence(channel, "part", e.Source)
		if e.Source != nil {
			h.userChannels.removeUser(e.Source.Name, channel)
		}
	}
	h.storeEvent(e, channel, ircdb.BufferChannel, "part", "", reason)
}

func (h *handler) onKick(c *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	channel := e.Params[0]
	kickedNick := e.Params[1]
	reason := ""
	if len(e.Params) >= 3 {
		reason = e.Last()
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure kick buffer", "err", err, "network", h.networkName, "buffer", channel)
	} else {
		if h.hub != nil {
			h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, BufferID: globalBufID, Nick: kickedNick, State: "kick"})
		}
		if c != nil && strings.EqualFold(kickedNick, c.GetNick()) {
			if h.clearMemberListHook != nil {
				h.clearMemberListHook(channel)
			}
			delete(h.lastMemberListHash, channel)
			if h.setJoinedHook != nil {
				h.setJoinedHook(channel, false)
			}
			h.publishBufferUpdate(BufferUpdateEvent{Type: "buffer_update", ID: globalBufID, NetworkID: h.networkID, Joined: ptrTo(false)})
			h.publishMemberList(c, channel)
		}
	}
	if c != nil && strings.EqualFold(kickedNick, c.GetNick()) {
		h.userChannels.dropChannel(channel)
	} else {
		h.userChannels.removeUser(kickedNick, channel)
	}
	// target = the kicked nick
	h.storeEvent(e, channel, ircdb.BufferChannel, "kick", kickedNick, reason)
}

func (h *handler) onTopic(_ *girc.Client, e girc.Event) {
	channel, topic, ok := channelTopic(e)
	if !ok {
		return
	}
	setBy := ""
	if e.Source != nil {
		setBy = e.Source.Name
	}
	setAt := e.Timestamp
	if setAt.IsZero() {
		setAt = time.Now()
	}
	h.updateChannelTopicState(channel, &topic, &setBy, ptrTo(ircdb.FormatTime(setAt)))
	h.storeEvent(e, channel, ircdb.BufferChannel, "topic", "", topic)
}

func (h *handler) onTopicReply(_ *girc.Client, e girc.Event) {
	channel, topic, ok := channelTopic(e)
	if !ok {
		return
	}
	h.updateChannelTopicState(channel, &topic, nil, nil)
}

// onTopicWhoTime handles RPL_TOPICWHOTIME (333): who set the topic and when.
// Params: [me, #chan, setter, unixtime]. Metadata only — no message row.
func (h *handler) onTopicWhoTime(_ *girc.Client, e girc.Event) {
	if len(e.Params) < 4 {
		return
	}
	channel := e.Params[1]
	setBy := e.Params[2]
	// The setter may be a full hostmask in any of girc's source forms
	// (nick, nick!user@host, nick@host) — keep just the nick.
	if src := girc.ParseSource(e.Params[2]); src != nil {
		setBy = src.Name
	}
	unix, err := strconv.ParseInt(e.Params[3], 10, 64)
	if err != nil || unix <= 0 {
		return
	}
	h.updateChannelTopicState(channel, nil, &setBy, ptrTo(ircdb.FormatTime(time.Unix(unix, 0))))
}

func (h *handler) onMode(_ *girc.Client, e girc.Event) {
	target, modeArgs, ok := modeTargetAndArgs(e)
	if !ok {
		return
	}
	kind := "mode"
	if !girc.IsValidChannel(target) {
		// User mode changes go to the network status buffer.
		h.storeEvent(e, "", ircdb.BufferStatus, kind, target, modeArgs)
		return
	}
	h.touchChannelBuffer(target, "ensure mode buffer")
	h.storeEvent(e, target, ircdb.BufferChannel, kind, "", modeArgs)
}

func (h *handler) onChannelModeIs(_ *girc.Client, e girc.Event) {
	target, modeArgs, ok := modeTargetAndArgs(e)
	if !ok || !girc.IsValidChannel(target) {
		return
	}
	h.touchChannelBuffer(target, "ensure channel mode buffer")
	h.storeEvent(e, "", ircdb.BufferStatus, "notice", "", strings.TrimSpace(target+" "+modeArgs))
}
func (h *handler) onEndOfNames(c *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	channel := e.Params[1]
	if c != nil {
		if ch := c.LookupChannel(channel); ch != nil && len(ch.UserList) > 0 {
			// Skip empty UserList: a malformed/empty NAMES reply would
			// otherwise purge every tracked user from this channel.
			h.userChannels.syncChannel(channel, ch.UserList)
		}
	}
	h.publishMemberList(c, channel)
}

// onEndOfWho fires after girc's auto-WHO on join completes. At this point
// user.Extras.Name (realname) is populated, so republish the member list to
// backfill realnames that weren't yet available at RPL_ENDOFNAMES time.
// girc auto-WHOs channels on self-join (target=channel) and individual users
// on remote join (target=nick) — handle both.
func (h *handler) onEndOfWho(c *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	target := e.Params[1]
	if girc.IsValidChannel(target) {
		h.publishMemberList(c, target)
		return
	}
	if c == nil {
		return
	}
	user := c.LookupUser(target)
	if user == nil {
		return
	}
	// girc stores user.ChannelList names RFC1459-folded (see state.go
	// addChannel -> ToRFC1459). publishMemberList -> ensureBuffer does NOT
	// case-fold, so passing a folded name like "#foo" would spawn a phantom
	// second buffer for a channel tracked as "#Foo". Resolve each folded name
	// back to girc's canonical display casing via LookupChannel (which folds
	// its lookup key internally and returns the server-cased Channel.Name).
	for _, channel := range user.ChannelList {
		name := channel
		if ch := c.LookupChannel(channel); ch != nil {
			name = ch.Name
		}
		h.publishMemberList(c, name)
	}
}
func (h *handler) touchChannelBuffer(channel, action string) {
	ctx, cancel := h.eventContext()
	defer cancel()
	if _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel); err != nil {
		slog.Error(action, "err", err, "network", h.networkName, "buffer", channel)
	}
}

func (h *handler) markAllChannelsParted() {
	if h.drainJoinedHook == nil {
		return
	}
	channels := h.drainJoinedHook()
	if len(channels) == 0 {
		return
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	for _, channel := range channels {
		if h.clearMemberListHook != nil {
			h.clearMemberListHook(channel)
		}
		delete(h.lastMemberListHash, channel)
		globalBufID, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
		if err != nil {
			slog.Error("ensure channel buffer", "err", err, "network", h.networkName, "buffer", channel)
			continue
		}
		h.publishBufferUpdate(BufferUpdateEvent{Type: "buffer_update", ID: globalBufID, NetworkID: h.networkID, Joined: ptrTo(false)})
	}
}

func (h *handler) updateChannelJoined(channel string, joined bool, presenceState string, source *girc.Source) {
	if !joined && h.clearMemberListHook != nil {
		h.clearMemberListHook(channel)
	}
	if !joined {
		delete(h.lastMemberListHash, channel)
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure channel buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	if h.setJoinedHook != nil {
		h.setJoinedHook(channel, joined)
	}
	h.publishBufferUpdate(BufferUpdateEvent{Type: "buffer_update", ID: globalBufID, NetworkID: h.networkID, Joined: ptrTo(joined)})
	if h.hub != nil && source != nil {
		h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, BufferID: globalBufID, Nick: source.Name, State: presenceState})
	}
}

// updateChannelTopicState persists any subset of a channel's topic state and
// broadcasts it. Nil fields stay untouched — RPL_TOPIC (332) carries only
// text, RPL_TOPICWHOTIME (333) only setter metadata. A non-nil empty topic is
// a cleared topic and must reach clients as an explicit empty string. Topic
// updates deliberately carry no joined state: a topic reply is not proof of
// membership (servers answer /topic queries for channels we're not in).
func (h *handler) updateChannelTopicState(channel string, topic, setBy, setAt *string) {
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure topic buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	if err := ircdb.UpdateLogBufferTopicState(ctx, h.db, channel, topic, setBy, setAt); err != nil {
		// No publish on failure: clients must never render state that a
		// reload (which reads from the DB) can't reproduce.
		slog.Error("update channel topic state", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	ev := BufferUpdateEvent{Type: "buffer_update", ID: globalBufID, NetworkID: h.networkID, TopicSetBy: setBy, TopicSetAt: setAt}
	if topic != nil {
		// DB keeps the raw topic; the wire carries plain text (clients render
		// topics unstyled, and raw mIRC codes would leak as control chars).
		ev.Topic = ptrTo(mirc.Strip(*topic))
	}
	h.publishBufferUpdate(ev)
}
