package main

import (
	"strings"
)

type wsCmd map[string]any

// slashContext carries everything a slash handler needs.
type slashContext struct {
	buf  *bufferDTO
	args string   // full arg string (after the command word)
	rest []string // arg words
}

type slashHandler func(slashContext) (wsCmd, bool)

var slashRegistry = map[string]slashHandler{
	"me":        slashBufferContent("me"),
	"join":      slashJoin,
	"part":      slashBufferContent("part"),
	"leave":     slashBufferContent("part"),
	"msg":       slashTargetContent("msg"),
	"notice":    slashTargetContent("notice"),
	"nick":      slashNetworkContent("nick"),
	"topic":     slashBufferContent("topic"),
	"whois":     slashNetworkTarget("whois"),
	"query":     slashNetworkTarget("query"),
	"raw":       slashNetworkContent("raw"),
	"quote":     slashNetworkContent("raw"),
	"away":      slashAway,
	"back":      slashBack,
	"quit":      slashQuit,
	"invite":    slashInvite,
	"kick":      slashKick,
	"mode":      slashMode,
	"op":        slashBufferTarget("op"),
	"deop":      slashBufferTarget("deop"),
	"voice":     slashBufferTarget("voice"),
	"devoice":   slashBufferTarget("devoice"),
	"ban":       slashBufferTarget("ban"),
	"unban":     slashBufferTarget("unban"),
	"rejoin":    slashRejoin,
	"cycle":     slashRejoin,
	"list":      slashList,
	"archive":   slashArchive,
	"unarchive": slashUnarchive,
	"delete":    slashDelete,
}

// parseSlash takes a "/cmd args" line and produces the matching ws cmd.
// Returns ok=false if cmd is unknown or required args missing.
func parseSlash(line string, buf *bufferDTO) (wsCmd, bool) {
	if buf == nil || !strings.HasPrefix(line, "/") {
		return nil, false
	}
	parts := strings.Fields(line[1:])
	if len(parts) == 0 {
		return nil, false
	}
	cmd := strings.ToLower(parts[0])
	rest := parts[1:]
	args := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "/"+parts[0]), " "))

	handler, ok := slashRegistry[cmd]
	if !ok {
		return nil, false
	}
	return handler(slashContext{buf: buf, args: args, rest: rest})
}

// ── handler factories ─────────────────────────────────────────────────────────

func slashBufferContent(typ string) slashHandler {
	return func(c slashContext) (wsCmd, bool) {
		if typ == "me" && c.args == "" {
			return nil, false
		}
		return wsCmd{"type": typ, "buffer_id": c.buf.ID, "content": c.args}, true
	}
}

func slashNetworkContent(typ string) slashHandler {
	return func(c slashContext) (wsCmd, bool) {
		if c.args == "" {
			return nil, false
		}
		return wsCmd{"type": typ, "network_id": c.buf.NetworkID, "content": c.args}, true
	}
}

func slashNetworkTarget(typ string) slashHandler {
	return func(c slashContext) (wsCmd, bool) {
		if c.args == "" {
			return nil, false
		}
		return wsCmd{"type": typ, "network_id": c.buf.NetworkID, "target": c.args}, true
	}
}

func slashBufferTarget(typ string) slashHandler {
	return func(c slashContext) (wsCmd, bool) {
		if c.args == "" {
			return nil, false
		}
		return wsCmd{"type": typ, "buffer_id": c.buf.ID, "target": c.args}, true
	}
}

func slashTargetContent(typ string) slashHandler {
	return func(c slashContext) (wsCmd, bool) {
		if len(c.rest) < 2 {
			return nil, false
		}
		target := c.rest[0]
		content := strings.TrimSpace(strings.TrimPrefix(c.args, target))
		return wsCmd{"type": typ, "network_id": c.buf.NetworkID, "target": target, "content": content}, true
	}
}

// ── individual handlers ───────────────────────────────────────────────────────

func slashJoin(c slashContext) (wsCmd, bool) {
	if c.args == "" {
		return nil, false
	}
	return wsCmd{"type": "join", "network_id": c.buf.NetworkID, "channel": c.args}, true
}

func slashAway(c slashContext) (wsCmd, bool) {
	return wsCmd{"type": "away", "network_id": c.buf.NetworkID, "content": c.args}, true
}

func slashBack(c slashContext) (wsCmd, bool) {
	return wsCmd{"type": "back", "network_id": c.buf.NetworkID}, true
}

func slashQuit(c slashContext) (wsCmd, bool) {
	return wsCmd{"type": "quit", "network_id": c.buf.NetworkID, "content": c.args}, true
}

func slashRejoin(c slashContext) (wsCmd, bool) {
	return wsCmd{"type": "rejoin", "buffer_id": c.buf.ID}, true
}

func slashList(c slashContext) (wsCmd, bool) {
	return wsCmd{"type": "list", "network_id": c.buf.NetworkID, "content": c.args}, true
}

func slashMode(c slashContext) (wsCmd, bool) {
	return wsCmd{"type": "mode", "buffer_id": c.buf.ID, "content": c.args}, true
}

func slashInvite(c slashContext) (wsCmd, bool) {
	if len(c.rest) < 1 {
		return nil, false
	}
	target := c.rest[0]
	channel := c.buf.Name
	if len(c.rest) >= 2 {
		channel = c.rest[1]
	}
	return wsCmd{"type": "invite", "network_id": c.buf.NetworkID, "target": target, "channel": channel}, true
}

func slashKick(c slashContext) (wsCmd, bool) {
	if len(c.rest) < 1 {
		return nil, false
	}
	target := c.rest[0]
	reason := strings.TrimSpace(strings.TrimPrefix(c.args, target))
	return wsCmd{"type": "kick", "buffer_id": c.buf.ID, "target": target, "content": reason}, true
}

func slashArchive(c slashContext) (wsCmd, bool) {
	if c.buf.Kind == "status" {
		return nil, false
	}
	return wsCmd{"type": "archive_buffer", "buffer_id": c.buf.ID}, true
}

func slashUnarchive(c slashContext) (wsCmd, bool) {
	if c.buf.Kind == "status" {
		return nil, false
	}
	return wsCmd{"type": "unarchive_buffer", "buffer_id": c.buf.ID}, true
}

func slashDelete(c slashContext) (wsCmd, bool) {
	// Server enforces archived-only too; the local guard keeps misfires
	// silent since the TUI does not surface WS error envelopes yet.
	if c.buf.Kind == "status" || !c.buf.Archived {
		return nil, false
	}
	return wsCmd{"type": "delete_buffer", "buffer_id": c.buf.ID}, true
}
