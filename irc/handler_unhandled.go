package irc

import (
	"strings"
	"unicode/utf8"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lrstanley/girc"
)

func (h *handler) onUnhandledEvent(e girc.Event) {
	if isSyntheticClientEvent(e.Command) {
		return
	}
	if isProtocolPlumbingEvent(e.Command) {
		return
	}
	if motdNumeric(e.Command) {
		h.storeEvent(e, "", ircdb.BufferStatus, "notice", "", formatMOTDLine(e))
		return
	}
	kind := unhandledEventKind(e.Command)
	bufName, bufKind := "", ircdb.BufferStatus
	if kind == "error" {
		if channel, ok := unhandledEventChannel(e); ok {
			bufName, bufKind = channel, ircdb.BufferChannel
		}
	}
	h.storeEvent(e, bufName, bufKind, kind, "", formatUnhandledEventContent(e, bufKind == ircdb.BufferChannel))
}
func isSyntheticClientEvent(command string) bool {
	return strings.HasPrefix(command, "CLIENT_") || strings.HasPrefix(command, "STS_")
}

// isProtocolPlumbingEvent filters IRCv3 machinery that carries no
// human-readable content: TAGMSG (typing indicators, reactions) and BATCH
// framing markers. Storing them as status notices is pure noise.
func isProtocolPlumbingEvent(command string) bool {
	return command == girc.CAP_TAGMSG || command == "BATCH"
}

func unhandledEventKind(command string) string {
	if len(command) == 3 && command[0] >= '4' && command[0] <= '5' {
		return "error"
	}
	return "notice"
}

func unhandledEventChannel(e girc.Event) (string, bool) {
	if len(e.Params) < 2 {
		return "", false
	}
	channel := e.Params[1]
	if !girc.IsValidChannel(channel) {
		return "", false
	}
	return channel, true
}

func formatUnhandledEventContent(e girc.Event, dropChannel bool) string {
	params := e.Params
	if len(params) > 0 {
		params = params[1:]
	}
	if dropChannel && len(params) > 0 && girc.IsValidChannel(params[0]) {
		params = params[1:]
	}
	text := strings.TrimSpace(strings.Join(params, " "))
	// Channel-targeted errors keep their stripped human form. Status-buffer
	// rows include the raw command (numeric or verb) so unknown server
	// responses are debuggable from history.
	if dropChannel {
		if text == "" {
			return e.Command
		}
		return text
	}
	if text == "" {
		return e.Command
	}
	return e.Command + " " + text
}

// motdNumeric reports whether command is one of the three MOTD numerics
// (start, body line, end) that formatMOTDLine renders as a box.
func motdNumeric(command string) bool {
	switch command {
	case girc.RPL_MOTDSTART, girc.RPL_MOTD, girc.RPL_ENDOFMOTD:
		return true
	default:
		return false
	}
}

// formatMOTDLine renders a single MOTD numeric as one line of a C-shaped
// box, hiding the raw numeric code. Stateless and per-line: each call only
// sees the one event, so the box shape comes purely from which numeric it
// is, not from tracking start/end state across calls.
func formatMOTDLine(e girc.Event) string {
	text := strings.TrimSpace(e.Last())
	if text == "-" {
		text = ""
	} else if rest, ok := strings.CutPrefix(text, "- "); ok {
		text = strings.TrimSpace(rest)
	}
	switch e.Command {
	case girc.RPL_MOTDSTART:
		return motdRule("╭─ Message of the Day ")
	case girc.RPL_ENDOFMOTD:
		return motdRule("╰─ End of MOTD ")
	default: // girc.RPL_MOTD
		if text == "" {
			return "│"
		}
		return "│ " + text
	}
}

// motdRule pads label with box-drawing dashes to a fixed visible width,
// counting runes (not bytes) since the box characters are multi-byte UTF-8.
func motdRule(label string) string {
	const width = 44
	n := max(width-utf8.RuneCountInString(label), 3)
	return label + strings.Repeat("─", n)
}
