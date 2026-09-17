package irc

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/lrstanley/girc"
)

// IRCv3 metadata (draft/metadata-2,
// https://ircv3.net/specs/extensions/metadata). girc has no constants or
// handling for these — we only track the "avatar" key, and only for nick
// targets (channel avatar metadata is out of scope for v1).
const (
	// rplWhoisKeyValue (760) carries a metadata key/value pair inline with a
	// WHOIS reply: "<client> <target> <key> <visibility> :<value>".
	rplWhoisKeyValue = "760"
	// rplKeyValue (761) answers METADATA GET/SYNC and is also the push a SUB
	// triggers when a subscribed key changes:
	// "<client> <target> <key> <visibility> :<value>".
	rplKeyValue = "761"
	// rplKeyNotSet (766) reports a key that is absent or was just cleared:
	// "<client> <target> <key> :<message>" — unlike the two numerics above,
	// there is no visibility or value field; the trailing text is a
	// human-readable message, not a value.
	rplKeyNotSet = "766"
	// rplMetadataSyncLater (774) tells us the server deferred a metadata
	// burst (e.g. SUB on connect, or joining a large/throttled channel):
	// "<client> <Target> [<RetryAfter>]". We must re-request it ourselves
	// with "METADATA <Target> SYNC" once RetryAfter (seconds) has elapsed.
	rplMetadataSyncLater = "774"
	// avatarMetadataKey is the only metadata key we track.
	avatarMetadataKey = "avatar"
	// maxMetadataSyncRetry caps how long we'll wait on a server-supplied
	// RetryAfter before issuing the deferred SYNC, so a hostile or broken
	// server can't schedule an absurd timer.
	maxMetadataSyncRetry = 300 * time.Second
)

// onMetadata handles the unsolicited/batched server command carrying a
// metadata value: "METADATA <target> <key> <visibility> :<value>". Unlike
// the numerics below, the command form has no leading client-nick param —
// e.Params[0] is already the target. Servers send this both standalone
// (a live SET by someone we're watching) and wrapped in a BATCH for
// connect-time SYNC pushes; girc has no BATCH buffering of its own (it only
// negotiates the "batch" cap), so every line inside the batch — including
// this one — reaches this handler exactly like a standalone line.
func (h *handler) onMetadata(_ *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	h.applyAvatarMetadata(e.Params[0], e.Params[1], e.Last())
}

// onWhoisKeyValue handles RPL_WHOISKEYVALUE (760).
func (h *handler) onWhoisKeyValue(_ *girc.Client, e girc.Event) {
	h.applyKeyValueNumeric(e)
}

// onKeyValue handles RPL_KEYVALUE (761).
func (h *handler) onKeyValue(_ *girc.Client, e girc.Event) {
	h.applyKeyValueNumeric(e)
}

// applyKeyValueNumeric is shared by 760/761: both carry the client's own
// nick as e.Params[0] (standard numeric-reply layout), then
// "<target> <key> <visibility> :<value>".
func (h *handler) applyKeyValueNumeric(e girc.Event) {
	if len(e.Params) < 3 {
		return
	}
	h.applyAvatarMetadata(e.Params[1], e.Params[2], e.Last())
}

// onKeyNotSet handles RPL_KEYNOTSET (766): "<client> <target> <key>
// :<message>". It always means the key is absent/cleared — there is no
// value field to inspect, so the trailing text is ignored.
func (h *handler) onKeyNotSet(_ *girc.Client, e girc.Event) {
	if len(e.Params) < 3 {
		return
	}
	h.applyAvatarMetadata(e.Params[1], e.Params[2], "")
}

// applyAvatarMetadata is the single funnel for all four metadata wire forms.
// Only the "avatar" key is tracked, and only for nick targets. A non-empty
// value sets the avatar URL; an empty value (or RPL_KEYNOTSET, which never
// carries one) clears it. Publishes an AvatarEvent only when the tracker
// actually changed, so a repeated SYNC push doesn't spam clients.
func (h *handler) applyAvatarMetadata(target, key, value string) {
	if key != avatarMetadataKey || target == "" || girc.IsValidChannel(target) {
		return
	}
	var changed bool
	if value == "" {
		changed = h.avatars.clear(target)
	} else {
		changed = h.avatars.set(target, value)
	}
	if !changed || h.hub == nil {
		return
	}
	h.hub.Publish(&AvatarEvent{
		Type:      "avatar",
		NetworkID: h.networkID,
		Nick:      target,
		HasAvatar: value != "",
	})
}

// onMetadataSyncLater handles RPL_METADATASYNCLATER (774): the server
// deferred a metadata burst (SUB on connect, or joining a large/throttled
// channel) and tells us to re-request it later with "METADATA <target>
// SYNC". Without this, avatars can stay absent indefinitely on large
// channels. A positive RetryAfter schedules the SYNC via time.AfterFunc so
// this handler never blocks the dispatch goroutine; a missing/zero/invalid
// RetryAfter sends immediately. A stale client after reconnect is harmless —
// SendRaw on a dead client just errors — so timers aren't tracked or
// cancelled across reconnects.
func (h *handler) onMetadataSyncLater(c *girc.Client, e girc.Event) {
	target, delay, ok := parseSyncLater(e)
	if !ok {
		return
	}
	send := func() {
		if err := c.Cmd.SendRaw("METADATA " + target + " SYNC"); err != nil {
			slog.Warn("sync deferred metadata", "err", err, "network", h.networkName, "target", target)
		}
	}
	if delay <= 0 {
		send()
		return
	}
	time.AfterFunc(delay, send)
}

// parseSyncLater extracts the target and retry delay from a 774
// RPL_METADATASYNCLATER numeric: "<client> <Target> [<RetryAfter>]".
// e.Params[0] is our own nick (standard numeric-reply layout), so the
// target is e.Params[1] and the optional RetryAfter seconds is
// e.Params[2]. RetryAfter is parsed defensively: absent, empty, or
// non-numeric all mean "send immediately" (delay 0); a valid value is
// clamped to maxMetadataSyncRetry so a hostile/broken server can't schedule
// an absurd timer. ok is false only when the target is missing.
func parseSyncLater(e girc.Event) (target string, delay time.Duration, ok bool) {
	if len(e.Params) < 2 || e.Params[1] == "" {
		return "", 0, false
	}
	target = e.Params[1]
	if len(e.Params) < 3 {
		return target, 0, true
	}
	secs, err := strconv.Atoi(e.Params[2])
	if err != nil || secs <= 0 {
		return target, 0, true
	}
	delay = min(time.Duration(secs)*time.Second, maxMetadataSyncRetry)
	return target, delay, true
}
