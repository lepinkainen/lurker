package irc

import "unicode/utf8"

// ensureUTF8 returns s unchanged if it is already valid UTF-8. Otherwise it
// reinterprets the raw bytes as ISO-8859-1 (each byte maps directly to the
// matching Unicode code point). This recovers messages from IRC clients that
// still send Latin-1.
func ensureUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	runes := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		runes[i] = rune(s[i])
	}
	return string(runes)
}
