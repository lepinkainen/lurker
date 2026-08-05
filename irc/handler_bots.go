package irc

import (
	"strings"

	"github.com/lrstanley/girc"
)

// IRCv3 bot mode (https://ircv3.net/specs/extensions/bot-mode).
const (
	// rplWhoisBot is the WHOIS numeric a server sends for a bot. girc has no
	// constant for it.
	rplWhoisBot = "335"
	// botWhoxQueryType tags the WHOX queries we send ourselves so the replies
	// are distinguishable from girc's own (which uses "1") and from
	// girc.Client.Cmd.Who() (which uses "2").
	botWhoxQueryType = "9"
	// botTag / botTagDraft are the message tags a server may attach to
	// commands originating from a bot. Values are meaningless and ignored.
	botTag      = "bot"
	botTagDraft = "draft/bot"
)

// botModeChar returns the user-mode character this server uses for bot mode,
// or "" when the server does not advertise the ISUPPORT BOT token.
func botModeChar(c *girc.Client) string {
	if c == nil {
		return ""
	}
	value, ok := c.GetServerOption("BOT")
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	if len(value) != 1 {
		return ""
	}
	return value
}

// requestBotWho asks for the bot flag of a channel's members (or a single
// nick). girc's own auto-WHO requests "%tacuhnr" — no flags field — so bot
// status never reaches us through it. Only sent when the server advertises
// BOT, so networks without bot mode see no extra traffic.
func (h *handler) requestBotWho(c *girc.Client, target string) {
	if c == nil || target == "" || botModeChar(c) == "" {
		return
	}
	if _, ok := c.GetServerOption("WHOX"); ok {
		// Fields are returned in the spec's canonical order, so "%tnf" yields
		// <client> <querytype> <nick> <flags>.
		c.Send(&girc.Event{Command: girc.WHO, Params: []string{target, "%tnf," + botWhoxQueryType}})
		return
	}
	c.Send(&girc.Event{Command: girc.WHO, Params: []string{target}})
}

// onWhoxBotReply handles RPL_WHOSPCRPL for our own bot query. This is the
// only source authoritative enough to clear bot status, since we know the
// flags field was actually requested.
func (h *handler) onWhoxBotReply(c *girc.Client, e girc.Event) {
	if len(e.Params) != 4 || e.Params[1] != botWhoxQueryType {
		return
	}
	h.applyBotFlags(c, e.Params[2], e.Params[3], true)
}

// onWhoBotReply handles plain RPL_WHOREPLY:
// "<client> <channel> <user> <host> <server> <nick> <flags> :<hops> <realname>".
func (h *handler) onWhoBotReply(c *girc.Client, e girc.Event) {
	if len(e.Params) < 7 {
		return
	}
	h.applyBotFlags(c, e.Params[5], e.Params[6], false)
}

// applyBotFlags records bot status from a WHO flags field. definitive marks
// replies to our own "%tnf" query, where the absence of the mode character
// genuinely means "not a bot"; for any other WHO reply we can't tell whether
// the server simply omitted it, so we only ever set the flag.
func (h *handler) applyBotFlags(c *girc.Client, nick, flags string, definitive bool) {
	mode := botModeChar(c)
	if mode == "" || nick == "" {
		return
	}
	isBot := strings.Contains(stripAwayFlag(flags), mode)
	if !isBot && !definitive {
		return
	}
	// Member lists are republished from onEndOfWho, which fires after the
	// whole batch of replies — no need to publish per nick here.
	h.bots.set(nick, isBot)
}

// stripAwayFlag drops the leading H/G away indicator so a network using "H"
// or "G" as its BOT mode character can't self-match on it.
func stripAwayFlag(flags string) string {
	if flags != "" && (flags[0] == 'H' || flags[0] == 'G') {
		return flags[1:]
	}
	return flags
}

// onWhoisBot handles RPL_WHOISBOT (335): "<client> <nick> :is a bot".
func (h *handler) onWhoisBot(c *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	h.markBot(c, e.Params[1])
}

// noteBotTag records bot status from the `bot` message tag, which servers
// attach to every command from a bot when message-tags is negotiated. This is
// the fallback for networks whose WHO replies we never see (queries, or
// channels joined before the ISUPPORT BOT token arrived).
func (h *handler) noteBotTag(e girc.Event) {
	if e.Source == nil || e.Source.Name == "" {
		return
	}
	if _, ok := e.Tags.Get(botTag); !ok {
		if _, ok := e.Tags.Get(botTagDraft); !ok {
			return
		}
	}
	h.markBot(h.client, e.Source.Name)
}

// markBot flags a nick as a bot and republishes the member list of every
// channel it shares with us, so open clients swap the identicon immediately.
func (h *handler) markBot(c *girc.Client, nick string) {
	if !h.bots.mark(nick) {
		return
	}
	if c == nil {
		return
	}
	channels, tracked := h.userChannels.channelsFor(nick)
	if !tracked {
		return
	}
	for _, channel := range channels {
		h.publishMemberList(c, channel)
	}
}
