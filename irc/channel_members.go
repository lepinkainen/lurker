package irc

import (
	"sort"
	"strings"

	"github.com/lepinkainen/lurker/mirc"
	"github.com/lepinkainen/lurker/nickcolor"
	"github.com/lrstanley/girc"
)

type channelMember struct {
	Nick     string
	Prefix   string
	Realname string
	Away     bool
	Self     bool
	Color    int
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
		displayNick := nick
		if user != nil && user.Nick != "" {
			displayNick = user.Nick
		}
		realname := ""
		if user != nil {
			// Some users put mIRC color/formatting codes in their realname;
			// clients render it as plain text, so strip codes server-side.
			realname = strings.TrimSpace(mirc.Strip(user.Extras.Name))
		}
		members = append(members, channelMember{
			Nick:     displayNick,
			Prefix:   channelMemberPrefix(user, channel),
			Realname: realname,
			Away:     user != nil && user.Extras.Away != "",
			Self:     strings.EqualFold(displayNick, selfNick),
			Color:    nickcolor.Index(displayNick),
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
