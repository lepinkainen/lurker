package irc

import (
	"fmt"

	"github.com/lrstanley/girc"
)

func (h *handler) onRPLList(_ *girc.Client, e girc.Event) {
	// Params: [me, #channel, usercount, :topic]
	if len(e.Params) < 3 {
		return
	}
	name := e.Params[1]
	count := 0
	_, _ = fmt.Sscanf(e.Params[2], "%d", &count)
	topic := ""
	if len(e.Params) >= 4 {
		topic = e.Last()
	}
	h.listEntries = append(h.listEntries, ChannelListEntry{Name: name, Count: count, Topic: topic})
}

func (h *handler) onRPLListEnd(_ *girc.Client, _ girc.Event) {
	if h.hub == nil {
		h.listEntries = nil
		return
	}
	entries := h.listEntries
	h.listEntries = nil
	h.hub.Publish(&ChannelListEvent{
		Type:      "channel_list",
		NetworkID: h.networkID,
		Entries:   entries,
		Done:      true,
	})
}
