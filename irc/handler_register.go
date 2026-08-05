package irc

import "github.com/lrstanley/girc"

func (h *handler) register(c *girc.Client) {
	h.client = c
	c.Handlers.Add(girc.CONNECTED, h.onConnected)
	c.Handlers.Add(girc.DISCONNECTED, h.onDisconnected)
	c.Handlers.Add(girc.PRIVMSG, h.onPrivmsg)
	c.Handlers.Add(girc.NOTICE, h.onPrivmsg) // same handling, different kind
	c.Handlers.Add(girc.JOIN, h.onJoin)
	c.Handlers.Add(girc.PART, h.onPart)
	c.Handlers.Add(girc.KICK, h.onKick)
	c.Handlers.Add(girc.TOPIC, h.onTopic)
	c.Handlers.Add(girc.RPL_TOPIC, h.onTopicReply)
	c.Handlers.Add(girc.RPL_TOPICWHOTIME, h.onTopicWhoTime)
	c.Handlers.Add(girc.RPL_NOTOPIC, h.onNoTopicReply)
	c.Handlers.Add(girc.MODE, h.onMode)
	c.Handlers.Add(girc.RPL_CHANNELMODEIS, h.onChannelModeIs)
	c.Handlers.Add(girc.INVITE, h.onInvite)
	c.Handlers.Add(girc.QUIT, h.onQuit)
	c.Handlers.Add(girc.NICK, h.onNick)
	c.Handlers.Add(girc.CAP_AWAY, h.onAway)
	c.Handlers.Add(girc.CAP_ACCOUNT, h.onAccount)
	c.Handlers.Add(girc.CAP_CHGHOST, h.onChghost)
	c.Handlers.Add(girc.RPL_ENDOFNAMES, h.onEndOfNames)
	c.Handlers.Add(girc.RPL_ENDOFWHO, h.onEndOfWho)
	// IRCv3 bot mode: girc tracks neither WHO flags nor the 335 numeric.
	c.Handlers.Add(girc.RPL_WHOREPLY, h.onWhoBotReply)
	c.Handlers.Add(girc.RPL_WHOSPCRPL, h.onWhoxBotReply)
	c.Handlers.Add(rplWhoisBot, h.onWhoisBot)
	c.Handlers.Add(girc.UPDATE_STATE, h.onStateUpdate)
	c.Handlers.Add(girc.RPL_LIST, h.onRPLList)
	c.Handlers.Add(girc.RPL_LISTEND, h.onRPLListEnd)
	c.Handlers.Add("BATCH", h.onBatch)
	// echo-message: girc routes our own PRIVMSG/NOTICE echoes only through
	// ALL_EVENTS. Catch them here and feed the normal persistence path so
	// outbound messages land in history with the server-assigned msgid.
	c.Handlers.Add(girc.ALL_EVENTS, h.onAllEvent)
}

func (h *handler) onAllEvent(c *girc.Client, e girc.Event) {
	if e.Echo {
		if e.Command == girc.PRIVMSG || e.Command == girc.NOTICE {
			h.onPrivmsg(c, e)
		}
		return
	}
	if e.Command == girc.MODE {
		h.queueModeMemberRefresh(e)
	}
	if isExplicitlyHandledEvent(e.Command) {
		return
	}
	h.onUnhandledEvent(e)
}

// queueModeMemberRefresh runs from ALL_EVENTS, which girc completes before
// dispatching the MODE-specific handlers. This guarantees the affected
// channel is queued before girc updates its tracked user permissions and
// emits UPDATE_STATE.
func (h *handler) queueModeMemberRefresh(e girc.Event) {
	target, _, ok := modeTargetAndArgs(e)
	if !ok || !girc.IsValidChannel(target) {
		return
	}
	h.modeMemberRefreshMu.Lock()
	if h.modeMemberRefresh == nil {
		h.modeMemberRefresh = make(map[string]struct{})
	}
	h.modeMemberRefresh[target] = struct{}{}
	h.modeMemberRefreshMu.Unlock()
}

// onStateUpdate republishes MODE-affected member lists only after girc has
// applied the mode to User.Perms. Publishing directly from onMode would race
// girc because its internal and external MODE handlers run concurrently.
func (h *handler) onStateUpdate(c *girc.Client, _ girc.Event) {
	h.modeMemberRefreshMu.Lock()
	channels := h.modeMemberRefresh
	h.modeMemberRefresh = nil
	h.modeMemberRefreshMu.Unlock()

	for channel := range channels {
		h.publishMemberList(c, channel)
	}
}

func isExplicitlyHandledEvent(command string) bool {
	switch command {
	case girc.CONNECTED,
		girc.DISCONNECTED,
		girc.PRIVMSG,
		girc.NOTICE,
		girc.JOIN,
		girc.PART,
		girc.KICK,
		girc.TOPIC,
		girc.RPL_TOPIC,
		girc.RPL_TOPICWHOTIME,
		girc.RPL_NOTOPIC,
		girc.RPL_CREATIONTIME,
		girc.MODE,
		girc.RPL_CHANNELMODEIS,
		girc.INVITE,
		girc.QUIT,
		girc.NICK,
		girc.CAP_AWAY,
		girc.CAP_ACCOUNT,
		girc.CAP_CHGHOST,
		girc.RPL_ENDOFNAMES,
		girc.RPL_LIST,
		girc.RPL_LISTSTART,
		girc.RPL_LISTEND,
		girc.RPL_NAMREPLY,
		girc.RPL_WHOREPLY,
		girc.RPL_WHOSPCRPL,
		girc.RPL_ENDOFWHO,
		rplWhoisBot,
		girc.PING,
		girc.PONG:
		return true
	default:
		return false
	}
}
