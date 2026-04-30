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
	c.Handlers.Add("322", h.onRPLList)    // RPL_LIST
	c.Handlers.Add("323", h.onRPLListEnd) // RPL_LIST end
	// echo-message: girc routes our own PRIVMSG/NOTICE echoes only through
	// ALL_EVENTS. Catch them here and feed the normal persistence path so
	// outbound messages land in history with the server-assigned msgid.
	c.Handlers.Add(girc.ALL_EVENTS, func(c *girc.Client, e girc.Event) {
		if e.Echo {
			if e.Command == girc.PRIVMSG || e.Command == girc.NOTICE {
				h.onPrivmsg(c, e)
			}
			return
		}
		h.onUnhandledEvent(e)
	})
}
