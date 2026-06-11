package irc

import (
	"regexp"
	"sync/atomic"
)

// compiledHighlights is an immutable snapshot of the user's global highlight
// patterns. patterns[i] corresponds to regexes[i].
type compiledHighlights struct {
	patterns []string
	regexes  []*regexp.Regexp
}

var highlightSet atomic.Pointer[compiledHighlights]

// SetHighlightPatterns replaces the global highlight pattern set. Each
// pattern is matched as a case-insensitive word-boundary literal (same shape
// as nick mention matching). Patterns that fail to compile are skipped.
func SetHighlightPatterns(patterns []string) {
	set := &compiledHighlights{}
	for _, p := range patterns {
		re, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(p) + `\b`)
		if err != nil {
			continue
		}
		set.patterns = append(set.patterns, p)
		set.regexes = append(set.regexes, re)
	}
	highlightSet.Store(set)
}

// matchHighlight returns the first configured pattern that matches content.
func matchHighlight(content string) (string, bool) {
	set := highlightSet.Load()
	if set == nil {
		return "", false
	}
	for i, re := range set.regexes {
		if re.MatchString(content) {
			return set.patterns[i], true
		}
	}
	return "", false
}
