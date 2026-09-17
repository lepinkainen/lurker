package irc

import (
	"regexp"
	"strings"
)

// ircloudIdentRe matches IRCCloud's synthetic idents, which encode the
// account id directly: "sid<id>" for password-authenticated connections,
// "uid<id>" for username/SASL. A leading "~" (no identd) is stripped by the
// caller before matching.
var ircloudIdentRe = regexp.MustCompile(`^(?:sid|uid)(\d+)$`)

// irccloudAvatarURL derives an avatar URL from an IRCCloud hostmask, without
// requiring any IRCv3 support from the server. IRCCloud bakes the account id
// into the ident (see ircloudIdentRe); resolving it through IRCCloud's own
// avatar-redirect CDN works on plain servers like Libera or OFTC where
// draft/metadata-2 (see avatars.go) is unavailable. This is a pure function,
// consulted as a fallback wherever a metadata avatar isn't already known —
// never stored in avatarTracker, which stays metadata-only.
//
// The returned URL carries a literal "{size}" token (e.g. "s64" once
// substituted) for the "/api/avatar" proxy to fill in; "s" requests a
// square crop, matching the identicon-sized image other avatar sources
// provide.
func irccloudAvatarURL(ident, host string) (string, bool) {
	h := strings.ToLower(host)
	if h != "irccloud.com" && !strings.HasSuffix(h, ".irccloud.com") {
		return "", false
	}
	id := strings.TrimPrefix(ident, "~")
	m := ircloudIdentRe.FindStringSubmatch(id)
	if m == nil {
		return "", false
	}
	return "https://www.irccloud.com/avatar-redirect/s{size}/" + m[1], true
}
