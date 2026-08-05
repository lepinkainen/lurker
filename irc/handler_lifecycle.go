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
	for _, line := range h.connectCommands {
		if err := c.Cmd.SendRaw(line); err != nil {
			slog.Warn("send connect command", "err", err, "network", h.networkName)
		}
	}
	for _, ch := range h.autojoin {
		c.Cmd.Join(ch)
	}
	// Channel gaps are requested per self-JOIN; query gaps have no join
	// event, so request them as soon as the connection registers.
	h.requestQueryBackfills()
}

func (h *handler) onDisconnected(_ *girc.Client, e girc.Event) {
	h.logStatus(StateDisconnected.String(), e.Last())
	h.markAllChannelsParted()
	h.lastMemberListHash = nil
	h.userChannels = newUserChannels()
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
		h.hub.Publish((&MessageEvent{
			Type: "message",
			MessageCore: MessageCore{
				ID: id, NetworkID: h.networkID, BufferID: bufID,
				TS: ts, Sender: "*", Kind: kind, Content: content,
			},
		}).WithSemantics(""))
	}
	if kind == StateDisconnected.String() {
		h.publishNetworkState(StateDisconnected)
	}
}
