package irc

import (
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lrstanley/girc"
)

func (h *handler) onPrivmsg(_ *girc.Client, e girc.Event) {
	var bufName, bufferKind, kind string
	switch {
	case e.IsFromChannel():
		bufName = e.Params[0]
		bufferKind = ircdb.BufferChannel
		kind = "privmsg"
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
		kind = "privmsg"
	}
	if e.Command == girc.NOTICE {
		kind = "notice"
	}
	content := e.Last()
	if e.IsAction() {
		kind = "action"
		content = e.StripAction()
	} else if ok, ctcp := e.IsCTCP(); ok && ctcp.Command != girc.CTCP_ACTION {
		kind = "ctcp"
		if ctcp.Text != "" {
			content = ctcp.Command + " " + ctcp.Text
		} else {
			content = ctcp.Command
		}
	}
	h.storeEvent(e, bufName, bufferKind, kind, "", content)
}

// isIgnored checks whether the given nick matches any configured ignore mask.
func (h *handler) isIgnored(nick string) bool {
	if h.stores == nil {
		return false
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	masks, err := ircdb.ListIgnores(ctx, h.stores.Control, h.networkID)
	if err != nil || len(masks) == 0 {
		return false
	}
	nickLower := strings.ToLower(nick)
	for _, mask := range masks {
		if matched, _ := path.Match(strings.ToLower(mask), nickLower); matched {
			return true
		}
	}
	return false
}

// storeEvent is the single funnel for inbound IRC events. It upserts the
// target buffer, writes a messages row, and publishes hub events so
// WebSocket clients can render in real time.
func (h *handler) storeEvent(e girc.Event, bufName, bufKind, kind, target, content string) {
	content = ensureUTF8(content)
	sender := ""
	if e.Source != nil {
		sender = e.Source.Name
	}
	if sender != "" && sender != "*" && h.isIgnored(sender) {
		return
	}
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
	id, storedTS, inserted, err := ircdb.InsertLogMessage(ctx, h.db, ircdb.LogMessageInput{
		BufferID:  bufID,
		MsgID:     msgID,
		Timestamp: ts,
		Sender:    sender,
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
	h.hub.Publish(&MessageEvent{
		Type:      "message",
		ID:        id,
		NetworkID: h.networkID,
		BufferID:  bufID,
		MsgID:     msgID,
		TS:        storedTS,
		Sender:    sender,
		Account:   account,
		Kind:      kind,
		Target:    target,
		Content:   content,
	})
	h.enqueuePreviews(id, bufID, kind, content)
}

// enqueuePreviews schedules URL-preview fetches for user-authored content.
// We skip synthetic kinds (joins, modes, etc.) so the preview worker never
// wastes cycles on system noise.
func (h *handler) enqueuePreviews(messageID, bufferID uuid.UUID, kind, content string) {
	if h.previews == nil || content == "" {
		return
	}
	switch kind {
	case "privmsg", "notice", "action":
		h.previews.Enqueue(h.networkID, bufferID, messageID, content)
	}
}
