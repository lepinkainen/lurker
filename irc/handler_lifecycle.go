package irc

import (
	"log/slog"
	"time"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lrstanley/girc"
)

func (h *handler) onConnected(c *girc.Client, e girc.Event) {
	if h.connectedHook != nil {
		h.connectedHook(c.GetNick())
	}
	h.publishNetworkState(StateConnected)
	h.logStatus(StateConnected.String(), "")
	for _, ch := range h.autojoin {
		c.Cmd.Join(ch)
	}
}

func (h *handler) onDisconnected(_ *girc.Client, e girc.Event) {
	h.logStatus(StateDisconnected.String(), e.Last())
	h.markAllChannelsParted()
}

// logStatus writes a synthetic message (connect/disconnect) to the
// per-network status buffer and publishes state/message events.
func (h *handler) logStatus(kind, content string) {
	ctx, cancel := h.eventContext()
	defer cancel()
	bufID, err := h.ensureBuffer(ctx, "", ircdb.BufferStatus)
	if err != nil {
		slog.Error("ensure status buffer", "err", err, "network", h.networkName)
		return
	}
	id, ts, inserted, err := ircdb.InsertLogMessage(ctx, h.db, ircdb.LogMessageInput{
		BufferID:  bufID,
		Timestamp: time.Now(),
		Sender:    "*",
		Kind:      kind,
		Content:   content,
		Raw:       "",
	})
	if err == nil && inserted && h.hub != nil {
		h.hub.Publish(&MessageEvent{
			Type: "message", ID: id, NetworkID: h.networkID, BufferID: bufID,
			TS: ts, Sender: "*", Kind: kind, Content: content,
		})
	}
	if kind == StateDisconnected.String() {
		h.publishNetworkState(StateDisconnected)
	}
}
