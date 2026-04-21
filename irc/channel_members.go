package irc

import (
	"sort"
	"strings"

	"github.com/lrstanley/girc"
)

type channelMember struct {
	Nick   string
	Prefix string
	Away   bool
	Self   bool
}

func buildChannelMembers(c *girc.Client, channel string) []channelMember {
	if c == nil || channel == "" {
		return nil
	}
	ch := c.LookupChannel(channel)
	if ch == nil {
		return nil
	}
	members := make([]channelMember, 0, len(ch.UserList))
	selfNick := c.GetNick()
	for _, nick := range ch.UserList {
		user := c.LookupUser(nick)
		members = append(members, channelMember{
			Nick:   nick,
			Prefix: channelMemberPrefix(user, channel),
			Away:   user != nil && user.Extras.Away != "",
			Self:   strings.EqualFold(nick, selfNick),
		})
	}
	sort.Slice(members, func(i, j int) bool { return strings.ToLower(members[i].Nick) < strings.ToLower(members[j].Nick) })
	return members
}

func channelMemberPrefix(user *girc.User, channel string) string {
	if user == nil {
		return ""
	}
	perms, ok := user.Perms.Lookup(channel)
	if !ok {
		return ""
	}
	switch {
	case perms.Owner:
		return "~"
	case perms.Admin:
		return "&"
	case perms.Op:
		return "@"
	case perms.HalfOp:
		return "%"
	case perms.Voice:
		return "+"
	default:
		return ""
	}
}
