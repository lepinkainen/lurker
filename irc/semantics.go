package irc

import (
	"regexp"
	"strings"
	"sync"

	"github.com/lepinkainen/lurker/nickcolor"
)

// MessageSemantics is the IRC-level classification of a stored message,
// derived from kind/sender/content plus the network's current nick. It is
// the single source of truth for cross-client display semantics.
// SenderColor/TargetColor are nickcolor palette indexes (nil when the field
// isn't a nick): TargetColor is only set for kick/nick events, the kinds
// whose target clients render as a colored nick.
type MessageSemantics struct {
	DisplayKind    string `json:"display_kind"`
	IsSelf         bool   `json:"is_self"`
	MentionsMe     bool   `json:"mentions_me"`
	CountsAsUnread bool   `json:"counts_as_unread"`
	SenderColor    *int   `json:"sender_color,omitempty"`
	TargetColor    *int   `json:"target_color,omitempty"`
	// Highlight is set when content matches a user-defined highlight
	// pattern (see SetHighlightPatterns); HighlightPattern names the
	// first pattern that matched. Self-authored messages never highlight.
	Highlight        bool   `json:"highlight,omitzero"`
	HighlightPattern string `json:"highlight_pattern,omitzero"`
}

// nickTargetKinds are the event kinds whose target field holds a nick.
var nickTargetKinds = map[string]struct{}{"kick": {}, "nick": {}}

var sysKinds = map[string]struct{}{
	"join": {}, "part": {}, "quit": {}, "nick": {}, "kick": {}, "mode": {},
	"topic": {}, "connected": {}, "disconnected": {},
	"away": {}, "back": {}, "account": {}, "chghost": {},
}

// presenceKinds is the subset of sysKinds that represent user-presence
// changes (the events the UI collapses into "X joined / quit / changed
// nick" runs). Mirror in web/src/messages.ts PRESENCE_KINDS.
var presenceKinds = map[string]struct{}{
	"join": {}, "part": {}, "quit": {}, "nick": {},
	"away": {}, "back": {}, "account": {}, "chghost": {},
}

// IsPresenceKind reports whether the given event kind is a presence event
// eligible for run-collapsing in the UI. Exported for client renderers
// (cmd/tui) so the kind set has one source of truth.
func IsPresenceKind(kind string) bool {
	_, ok := presenceKinds[kind]
	return ok
}

// kinds that should NOT count as unread activity. Mirrors the historical
// frontend list in messages.ts so behavior is consistent.
var nonUnreadKinds = map[string]struct{}{
	"join": {}, "part": {}, "quit": {}, "nick": {}, "mode": {}, "kick": {},
	"connecting": {}, "connected": {}, "disconnected": {}, "error": {},
	"away": {}, "back": {}, "account": {}, "chghost": {}, "status": {},
}

// classifyKind maps an IRC event kind to a coarse display category.
func classifyKind(kind string) string {
	if _, ok := sysKinds[kind]; ok {
		return "sys"
	}
	switch kind {
	case "notice":
		return "notice"
	case "action":
		return "action"
	case "ctcp":
		return "ctcp"
	}
	return "message"
}

// countsAsUnread returns whether a kind contributes to a buffer's unread
// activity counter. Synthetic events (joins/parts/etc.) do not.
func countsAsUnread(kind string) bool {
	_, blocked := nonUnreadKinds[kind]
	return !blocked
}

var (
	mentionCacheMu sync.RWMutex
	mentionCache   = map[string]*regexp.Regexp{}
)

func mentionRegexp(nick string) *regexp.Regexp {
	mentionCacheMu.RLock()
	re := mentionCache[nick]
	mentionCacheMu.RUnlock()
	if re != nil {
		return re
	}
	pattern := `(?i)\b` + regexp.QuoteMeta(nick) + `\b`
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	mentionCacheMu.Lock()
	mentionCache[nick] = compiled
	mentionCacheMu.Unlock()
	return compiled
}

// ComputeMessageSemantics derives display flags for a stored message. nick
// is the network's current nickname; pass "" when unknown (mention/self
// detection are then both false). target is the message's target field, used
// only for nick-color annotation on kick/nick events. countsAsUnread is
// suppressed for self-authored messages and for the active buffer in the
// caller; this function returns the raw "would count" value.
func ComputeMessageSemantics(kind, sender, content, target, nick string) MessageSemantics {
	out := MessageSemantics{
		DisplayKind:    classifyKind(kind),
		CountsAsUnread: countsAsUnread(kind),
	}
	if sender != "" {
		idx := nickcolor.Index(sender)
		out.SenderColor = &idx
	}
	if target != "" {
		if _, ok := nickTargetKinds[kind]; ok {
			idx := nickcolor.Index(target)
			out.TargetColor = &idx
		}
	}
	if nick != "" && sender != "" && strings.EqualFold(sender, nick) {
		out.IsSelf = true
	}
	if nick != "" && content != "" {
		if re := mentionRegexp(nick); re != nil && re.MatchString(content) {
			out.MentionsMe = true
		}
	}
	if content != "" && !out.IsSelf {
		if pattern, ok := matchHighlight(content); ok {
			out.Highlight = true
			out.HighlightPattern = pattern
		}
	}
	return out
}
