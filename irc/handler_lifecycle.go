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
	joinBatched(c, h.autojoin)
	// Channel gaps are requested per self-JOIN; query gaps have no join
	// event, so request them as soon as the connection registers.
	h.requestQueryBackfills()
}

// joinBatchLen caps the channel-list length of one JOIN command, well under
// the 512-byte IRC line limit.
const joinBatchLen = 400

// joinBatched joins channels with comma-separated JOIN commands. One command
// instead of one per channel matters on IRCnet-style servers (ISUPPORT
// PENALTY), where every command charges a read-delay penalty and a connect
// burst can stall the connection for minutes. Batches are built here rather
// than passing everything to girc's Join, whose overflow split silently drops
// a channel at each line boundary.
func joinBatched(c *girc.Client, channels []string) {
	var batch []string
	length := 0
	for _, ch := range channels {
		if length > 0 && length+1+len(ch) > joinBatchLen {
			c.Cmd.Join(batch...)
			batch, length = nil, 0
		}
		batch = append(batch, ch)
		length += len(ch) + 1
	}
	if len(batch) > 0 {
		c.Cmd.Join(batch...)
	}
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
