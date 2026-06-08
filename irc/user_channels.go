package irc

import "sort"

// userChannels tracks which channels each known nick is currently in.
// We maintain it ourselves because girc's internal handlers run before
// ours and have already purged the quitting user from state by the time
// onQuit fires — so we can't reach back into girc to find shared channels.
//
// No mutex: girc serializes handler dispatch per-client, and every caller
// runs on that goroutine. The handler.lastMemberListHash field follows the
// same pattern.
//
// Two indices are kept in lockstep:
//   - byNick[caseFoldNick(nick)] → set of channels
//   - byChannel[channel] → set of folded nicks
//
// The reverse index makes dropChannel (self-part / kick) O(K) instead of
// O(N) over the full known-user population.
type userChannels struct {
	byNick    map[string]map[string]struct{}
	byChannel map[string]map[string]struct{}
}

func newUserChannels() *userChannels {
	return &userChannels{
		byNick:    map[string]map[string]struct{}{},
		byChannel: map[string]map[string]struct{}{},
	}
}

func (u *userChannels) addUser(nick, channel string) {
	if u == nil || nick == "" || channel == "" {
		return
	}
	k := caseFoldNick(nick)
	s, ok := u.byNick[k]
	if !ok {
		s = map[string]struct{}{}
		u.byNick[k] = s
	}
	s[channel] = struct{}{}
	cu, ok := u.byChannel[channel]
	if !ok {
		cu = map[string]struct{}{}
		u.byChannel[channel] = cu
	}
	cu[k] = struct{}{}
}

func (u *userChannels) removeUser(nick, channel string) {
	if u == nil || nick == "" || channel == "" {
		return
	}
	k := caseFoldNick(nick)
	if s, ok := u.byNick[k]; ok {
		delete(s, channel)
		if len(s) == 0 {
			delete(u.byNick, k)
		}
	}
	if cu, ok := u.byChannel[channel]; ok {
		delete(cu, k)
		if len(cu) == 0 {
			delete(u.byChannel, channel)
		}
	}
}

// dropUser removes the nick entirely and returns the sorted list of
// channels it was in plus a tracked flag — distinguishes "store missing"
// (u==nil, the caller forgot to init) from "nick legitimately untracked".
func (u *userChannels) dropUser(nick string) (channels []string, tracked bool) {
	if u == nil {
		return nil, false
	}
	if nick == "" {
		return nil, false
	}
	k := caseFoldNick(nick)
	s, ok := u.byNick[k]
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(s))
	for c := range s {
		out = append(out, c)
		if cu, ok := u.byChannel[c]; ok {
			delete(cu, k)
			if len(cu) == 0 {
				delete(u.byChannel, c)
			}
		}
	}
	delete(u.byNick, k)
	sort.Strings(out)
	return out, true
}

func (u *userChannels) renameUser(oldNick, newNick string) {
	if u == nil || oldNick == "" || newNick == "" {
		return
	}
	ok := caseFoldNick(oldNick)
	nk := caseFoldNick(newNick)
	if ok == nk {
		return
	}
	s, exists := u.byNick[ok]
	if !exists {
		return
	}
	delete(u.byNick, ok)
	u.byNick[nk] = s
	for c := range s {
		if cu, ok2 := u.byChannel[c]; ok2 {
			delete(cu, ok)
			cu[nk] = struct{}{}
		}
	}
}

// dropChannel removes the channel from every user's set (used when we
// leave a channel ourselves or are kicked).
func (u *userChannels) dropChannel(channel string) {
	if u == nil || channel == "" {
		return
	}
	cu, ok := u.byChannel[channel]
	if !ok {
		return
	}
	for k := range cu {
		if s, ok2 := u.byNick[k]; ok2 {
			delete(s, channel)
			if len(s) == 0 {
				delete(u.byNick, k)
			}
		}
	}
	delete(u.byChannel, channel)
}

// syncChannel replaces the membership for a channel with the given nicks.
// Called after RPL_ENDOFNAMES to reseed tracking from authoritative state.
func (u *userChannels) syncChannel(channel string, nicks []string) {
	if u == nil || channel == "" {
		return
	}
	want := foldNickSet(nicks)
	u.purgeStale(channel, want)
	u.applyChannelMembership(channel, want)
}

func foldNickSet(nicks []string) map[string]struct{} {
	want := make(map[string]struct{}, len(nicks))
	for _, n := range nicks {
		if n == "" {
			continue
		}
		want[caseFoldNick(n)] = struct{}{}
	}
	return want
}

func (u *userChannels) purgeStale(channel string, want map[string]struct{}) {
	existing, ok := u.byChannel[channel]
	if !ok {
		return
	}
	for k := range existing {
		if _, keep := want[k]; keep {
			continue
		}
		if s, ok2 := u.byNick[k]; ok2 {
			delete(s, channel)
			if len(s) == 0 {
				delete(u.byNick, k)
			}
		}
		delete(existing, k)
	}
}

func (u *userChannels) applyChannelMembership(channel string, want map[string]struct{}) {
	cu, ok := u.byChannel[channel]
	if !ok {
		cu = map[string]struct{}{}
		u.byChannel[channel] = cu
	}
	for k := range want {
		cu[k] = struct{}{}
		s, ok2 := u.byNick[k]
		if !ok2 {
			s = map[string]struct{}{}
			u.byNick[k] = s
		}
		s[channel] = struct{}{}
	}
}
