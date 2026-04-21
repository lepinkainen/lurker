package irc

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/lrstanley/girc"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

// MessageEvent is published after a message row is successfully written.
// The JSON tags match the wire shape sent to WebSocket clients.
type MessageEvent struct {
	Type      string `json:"type"`
	ID        int64  `json:"id"`
	NetworkID int64  `json:"network_id"`
	BufferID  int64  `json:"buffer_id"`
	MsgID     string `json:"msgid,omitzero"`
	TS        string `json:"ts"`
	Sender    string `json:"sender"`
	Account   string `json:"account,omitzero"`
	Kind      string `json:"kind"`
	Target    string `json:"target,omitzero"`
	Content   string `json:"content"`
}

// BufferCreatedEvent is published the first time we see activity in a
// buffer that didn't exist yet (autojoin, inbound PM, network status).
type BufferCreatedEvent struct {
	Type      string `json:"type"`
	ID        int64  `json:"id"`
	NetworkID int64  `json:"network_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
}

// BufferUpdateEvent announces mutable buffer state changes.
type BufferUpdateEvent struct {
	Type       string `json:"type"`
	ID         int64  `json:"id"`
	NetworkID  int64  `json:"network_id"`
	Topic      string `json:"topic,omitzero"`
	Joined     bool   `json:"joined"`
	LastSeenID int64  `json:"last_seen_id,omitzero"`
}

// PresenceEvent is a lightweight stream event for user presence-ish changes.
type PresenceEvent struct {
	Type      string `json:"type"`
	NetworkID int64  `json:"network_id"`
	BufferID  int64  `json:"buffer_id,omitzero"`
	Nick      string `json:"nick,omitzero"`
	State     string `json:"state"`
	Target    string `json:"target,omitzero"`
}

// MemberListEvent publishes the full known member list for a channel.
type MemberListEvent struct {
	Type      string        `json:"type"`
	NetworkID int64         `json:"network_id"`
	BufferID  int64         `json:"buffer_id"`
	Channel   string        `json:"channel"`
	Members   []ChannelUser `json:"members"`
}

// ChannelUser is one user in a channel member list.
type ChannelUser struct {
	Nick   string `json:"nick"`
	Prefix string `json:"prefix,omitzero"`
	Away   bool   `json:"away"`
	Self   bool   `json:"self"`
}

// NetworkStateEvent announces connection state transitions.
type NetworkStateEvent struct {
	Type      string `json:"type"`
	NetworkID int64  `json:"network_id"`
	State     string `json:"state"` // "connecting" | "connected" | "disconnected"
}

// handler is the glue between a girc.Client and the SQLite store. One
// instance per network connection.
type handler struct {
	stores              *ircdb.MultiStore
	db                  *sql.DB
	hub                 *hub.Hub
	networkID           int64
	networkName         string
	autojoin            []string
	connectedHook       func()
	memberListHook      func(channel string)
	clearMemberListHook func(channel string)
}

func (h *handler) register(c *girc.Client) {
	c.Handlers.Add(girc.CONNECTED, h.onConnected)
	c.Handlers.Add(girc.DISCONNECTED, h.onDisconnected)
	c.Handlers.Add(girc.PRIVMSG, h.onPrivmsg)
	c.Handlers.Add(girc.NOTICE, h.onPrivmsg) // same handling, different kind
	c.Handlers.Add(girc.JOIN, h.onJoin)
	c.Handlers.Add(girc.PART, h.onPart)
	c.Handlers.Add(girc.KICK, h.onKick)
	c.Handlers.Add(girc.TOPIC, h.onTopic)
	c.Handlers.Add(girc.RPL_TOPIC, h.onTopicReply)
	c.Handlers.Add(girc.MODE, h.onMode)
	c.Handlers.Add(girc.RPL_CHANNELMODEIS, h.onChannelModeIs)
	c.Handlers.Add(girc.INVITE, h.onInvite)
	c.Handlers.Add(girc.QUIT, h.onQuit)
	c.Handlers.Add(girc.NICK, h.onNick)
	c.Handlers.Add(girc.CAP_AWAY, h.onAway)
	c.Handlers.Add(girc.CAP_ACCOUNT, h.onAccount)
	c.Handlers.Add(girc.CAP_CHGHOST, h.onChghost)
	c.Handlers.Add(girc.RPL_ENDOFNAMES, h.onEndOfNames)
	// echo-message: girc routes our own PRIVMSG/NOTICE echoes only through
	// ALL_EVENTS. Catch them here and feed the normal persistence path so
	// outbound messages land in history with the server-assigned msgid.
	c.Handlers.Add(girc.ALL_EVENTS, func(c *girc.Client, e girc.Event) {
		if !e.Echo {
			return
		}
		if e.Command == girc.PRIVMSG || e.Command == girc.NOTICE {
			h.onPrivmsg(c, e)
		}
	})
}

// --- event handlers ---

func (h *handler) onConnected(c *girc.Client, e girc.Event) {
	if h.connectedHook != nil {
		h.connectedHook()
	}
	h.publishNetworkState(StateConnected)
	h.logStatus(StateConnected.String(), "")
	for _, ch := range h.autojoin {
		c.Cmd.Join(ch)
	}
}

func (h *handler) onDisconnected(_ *girc.Client, e girc.Event) {
	h.logStatus(StateDisconnected.String(), e.Last())
}

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
	}
	h.storeEvent(e, bufName, bufferKind, kind, "", content)
}

func (h *handler) onJoin(_ *girc.Client, e girc.Event) {
	channel, ok := channelParam(e)
	if !ok {
		return
	}
	h.updateChannelJoined(channel, true, "join", e.Source)
	h.storeEvent(e, channel, ircdb.BufferChannel, "join", "", "")
}

func (h *handler) onPart(c *girc.Client, e girc.Event) {
	channel, ok := channelParam(e)
	if !ok {
		return
	}
	reason := ""
	if len(e.Params) >= 2 {
		reason = e.Last()
	}
	if c != nil && e.Source != nil && strings.EqualFold(e.Source.Name, c.GetNick()) {
		h.updateChannelJoined(channel, false, "part", e.Source)
	} else {
		h.publishRemotePresence(channel, "part", e.Source)
	}
	h.storeEvent(e, channel, ircdb.BufferChannel, "part", "", reason)
}

func (h *handler) onKick(c *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	channel := e.Params[0]
	kickedNick := e.Params[1]
	reason := ""
	if len(e.Params) >= 3 {
		reason = e.Last()
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure kick buffer", "err", err, "network", h.networkName, "buffer", channel)
	} else {
		if h.hub != nil {
			h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, BufferID: globalBufID, Nick: kickedNick, State: "kick"})
		}
		if c != nil && strings.EqualFold(kickedNick, c.GetNick()) {
			if h.clearMemberListHook != nil {
				h.clearMemberListHook(channel)
			}
			if err := ircdb.UpdateLogBufferJoined(ctx, h.db, channel, false); err != nil {
				slog.Error("update channel joined", "err", err, "network", h.networkName, "buffer", channel, "joined", false)
			}
			h.publishBufferUpdate(BufferUpdateEvent{Type: "buffer_update", ID: globalBufID, NetworkID: h.networkID, Joined: false})
			h.publishMemberList(c, channel)
		}
	}
	// target = the kicked nick
	h.storeEvent(e, channel, ircdb.BufferChannel, "kick", kickedNick, reason)
}

func (h *handler) onTopic(_ *girc.Client, e girc.Event) {
	channel, topic, ok := channelTopic(e)
	if !ok {
		return
	}
	h.updateChannelTopic(channel, topic)
	h.storeEvent(e, channel, ircdb.BufferChannel, "topic", "", topic)
}

func (h *handler) onTopicReply(_ *girc.Client, e girc.Event) {
	channel, topic, ok := channelTopic(e)
	if !ok {
		return
	}
	h.updateChannelTopic(channel, topic)
}

func (h *handler) onMode(_ *girc.Client, e girc.Event) {
	target, modeArgs, ok := modeTargetAndArgs(e)
	if !ok {
		return
	}
	kind := "mode"
	if !girc.IsValidChannel(target) {
		// User mode changes go to the network status buffer.
		h.storeEvent(e, "", ircdb.BufferStatus, kind, target, modeArgs)
		return
	}
	h.touchChannelBuffer(target, "ensure mode buffer")
	h.storeEvent(e, target, ircdb.BufferChannel, kind, "", modeArgs)
}

func (h *handler) onChannelModeIs(_ *girc.Client, e girc.Event) {
	target, _, ok := modeTargetAndArgs(e)
	if !ok || !girc.IsValidChannel(target) {
		return
	}
	h.touchChannelBuffer(target, "ensure channel mode buffer")
}

func (h *handler) onQuit(_ *girc.Client, e girc.Event) {
	// QUIT doesn't name the channels the user was in; we log it to the
	// status buffer for now. Per-channel fan-out is a later enhancement.
	h.storeEvent(e, "", ircdb.BufferStatus, "quit", "", e.Last())
}

func (h *handler) onNick(_ *girc.Client, e girc.Event) {
	newNick := e.Last()
	if h.hub != nil && e.Source != nil {
		h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, Nick: e.Source.Name, State: "nick", Target: newNick})
	}
	h.storeEvent(e, "", ircdb.BufferStatus, "nick", newNick, "")
}

func (h *handler) onInvite(_ *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	h.touchChannelBuffer(e.Params[1], "ensure invite buffer")
	h.storeEvent(e, e.Params[1], ircdb.BufferChannel, "invite", e.Params[0], "")
}

func (h *handler) onAway(_ *girc.Client, e girc.Event) {
	if e.Source == nil {
		return
	}
	state := "back"
	message := ""
	if e.Last() != "" {
		state = "away"
		message = e.Last()
	}
	if h.hub != nil {
		h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, Nick: e.Source.Name, State: state})
	}
	h.storeEvent(e, "", ircdb.BufferStatus, state, e.Source.Name, message)
}

func (h *handler) onAccount(_ *girc.Client, e girc.Event) {
	if e.Source == nil {
		return
	}
	account := e.Last()
	if account == "*" {
		account = ""
	}
	h.storeEvent(e, "", ircdb.BufferStatus, "account", e.Source.Name, account)
}

func (h *handler) onChghost(_ *girc.Client, e girc.Event) {
	if e.Source == nil || len(e.Params) < 2 {
		return
	}
	h.storeEvent(e, "", ircdb.BufferStatus, "chghost", e.Source.Name, strings.Join(e.Params[:2], " "))
}

func (h *handler) onEndOfNames(c *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	channel := e.Params[1]
	h.publishMemberList(c, channel)
}

// --- helpers ---

// storeEvent is the single funnel for inbound IRC events. It upserts the
// target buffer, writes a messages row, and publishes hub events so
// WebSocket clients can render in real time.
func (h *handler) storeEvent(e girc.Event, bufName, bufKind, kind, target, content string) {
	sender := ""
	if e.Source != nil {
		sender = e.Source.Name
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

	globalBufID, localBufID, err := h.ensureBuffer(ctx, bufName, bufKind)
	if err != nil {
		slog.Error("ensure buffer", "err", err, "network", h.networkName, "buffer", bufName)
		return
	}
	id, storedTS, inserted, err := ircdb.InsertLogMessage(ctx, h.db, ircdb.LogMessageInput{
		BufferID:  localBufID,
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
		BufferID:  globalBufID,
		MsgID:     msgID,
		TS:        storedTS,
		Sender:    sender,
		Account:   account,
		Kind:      kind,
		Target:    target,
		Content:   content,
	})
}

// logStatus writes a synthetic message (connect/disconnect) to the
// per-network status buffer and publishes state/message events.
func (h *handler) logStatus(kind, content string) {
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, localBufID, err := h.ensureBuffer(ctx, "", ircdb.BufferStatus)
	if err != nil {
		slog.Error("ensure status buffer", "err", err, "network", h.networkName)
		return
	}
	id, ts, inserted, err := ircdb.InsertLogMessage(ctx, h.db, ircdb.LogMessageInput{
		BufferID:  localBufID,
		Timestamp: time.Now(),
		Sender:    "*",
		Kind:      kind,
		Content:   content,
		Raw:       "",
	})
	if err == nil && inserted && h.hub != nil {
		h.hub.Publish(&MessageEvent{
			Type: "message", ID: id, NetworkID: h.networkID, BufferID: globalBufID,
			TS: ts, Sender: "*", Kind: kind, Content: content,
		})
	}
	if kind == StateDisconnected.String() {
		h.publishNetworkState(StateDisconnected)
	}
}

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

func (h *handler) touchChannelBuffer(channel, action string) {
	ctx, cancel := h.eventContext()
	defer cancel()
	if _, _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel); err != nil {
		slog.Error(action, "err", err, "network", h.networkName, "buffer", channel)
	}
}

func (h *handler) updateChannelJoined(channel string, joined bool, presenceState string, source *girc.Source) {
	if !joined && h.clearMemberListHook != nil {
		h.clearMemberListHook(channel)
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure channel buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	if err := ircdb.UpdateLogBufferJoined(ctx, h.db, channel, joined); err != nil {
		slog.Error("update channel joined", "err", err, "network", h.networkName, "buffer", channel, "joined", joined)
	}
	h.publishBufferUpdate(BufferUpdateEvent{Type: "buffer_update", ID: globalBufID, NetworkID: h.networkID, Joined: joined})
	if h.hub != nil && source != nil {
		h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, BufferID: globalBufID, Nick: source.Name, State: presenceState})
	}
}

func (h *handler) publishRemotePresence(channel, state string, source *girc.Source) {
	if h.hub == nil || source == nil {
		return
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure presence buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	h.hub.Publish(&PresenceEvent{Type: "presence", NetworkID: h.networkID, BufferID: globalBufID, Nick: source.Name, State: state})
}

func (h *handler) updateChannelTopic(channel, topic string) {
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure topic buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	if err := ircdb.UpdateLogBufferTopic(ctx, h.db, channel, topic); err != nil {
		slog.Error("update channel topic", "err", err, "network", h.networkName, "buffer", channel)
	}
	h.publishBufferUpdate(BufferUpdateEvent{Type: "buffer_update", ID: globalBufID, NetworkID: h.networkID, Topic: topic, Joined: true})
}

func (h *handler) ensureBuffer(ctx context.Context, name, kind string) (globalBufID, localBufID int64, err error) {
	globalBufID, created, buf, err := h.stores.UpsertBufferRegistry(ctx, h.networkID, name, kind)
	if err != nil {
		return 0, 0, err
	}
	localBufID, _, _, err = ircdb.UpsertLogBuffer(ctx, h.db, h.networkID, name, kind)
	if err != nil {
		return 0, 0, err
	}
	h.publishBufferCreated(created, globalBufID, buf)
	return globalBufID, localBufID, nil
}

func (h *handler) publishBufferCreated(created bool, globalBufID int64, buf ircdb.Buffer) {
	if !created || h.hub == nil {
		return
	}
	h.hub.Publish(&BufferCreatedEvent{Type: "buffer_created", ID: globalBufID, NetworkID: buf.NetworkID, Name: buf.Name, Kind: buf.Kind, CreatedAt: buf.CreatedAt})
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

func (h *handler) publishMemberList(c *girc.Client, channel string) {
	if c == nil || channel == "" {
		return
	}
	members := buildChannelMembers(c, channel)
	if members == nil {
		return
	}
	if h.memberListHook != nil {
		h.memberListHook(channel)
	}
	if h.hub == nil {
		return
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	globalBufID, _, err := h.ensureBuffer(ctx, channel, ircdb.BufferChannel)
	if err != nil {
		slog.Error("ensure names buffer", "err", err, "network", h.networkName, "buffer", channel)
		return
	}
	out := make([]ChannelUser, 0, len(members))
	for _, member := range members {
		out = append(out, ChannelUser(member))
	}
	h.hub.Publish(&MemberListEvent{Type: "member_list", NetworkID: h.networkID, BufferID: globalBufID, Channel: channel, Members: out})
}
