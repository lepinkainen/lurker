// Package preview extracts URLs from IRC message content, fetches their
// metadata, and caches the result in a shared SQLite store.
package preview

import "regexp"

// urlRegexp matches what the frontend linkify does: http(s)://… terminated by
// whitespace or an angle bracket. Trailing punctuation is trimmed separately.
var urlRegexp = regexp.MustCompile(`https?://[^\s<>"']+`)

// trailingPunct holds bytes that are almost never part of a URL and are
// commonly attached by sentence punctuation.
const trailingPunct = ".,;:!?)]}>"

// ExtractURLs returns de-duplicated URLs in the order they appear in content.
// Trailing sentence punctuation is trimmed so `https://foo/bar.` becomes
// `https://foo/bar`.
func ExtractURLs(content string) []string {
	matches := urlRegexp.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, u := range matches {
		u = trimTrailing(u)
		if u == "" {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func trimTrailing(u string) string {
	for u != "" && isTrailing(u[len(u)-1]) {
		u = u[:len(u)-1]
	}
	return u
}

func isTrailing(b byte) bool {
	for i := range len(trailingPunct) {
		if trailingPunct[i] == b {
			return true
		}
	}
	return false
}
