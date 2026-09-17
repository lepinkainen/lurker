package irc

import "sync"

// avatarTracker records the avatar URL other users have published via IRCv3
// metadata (draft/metadata-2, https://ircv3.net/specs/extensions/metadata)
// under the "avatar" key. The URL itself stays server-side (a later task
// proxies it); we only ever hand out a boolean, so the map is not part of
// any wire response.
//
// Unlike userChannels this needs a mutex: girc serializes handler dispatch,
// but Manager.ChannelMembers and Manager.AvatarURL read the map from HTTP
// request goroutines.
type avatarTracker struct {
	mu   sync.RWMutex
	urls map[string]string
}

func newAvatarTracker() *avatarTracker {
	return &avatarTracker{urls: map[string]string{}}
}

// set records nick's avatar URL and reports whether that changed anything,
// so callers only publish an AvatarEvent on a real transition.
func (a *avatarTracker) set(nick, url string) bool {
	if a == nil || nick == "" || url == "" {
		return false
	}
	k := caseFoldNick(nick)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.urls[k] == url {
		return false
	}
	a.urls[k] = url
	return true
}

// clear removes nick's avatar URL and reports whether it was previously set.
func (a *avatarTracker) clear(nick string) bool {
	if a == nil || nick == "" {
		return false
	}
	k := caseFoldNick(nick)
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.urls[k]; !ok {
		return false
	}
	delete(a.urls, k)
	return true
}

// get returns nick's avatar URL, if any.
func (a *avatarTracker) get(nick string) (string, bool) {
	if a == nil || nick == "" {
		return "", false
	}
	k := caseFoldNick(nick)
	a.mu.RLock()
	defer a.mu.RUnlock()
	url, ok := a.urls[k]
	return url, ok
}

// has reports whether nick has a known avatar URL.
func (a *avatarTracker) has(nick string) bool {
	_, ok := a.get(nick)
	return ok
}

// rename moves oldNick's avatar entry to newNick, if any, and reports
// whether an entry was actually moved (false on any no-op: nil tracker,
// empty nick, same case-folded nick, or no entry under oldNick).
func (a *avatarTracker) rename(oldNick, newNick string) bool {
	if a == nil || oldNick == "" || newNick == "" {
		return false
	}
	ok := caseFoldNick(oldNick)
	nk := caseFoldNick(newNick)
	if ok == nk {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	url, exists := a.urls[ok]
	if !exists {
		return false
	}
	delete(a.urls, ok)
	a.urls[nk] = url
	return true
}
