package irc

import (
	"log/slog"
	"time"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lrstanley/girc"
)

func (h *handler) onPrivmsg(_ *girc.Client, e girc.Event) {
	// Replayed history goes through the backfill path: buffer from the
	// batch target, no live publish, chronology-preserving row IDs.
	if ref, target, ok := h.chathistoryDivert(e); ok {
		h.storeBackfillMessage(e, ref, target)
		return
	}
	var bufName, bufferKind string
	switch {
	case e.IsFromChannel():
		bufName = e.Params[0]
		bufferKind = ircdb.BufferChannel
	default:
		if e.Command == girc.NOTICE && (e.Source == nil || e.Source.Ident == "") {
			// Server notice (no userhost) — route to network status buffer.
			bufferKind = ircdb.BufferStatus
		} else {
			if e.Source != nil {
				bufName = e.Source.Name
			}
			bufferKind = ircdb.BufferQuery
		}
	}
	kind, content := privmsgKindContent(e)
	h.storeEvent(e, bufName, bufferKind, kind, "", content)
}

// ignoreLevel returns the configured ignore level ("", hide, or mute) for
// the given nick.
func (h *handler) ignoreLevel(nick string) string {
	if h.stores == nil {
		return ""
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	entries, err := ircdb.ListIgnores(ctx, h.stores.Control, h.networkID)
	if err != nil || len(entries) == 0 {
		return ""
	}
	return ircdb.IgnoreLevelFor(entries, nick)
}

// storeEvent is the single funnel for inbound IRC events. It upserts the
// target buffer, writes a messages row, and publishes hub events so
// WebSocket clients can render in real time.
func (h *handler) storeEvent(e girc.Event, bufName, bufKind, kind, target, content string) {
	content = ensureUTF8(content)
	sender := ""
	userhost := ""
	if e.Source != nil {
		sender = e.Source.Name
		if e.Source.Ident != "" {
			userhost = e.Source.Ident + "@" + e.Source.Host
		}
	}
	muted := false
	if sender != "" && sender != "*" {
		switch h.ignoreLevel(sender) {
		case ircdb.IgnoreLevelHide:
			return
		case ircdb.IgnoreLevelMute:
			muted = true
		}
	}
	h.noteBotTag(e)
	msgID, _ := e.Tags.Get("msgid")
	account, _ := e.Tags.Get("account")
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	raw := e.String()

	ctx, cancel := h.eventContext()
	defer cancel()

	bufID, err := h.ensureBuffer(ctx, bufName, bufKind)
	if err != nil {
		slog.Error("ensure buffer", "err", err, "network", h.networkName, "buffer", bufName)
		return
	}
	// New activity in an archived query resurfaces the conversation
	// (IRCCloud behavior); persistent spam is handled by buffer deletion.
	if bufKind == ircdb.BufferQuery {
		if h.syncBufferArchived(ctx, bufID, false) {
			h.publishBufferUpdate(BufferUpdateEvent{Type: "buffer_update", ID: bufID, NetworkID: h.networkID, Archived: new(false)})
		}
	}
	id, storedTS, inserted, err := ircdb.InsertLogMessage(ctx, h.db, ircdb.LogMessageInput{
		BufferID:  bufID,
		MsgID:     msgID,
		Timestamp: ts,
		Sender:    sender,
		Userhost:  userhost,
		Account:   account,
		Kind:      kind,
		Target:    target,
		Content:   content,
		Raw:       raw,
	})
	if err != nil {
		slog.Error("insert message", "err", err, "network", h.networkName, "kind", kind)
		return
	}
	if !inserted || h.hub == nil {
		return
	}
	nick := ""
	if h.nickFn != nil {
		nick = h.nickFn()
	}
	nsMeta := h.trackNetsplit(bufID, id, bufKind, kind, sender, content, storedTS, ts)
	ev := (&MessageEvent{
		Type: "message",
		MessageCore: MessageCore{
			ID:        id,
			NetworkID: h.networkID,
			BufferID:  bufID,
			MsgID:     msgID,
			TS:        storedTS,
			Sender:    sender,
			Userhost:  userhost,
			Account:   account,
			Kind:      kind,
			Target:    target,
			Content:   content,
		},
	}).WithSemantics(nick)
	if muted {
		ev.CountsAsUnread = false
	}
	ev.Netsplit = nsMeta
	h.hub.Publish(ev)
	h.enqueuePreviews(id, bufID, bufKind, kind, content)
}

// trackNetsplit feeds stored channel quit/join messages through the live
// netsplit tracker. Returns the annotation for the outgoing message event
// (nil when it isn't part of a qualified netsplit) and publishes a
// retroactive NetsplitEvent for earlier quits when a cluster qualifies.
func (h *handler) trackNetsplit(bufID, msgID uuid.UUID, bufKind, kind, sender, content, storedTS string, fallbackTS time.Time) *NetsplitMeta {
	if bufKind != ircdb.BufferChannel || (kind != "quit" && kind != "join") {
		return nil
	}
	if h.netsplits == nil {
		h.netsplits = newNetsplitTracker()
	}
	eventTS := parseStoredTS(storedTS, fallbackTS)
	if kind == "join" {
		return h.netsplits.OnJoin(bufID, sender, eventTS)
	}
	meta, retro := h.netsplits.OnQuit(bufID, msgID, sender, content, eventTS)
	if meta != nil && len(retro) > 0 {
		h.hub.Publish(&NetsplitEvent{
			Type:       "netsplit",
			NetworkID:  h.networkID,
			BufferID:   bufID,
			Netsplit:   *meta,
			MessageIDs: retro,
		})
	}
	return meta
}

// enqueuePreviews schedules URL-preview fetches for user-authored content.
// We skip synthetic kinds (joins, modes, etc.) so the preview worker never
// wastes cycles on system noise. Status windows (server notices, MOTD) never
// get previews: link previews are off by default there.
func (h *handler) enqueuePreviews(messageID, bufferID uuid.UUID, bufKind, kind, content string) {
	if h.previews == nil || content == "" || bufKind == ircdb.BufferStatus {
		return
	}
	switch kind {
	case "privmsg", "notice", "action":
		h.previews.Enqueue(h.networkID, bufferID, messageID, content)
	}
}
