package irc

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
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
	h.publishBufferCreated(created, buf)
	return bufID, nil
}

// PublishBufferCreated publishes a buffer_created event to h. No-op if h is nil.
func PublishBufferCreated(h *hub.Hub, buf ircdb.Buffer) {
	if h == nil {
		return
	}
	h.Publish(&BufferCreatedEvent{Type: "buffer_created", ID: buf.ID, NetworkID: buf.NetworkID, Name: buf.Name, Kind: buf.Kind, CreatedAt: buf.CreatedAt})
}

// PublishNetworkState publishes a network_state event to h. No-op if h is nil.
func PublishNetworkState(h *hub.Hub, networkID uuid.UUID, state NetworkState) {
	if h == nil {
		return
	}
	h.Publish(&NetworkStateEvent{Type: "network_state", NetworkID: networkID, State: state.String()})
}

func (h *handler) publishBufferCreated(created bool, buf ircdb.Buffer) {
	if !created {
		return
	}
	PublishBufferCreated(h.hub, buf)
}

func (h *handler) publishBufferUpdate(ev BufferUpdateEvent) {
	if h.hub == nil {
		return
	}
	h.hub.Publish(&ev)
}

// syncBufferArchived persists the archived flag for a buffer and reports
// whether it actually changed (no write, no event churn when already in the
// desired state — remote joins hit this path on every JOIN event).
func (h *handler) syncBufferArchived(ctx context.Context, bufferID uuid.UUID, archived bool) bool {
	current, err := ircdb.GetBufferSettings(ctx, h.stores.Control, bufferID)
	if err == nil && current.Archived == archived {
		return false
	}
	if _, err := ircdb.UpdateBufferSettings(ctx, h.stores.Control, bufferID, ircdb.BufferSettingsPatch{Archived: &archived}); err != nil {
		slog.Error("sync buffer archived", "err", err, "network", h.networkName, "buffer", bufferID)
		return false
	}
	return true
}

func (h *handler) publishNetworkState(state NetworkState) {
	PublishNetworkState(h.hub, h.networkID, state)
}
