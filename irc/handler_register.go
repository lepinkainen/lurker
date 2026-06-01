package irc

import "github.com/lrstanley/girc"

func (h *handler) register(c *girc.Client) {
	c.Handlers.Add(girc.CONNECTED, h.onConnected)
	c.Handlers.Add(girc.DISCONNECTED, h.onDisconnected)
	c.Handlers.Add(girc.PRIVMSG, h.onPrivmsg)
	c.Handlers.Add(girc.NOTICE, h.onPrivmsg) // same handling, different kind
	c.Handlers.Add(girc.JOIN, h.onJoin)
	c.Handlers.Add(girc.PART, h.onPart)
	c.Handlers.Add(girc.KICK, h.onKick)
	c.Handlers.Add(girc.TOPIC, h.onTopic)
	c.Handlers.Add(girc.RPL_TOPIC, h.onTopicReply)
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
	c.Handlers.Add(girc.RPL_LIST, h.onRPLList)
	c.Handlers.Add(girc.RPL_LISTEND, h.onRPLListEnd)
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
	if isExplicitlyHandledEvent(e.Command) {
		return
	}
	h.onUnhandledEvent(e)
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
		girc.RPL_LISTEND,
		girc.RPL_NAMREPLY,
		girc.RPL_WHOREPLY,
		girc.RPL_ENDOFWHO,
		girc.PING,
		girc.PONG:
		return true
	default:
		return false
	}
}
