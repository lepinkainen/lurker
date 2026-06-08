package irc

// caseFoldNick lowercases an IRC nick using RFC1459 casemapping: in addition
// to ASCII A-Z → a-z, the characters {}|~ are case-equivalent to []\^. Most
// IRC networks (including IRCnet, Libera, OFTC) advertise either "rfc1459"
// or "rfc1459-strict" as their CASEMAPPING. We use the strict variant which
// matches all five pairs — superset-safe for the looser variant.
//
// Use this anywhere nicks are compared, hashed, or used as map keys.
func caseFoldNick(s string) string {
	if s == "" {
		return s
	}
	buf := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			buf[i] = c + 32
		case c == '[':
			buf[i] = '{'
		case c == ']':
			buf[i] = '}'
		case c == '\\':
			buf[i] = '|'
		case c == '^':
			buf[i] = '~'
		default:
			buf[i] = c
		}
	}
	return string(buf)
}
