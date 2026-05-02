package irc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lrstanley/girc"
)

func (h *handler) eventContext() (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(context.Background(), 5*time.Second, errors.New("irc event timeout"))
}

func channelParam(e girc.Event) (string, bool) {
	if len(e.Params) < 1 || e.Params[0] == "" {
		return "", false
	}
	return e.Params[0], true
}

func channelTopic(e girc.Event) (channel, topic string, ok bool) {
	switch len(e.Params) {
	case 0:
		return "", "", false
	case 1:
		return e.Params[0], "", e.Params[0] != ""
	case 2:
		return e.Params[0], e.Last(), e.Params[0] != ""
	default:
		return e.Params[1], e.Last(), e.Params[1] != ""
	}
}

func modeTargetAndArgs(e girc.Event) (target, args string, ok bool) {
	if len(e.Params) < 1 || e.Params[0] == "" {
		return "", "", false
	}
	return e.Params[0], strings.Join(e.Params[1:], " "), true
}

// ensureBuffer ensures a buffer exists in both the global registry and the
// per-network log DB with the SAME UUID. Returns that single ID.
func (h *handler) ensureBuffer(ctx context.Context, name, kind string) (bufID uuid.UUID, err error) {
	bufID, created, buf, err := h.stores.EnsureBuffer(ctx, h.networkID, name, kind)
	if err != nil {
		return uuid.Nil, err
	}
	h.publishBufferCreated(created, bufID, buf)
	return bufID, nil
}

func (h *handler) publishBufferCreated(created bool, bufID uuid.UUID, buf ircdb.Buffer) {
	if !created || h.hub == nil {
		return
	}
	h.hub.Publish(&BufferCreatedEvent{Type: "buffer_created", ID: bufID, NetworkID: buf.NetworkID, Name: buf.Name, Kind: buf.Kind, CreatedAt: buf.CreatedAt})
}

func (h *handler) publishBufferUpdate(ev BufferUpdateEvent) {
	if h.hub == nil {
		return
	}
	h.hub.Publish(&ev)
}

func (h *handler) publishNetworkState(state NetworkState) {
	if h.hub == nil {
		return
	}
	h.hub.Publish(&NetworkStateEvent{Type: "network_state", NetworkID: h.networkID, State: state.String()})
}
