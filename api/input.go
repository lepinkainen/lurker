package api

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrEmptyInput indicates the input verb received no content to dispatch.
var ErrEmptyInput = errors.New("input requires content")

// ErrUnknownSlashCommand indicates the input started with "/" but the
// command name is not in the registered set.
var ErrUnknownSlashCommand = errors.New("unknown slash command")

// parseInput maps a free-form user input line ("/join #foo" or plain text)
// to the structured clientCmd that the existing WS dispatcher already
// understands. The active buffer context (id, network_id, name, kind) is
// provided by the caller; the parser does no IRC I/O of its own.
//
// Returns the command to dispatch, or an error if the input is malformed.
// Plain (non-slash) text becomes a "send" command bound to the active
// buffer.
func parseInput(content string, buffer inputBuffer) (clientCmd, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return clientCmd{}, ErrEmptyInput
	}
	if !strings.HasPrefix(content, "/") {
		return clientCmd{Type: "send", BufferID: buffer.ID, Content: content}, nil
	}
	body := content[1:]
	name, args := splitFirstSpace(body)
	name = strings.ToLower(name)
	args = strings.TrimSpace(args)
	rest := splitFields(args)

	cmd := clientCmd{NetworkID: buffer.NetworkID, BufferID: buffer.ID}
	switch name {
	case "join":
		cmd.Type = "join"
		cmd.Channel = args
	case "part":
		cmd.Type = "part"
		cmd.Content = args
	case "msg":
		target, msg := splitFirstSpace(args)
		cmd.Type = "msg"
		cmd.Target = target
		cmd.Content = msg
	case "me":
		cmd.Type = "me"
		cmd.Content = args
	case "nick":
		cmd.Type = "nick"
		cmd.Content = args
	case "topic":
		cmd.Type = "topic"
		cmd.Content = args
	case "whois":
		cmd.Type = "whois"
		cmd.Target = args
	case "invite":
		target, channel := splitFirstSpace(args)
		cmd.Type = "invite"
		cmd.Target = target
		if channel == "" {
			channel = buffer.Name
		}
		cmd.Channel = channel
	case "kick":
		target, reason := splitFirstSpace(args)
		cmd.Type = "kick"
		cmd.Target = target
		cmd.Content = reason
	case "mode":
		cmd.Type = "mode"
		cmd.Content = args
	case "raw":
		cmd.Type = "raw"
		cmd.Content = args
	case "away":
		cmd.Type = "away"
		cmd.Content = args
	case "back":
		cmd.Type = "back"
	case "quit":
		cmd.Type = "quit"
		cmd.Content = args
	case "rejoin", "cycle":
		cmd.Type = "rejoin"
	case "notice":
		target, msg := splitFirstSpace(args)
		cmd.Type = "notice"
		cmd.Target = target
		cmd.Content = msg
	case "ctcp":
		// Form: /ctcp <nick> <CMD> [args]
		if len(rest) < 2 {
			return clientCmd{}, errors.New("ctcp requires <nick> <command>")
		}
		cmd.Type = "ctcp"
		cmd.Target = rest[0]
		cmd.Content = strings.Join(rest[1:], " ")
	case "query":
		cmd.Type = "query"
		cmd.Target = args
	case "list":
		cmd.Type = "list"
		cmd.Content = args
	case "op", "deop", "voice", "devoice", "ban", "unban":
		cmd.Type = name
		cmd.Target = args
	case "banlist":
		cmd.Type = "banlist"
	case "kickban":
		target, reason := splitFirstSpace(args)
		cmd.Type = "kickban"
		cmd.Target = target
		cmd.Content = reason
	case "ignore":
		cmd.Type = "ignore"
		cmd.Target = args
	case "unignore":
		cmd.Type = "unignore"
		cmd.Target = args
	case "ignorelist":
		cmd.Type = "ignorelist"
	default:
		return clientCmd{}, ErrUnknownSlashCommand
	}
	return cmd, nil
}

// inputBuffer is the minimal active-buffer context the parser needs to
// resolve relative commands like /part or /invite without explicit args.
type inputBuffer struct {
	ID        uuid.UUID
	NetworkID uuid.UUID
	Name      string
}

// splitFirstSpace splits on the first run of whitespace. Trailing parts
// keep internal spacing intact ("/msg foo hello  world" -> "hello  world").
func splitFirstSpace(s string) (head, tail string) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return "", ""
	}
	idx := strings.IndexAny(s, " \t")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], strings.TrimLeft(s[idx:], " \t")
}

func splitFields(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}
