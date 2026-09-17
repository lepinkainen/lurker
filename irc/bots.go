package irc

import "sync"

// botTracker records which nicks on a network have been identified as bots
// per the IRCv3 bot-mode spec (https://ircv3.net/specs/extensions/bot-mode).
//
// Bot status is not part of girc's tracked user state, so we accumulate it
// ourselves from the three sources the spec defines:
//   - the ISUPPORT BOT mode character appearing in WHO/WHOX reply flags
//   - RPL_WHOISBOT (335) in a WHOIS response
//   - the `bot` message tag on an incoming PRIVMSG/NOTICE
//
// Unlike userChannels this needs a mutex: girc serializes handler dispatch,
// but Manager.ChannelMembers reads the set from HTTP request goroutines.
type botTracker struct {
	mu    sync.RWMutex
	nicks map[string]struct{}
}

func newBotTracker() *botTracker {
	return &botTracker{nicks: map[string]struct{}{}}
}

// mark records the nick as a bot and reports whether that was a change, so
// callers only republish member lists when something actually flipped.
func (b *botTracker) mark(nick string) bool {
	if b == nil || nick == "" {
		return false
	}
	k := caseFoldNick(nick)
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.nicks[k]; ok {
		return false
	}
	b.nicks[k] = struct{}{}
	return true
}

// unmark clears bot status, reporting whether the nick was previously known
// as a bot. Used when a WHO reply shows the mode is gone, and on QUIT.
func (b *botTracker) unmark(nick string) bool {
	if b == nil || nick == "" {
		return false
	}
	k := caseFoldNick(nick)
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.nicks[k]; !ok {
		return false
	}
	delete(b.nicks, k)
	return true
}

// set applies a definitive bot/not-bot observation (a WHO reply carries both
// outcomes) and reports whether it changed anything.
func (b *botTracker) set(nick string, bot bool) bool {
	if bot {
		return b.mark(nick)
	}
	return b.unmark(nick)
}

func (b *botTracker) isBot(nick string) bool {
	if b == nil || nick == "" {
		return false
	}
	k := caseFoldNick(nick)
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.nicks[k]
	return ok
}

func (b *botTracker) rename(oldNick, newNick string) {
	if b == nil || oldNick == "" || newNick == "" {
		return
	}
	ok := caseFoldNick(oldNick)
	nk := caseFoldNick(newNick)
	if ok == nk {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.nicks[ok]; !exists {
		return
	}
	delete(b.nicks, ok)
	b.nicks[nk] = struct{}{}
}
