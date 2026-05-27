package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/internal/closeutil"
)

// clientCmd is the set of verbs a client can send. We union-decode from
// JSON into this struct and dispatch on Type; fields irrelevant to the
// chosen verb are simply ignored.
type clientCmd struct {
	Type      string    `json:"type"`
	ReqID     string    `json:"req_id,omitzero"`
	BufferID  uuid.UUID `json:"buffer_id,omitzero"`
	NetworkID uuid.UUID `json:"network_id,omitzero"`
	Channel   string    `json:"channel,omitzero"`
	Content   string    `json:"content,omitzero"`
	Target    string    `json:"target,omitzero"`
	Before    uuid.UUID `json:"before,omitzero"`
	Limit     int       `json:"limit,omitzero"`
	MessageID uuid.UUID `json:"message_id,omitzero"`
	// internal is true when handleCmd was re-entered from cmdInput after
	// parseInput already resolved the buffer/network. Skips the kind gate
	// so the input → send/join/… hot path pays one buffer lookup, not two.
	// Unexported so encoding/json never touches it.
	internal bool `json:"-"`
}

// ack / error envelopes. We keep these separate from IRC event types so
// the client can tell them apart by the top-level "type".
type ackEnvelope struct {
	Type  string `json:"type"`
	ReqID string `json:"req_id"`
}

type errorEnvelope struct {
	Type    string `json:"type"`
	ReqID   string `json:"req_id,omitzero"`
	Message string `json:"message"`
}

type historyResult struct {
	Type     string       `json:"type"`
	ReqID    string       `json:"req_id"`
	BufferID uuid.UUID    `json:"buffer_id"`
	Messages []messageDTO `json:"messages"`
}

type bufferLastSeenEvent struct {
	Type       string    `json:"type"`
	ID         uuid.UUID `json:"id"`
	NetworkID  uuid.UUID `json:"network_id"`
	LastSeenID uuid.UUID `json:"last_seen_id"`
	Unread     int       `json:"unread"`
	Mentions   int       `json:"mentions"`
}

type ignoreListResult struct {
	Type      string    `json:"type"`
	ReqID     string    `json:"req_id"`
	NetworkID uuid.UUID `json:"network_id"`
	Masks     []string  `json:"masks"`
}

// messageSender covers outbound IRC messages to channels and users plus
// the local-echo log write that pairs with every send.
type messageSender interface {
	Send(networkID uuid.UUID, target, content string) error
	Me(networkID uuid.UUID, target, message string) error
	Notice(networkID uuid.UUID, target, content string) error
	CTCP(networkID uuid.UUID, nick, command, args string) error
	LogOutbound(ctx context.Context, networkID uuid.UUID, target, kind, content string) error
}

// channelOps covers channel-membership and channel-state mutations.
type channelOps interface {
	Join(networkID uuid.UUID, channel string) error
	Part(networkID uuid.UUID, channel, reason string) error
	Rejoin(networkID uuid.UUID, channel string) error
	Topic(networkID uuid.UUID, channel, topic string) error
	Invite(networkID uuid.UUID, nick, channel string) error
	Kick(networkID uuid.UUID, channel, nick, reason string) error
	ListChannels(networkID uuid.UUID, filter string) error
}

// presenceOps covers nickname/identity and away-status mutations.
type presenceOps interface {
	ChangeNick(networkID uuid.UUID, nick string) error
	Whois(networkID uuid.UUID, nick string) error
	Away(networkID uuid.UUID, message string) error
	Back(networkID uuid.UUID) error
	Quit(networkID uuid.UUID, message string) error
}

// modeOps covers privileged IRC commands: mode changes and raw escapes.
type modeOps interface {
	Mode(networkID uuid.UUID, target, modes string, params ...string) error
	Raw(networkID uuid.UUID, line string) error
}

// wsManager is the IRC manager surface used by WebSocket command handlers.
// It is the composition of four cohesive sub-interfaces; handlers depend on
// this composed view, but tests can mock a single sub-interface in isolation.
type wsManager interface {
	messageSender
	channelOps
	presenceOps
	modeOps
}

// stream is the WebSocket endpoint. It subscribes to the event hub and
// forwards every published event as JSON; it also reads client commands
// and dispatches them to the IRC manager or the SQLite store.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("ws accept", "err", err)
		return
	}
	defer closeutil.Ignore(closeFunc(func() error {
		return c.Close(websocket.StatusNormalClosure, "")
	}), "component", "websocket")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	events, unsub := s.Hub.Subscribe(256)
	defer unsub()

	done := make(chan struct{})
	go runStreamWriter(ctx, c, events, done)
	s.runStreamReader(ctx, c, cancel, done)
}

// runStreamWriter forwards every event published on the hub channel to the
// WebSocket. The 10s write timeout is per-message; a stalled client tears
// down the connection rather than backing up the hub.
func runStreamWriter(ctx context.Context, c *websocket.Conn, events <-chan any, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			err := wsjson.Write(wctx, c, ev)
			wcancel()
			if err != nil {
				return
			}
		}
	}
}

func (s *Server) runStreamReader(ctx context.Context, c *websocket.Conn, cancel context.CancelFunc, done <-chan struct{}) {
	for {
		var cmd clientCmd
		err := wsjson.Read(ctx, c, &cmd)
		if err != nil {
			if _, ok := errors.AsType[websocket.CloseError](err); !ok && ctx.Err() == nil {
				slog.Debug("ws read", "err", err)
			}
			cancel()
			<-done
			return
		}
		s.handleCmd(ctx, c, cmd)
	}
}

// commandsAllowedForNonIRC enumerates the WS command types permitted when
// the target network is not an IRC network. Everything else is an IRC-only
// mutation (PRIVMSG, JOIN, MODE, …) and is rejected for datasource networks
// such as Bluesky.
var commandsAllowedForNonIRC = map[string]struct{}{
	"input":     {},
	"history":   {},
	"mark_read": {},
}

func (s *Server) handleCmd(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if !cmd.internal {
		if _, ok := commandsAllowedForNonIRC[cmd.Type]; !ok {
			if kind, resolved := s.resolveTargetNetworkKind(ctx, cmd); resolved && isNonIRCNetwork(kind) {
				writeWSErr(ctx, c, cmd.ReqID, "command not supported on "+kind+" networks")
				return
			}
		}
	}
	switch cmd.Type {
	case "input":
		s.cmdInput(ctx, c, cmd)
	case "send":
		s.cmdSend(ctx, c, cmd)
	case "history":
		s.cmdHistory(ctx, c, cmd)
	case "join":
		s.cmdJoin(ctx, c, cmd)
	case "part":
		s.cmdPart(ctx, c, cmd)
	case "mark_read":
		s.cmdMarkRead(ctx, c, cmd)
	case "nick":
		s.cmdNick(ctx, c, cmd)
	case "me":
		s.cmdMe(ctx, c, cmd)
	case "msg":
		s.cmdMsg(ctx, c, cmd)
	case "topic":
		s.cmdTopic(ctx, c, cmd)
	case "whois":
		s.cmdWhois(ctx, c, cmd)
	case "invite":
		s.cmdInvite(ctx, c, cmd)
	case "kick":
		s.cmdKick(ctx, c, cmd)
	case "mode":
		s.cmdMode(ctx, c, cmd)
	case "raw":
		s.cmdRaw(ctx, c, cmd)
	case "away":
		s.cmdAway(ctx, c, cmd)
	case "back":
		s.cmdBack(ctx, c, cmd)
	case "quit":
		s.cmdQuit(ctx, c, cmd)
	case "rejoin":
		s.cmdRejoin(ctx, c, cmd)
	case "notice":
		s.cmdNotice(ctx, c, cmd)
	case "ctcp":
		s.cmdCTCP(ctx, c, cmd)
	case "query":
		s.cmdQuery(ctx, c, cmd)
	case "list":
		s.cmdList(ctx, c, cmd)
	case "op":
		s.cmdChannelMode(ctx, c, cmd, "+o")
	case "deop":
		s.cmdChannelMode(ctx, c, cmd, "-o")
	case "voice":
		s.cmdChannelMode(ctx, c, cmd, "+v")
	case "devoice":
		s.cmdChannelMode(ctx, c, cmd, "-v")
	case "ban":
		s.cmdChannelMode(ctx, c, cmd, "+b")
	case "unban":
		s.cmdChannelMode(ctx, c, cmd, "-b")
	case "banlist":
		s.cmdBanlist(ctx, c, cmd)
	case "kickban":
		s.cmdKickban(ctx, c, cmd)
	case "ignore":
		s.cmdIgnore(ctx, c, cmd)
	case "unignore":
		s.cmdUnignore(ctx, c, cmd)
	case "ignorelist":
		s.cmdIgnorelist(ctx, c, cmd)
	default:
		writeWSErr(ctx, c, cmd.ReqID, "unknown command: "+cmd.Type)
	}
}

// cmdInput is the unified input verb: a free-form line ("/join #foo" or
// plain text) is parsed server-side and dispatched to the existing
// command handlers. Buffer context is resolved from cmd.BufferID.
func (s *Server) cmdInput(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.BufferID == uuid.Nil || strings.TrimSpace(cmd.Content) == "" {
		writeWSErr(ctx, c, cmd.ReqID, "input requires buffer_id and content")
		return
	}
	networkID, name, _, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil {
		writeWSErr(ctx, c, cmd.ReqID, "unknown buffer")
		return
	}
	parsed, err := parseInput(cmd.Content, inputBuffer{ID: cmd.BufferID, NetworkID: networkID, Name: name})
	if err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	parsed.ReqID = cmd.ReqID
	// Re-dispatch bypasses handleCmd's gate (internal=true), so apply it
	// here once using the network row we resolve directly. Saves one buffer
	// lookup compared to letting handleCmd re-resolve from scratch.
	if _, allowed := commandsAllowedForNonIRC[parsed.Type]; !allowed {
		if n, gerr := ircdb.GetNetwork(ctx, s.Stores.Control, networkID); gerr == nil && isNonIRCNetwork(n.Kind) {
			writeWSErr(ctx, c, cmd.ReqID, "command not supported on "+n.Kind+" networks")
			return
		}
	}
	parsed.internal = true
	s.handleCmd(ctx, c, parsed)
}

func (s *Server) cmdSend(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	content := strings.TrimSpace(cmd.Content)
	if cmd.BufferID == uuid.Nil || content == "" {
		writeWSErr(ctx, c, cmd.ReqID, "send requires buffer_id and content")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil {
		writeWSErr(ctx, c, cmd.ReqID, "unknown buffer")
		return
	}
	if kind == ircdb.BufferStatus {
		writeWSErr(ctx, c, cmd.ReqID, "cannot send to status buffer")
		return
	}
	if err := s.Manager.Send(networkID, name, content); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	if err := s.Manager.LogOutbound(ctx, networkID, name, "privmsg", content); err != nil {
		slog.Error("log outbound", "err", err, "network_id", networkID, "buffer_id", cmd.BufferID)
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdHistory(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.BufferID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "history requires buffer_id")
		return
	}
	msgs, err := s.loadHistory(ctx, cmd.BufferID, cmd.Before, clampLimitInt(cmd.Limit, 200, 500))
	if err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	_ = wsjson.Write(ctx, c, historyResult{
		Type: "history_result", ReqID: cmd.ReqID,
		BufferID: cmd.BufferID, Messages: msgs,
	})
}

func (s *Server) cmdJoin(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.NetworkID == uuid.Nil || cmd.Channel == "" {
		writeWSErr(ctx, c, cmd.ReqID, "join requires network_id and channel")
		return
	}
	if err := s.Manager.Join(cmd.NetworkID, cmd.Channel); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdPart(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.BufferID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "part requires buffer_id")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind != ircdb.BufferChannel {
		writeWSErr(ctx, c, cmd.ReqID, "part only works on channel buffers")
		return
	}
	if err := s.Manager.Part(networkID, name, cmd.Content); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdMarkRead(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.BufferID == uuid.Nil || cmd.MessageID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "mark_read requires buffer_id and message_id")
		return
	}
	networkID, _, _, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	if err := s.Stores.MarkBufferLastSeen(ctx, cmd.BufferID, cmd.MessageID); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	if s.Hub != nil {
		nick := ""
		if s.Manager != nil {
			nick = s.Manager.Nick(networkID)
		}
		unread, mentions := s.computeUnreadCounts(ctx, networkID, cmd.BufferID, cmd.MessageID, nick)
		s.Hub.Publish(bufferLastSeenEvent{
			Type: "buffer_update", ID: cmd.BufferID, NetworkID: networkID,
			LastSeenID: cmd.MessageID, Unread: unread, Mentions: mentions,
		})
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdNick(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	nick := strings.TrimSpace(cmd.Content)
	if cmd.NetworkID == uuid.Nil || nick == "" {
		writeWSErr(ctx, c, cmd.ReqID, "nick requires network_id and content")
		return
	}
	if err := s.Manager.ChangeNick(cmd.NetworkID, nick); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdMe(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	content := strings.TrimSpace(cmd.Content)
	if cmd.BufferID == uuid.Nil || content == "" {
		writeWSErr(ctx, c, cmd.ReqID, "me requires buffer_id and content")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind == ircdb.BufferStatus {
		writeWSErr(ctx, c, cmd.ReqID, "invalid buffer for /me")
		return
	}
	if err := s.Manager.Me(networkID, name, content); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	if err := s.Manager.LogOutbound(ctx, networkID, name, "action", content); err != nil {
		slog.Error("log outbound me", "err", err)
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdMsg(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	target := strings.TrimSpace(cmd.Target)
	content := strings.TrimSpace(cmd.Content)
	if cmd.NetworkID == uuid.Nil || target == "" || content == "" {
		writeWSErr(ctx, c, cmd.ReqID, "msg requires network_id, target, and content")
		return
	}
	if err := s.Manager.Send(cmd.NetworkID, target, content); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	if err := s.Manager.LogOutbound(ctx, cmd.NetworkID, target, "privmsg", content); err != nil {
		slog.Error("log outbound msg", "err", err)
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdTopic(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.BufferID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "topic requires buffer_id")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind != ircdb.BufferChannel {
		writeWSErr(ctx, c, cmd.ReqID, "topic only works on channel buffers")
		return
	}
	if err := s.Manager.Topic(networkID, name, strings.TrimSpace(cmd.Content)); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdWhois(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	target := strings.TrimSpace(cmd.Target)
	if cmd.NetworkID == uuid.Nil || target == "" {
		writeWSErr(ctx, c, cmd.ReqID, "whois requires network_id and target")
		return
	}
	if err := s.Manager.Whois(cmd.NetworkID, target); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdInvite(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	target := strings.TrimSpace(cmd.Target)
	channel := strings.TrimSpace(cmd.Channel)
	if cmd.NetworkID == uuid.Nil || target == "" || channel == "" {
		writeWSErr(ctx, c, cmd.ReqID, "invite requires network_id, target, and channel")
		return
	}
	if err := s.Manager.Invite(cmd.NetworkID, target, channel); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdKick(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	target := strings.TrimSpace(cmd.Target)
	if cmd.BufferID == uuid.Nil || target == "" {
		writeWSErr(ctx, c, cmd.ReqID, "kick requires buffer_id and target")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind != ircdb.BufferChannel {
		writeWSErr(ctx, c, cmd.ReqID, "kick only works on channel buffers")
		return
	}
	if err := s.Manager.Kick(networkID, name, target, strings.TrimSpace(cmd.Content)); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdMode(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	content := strings.TrimSpace(cmd.Content)
	if cmd.BufferID == uuid.Nil || content == "" {
		writeWSErr(ctx, c, cmd.ReqID, "mode requires buffer_id and content")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind != ircdb.BufferChannel {
		writeWSErr(ctx, c, cmd.ReqID, "mode only works on channel buffers")
		return
	}
	parts := strings.Fields(content)
	modes := parts[0]
	params := parts[1:]
	if err := s.Manager.Mode(networkID, name, modes, params...); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdRaw(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	content := strings.TrimSpace(cmd.Content)
	if cmd.NetworkID == uuid.Nil || content == "" {
		writeWSErr(ctx, c, cmd.ReqID, "raw requires network_id and content")
		return
	}
	if err := s.Manager.Raw(cmd.NetworkID, content); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdAway(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.NetworkID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "away requires network_id")
		return
	}
	if err := s.Manager.Away(cmd.NetworkID, strings.TrimSpace(cmd.Content)); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdBack(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.NetworkID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "back requires network_id")
		return
	}
	if err := s.Manager.Back(cmd.NetworkID); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdQuit(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.NetworkID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "quit requires network_id")
		return
	}
	if err := s.Manager.Quit(cmd.NetworkID, strings.TrimSpace(cmd.Content)); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdRejoin(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.BufferID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "rejoin requires buffer_id")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind != ircdb.BufferChannel {
		writeWSErr(ctx, c, cmd.ReqID, "rejoin only works on channel buffers")
		return
	}
	if err := s.Manager.Rejoin(networkID, name); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdNotice(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	target := strings.TrimSpace(cmd.Target)
	content := strings.TrimSpace(cmd.Content)
	if cmd.NetworkID == uuid.Nil || target == "" || content == "" {
		writeWSErr(ctx, c, cmd.ReqID, "notice requires network_id, target, and content")
		return
	}
	if err := s.Manager.Notice(cmd.NetworkID, target, content); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	if err := s.Manager.LogOutbound(ctx, cmd.NetworkID, target, "notice", content); err != nil {
		slog.Error("log outbound notice", "err", err)
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdCTCP(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	target := strings.TrimSpace(cmd.Target)
	content := strings.TrimSpace(cmd.Content)
	if cmd.NetworkID == uuid.Nil || target == "" || content == "" {
		writeWSErr(ctx, c, cmd.ReqID, "ctcp requires network_id, target, and content (COMMAND [args])")
		return
	}
	parts := strings.SplitN(content, " ", 2)
	command := strings.ToUpper(parts[0])
	args := ""
	if len(parts) == 2 {
		args = parts[1]
	}
	if err := s.Manager.CTCP(cmd.NetworkID, target, command, args); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdQuery(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	target := strings.TrimSpace(cmd.Target)
	if cmd.NetworkID == uuid.Nil || target == "" {
		writeWSErr(ctx, c, cmd.ReqID, "query requires network_id and target")
		return
	}
	if _, _, _, err := s.Stores.EnsureBuffer(ctx, cmd.NetworkID, target, ircdb.BufferQuery); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdList(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.NetworkID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "list requires network_id")
		return
	}
	if err := s.Manager.ListChannels(cmd.NetworkID, strings.TrimSpace(cmd.Content)); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdChannelMode(ctx context.Context, c *websocket.Conn, cmd clientCmd, modeStr string) {
	target := strings.TrimSpace(cmd.Target)
	if cmd.BufferID == uuid.Nil || target == "" {
		writeWSErr(ctx, c, cmd.ReqID, cmd.Type+" requires buffer_id and target")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind != ircdb.BufferChannel {
		writeWSErr(ctx, c, cmd.ReqID, cmd.Type+" only works on channel buffers")
		return
	}
	if err := s.Manager.Mode(networkID, name, modeStr, target); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdBanlist(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.BufferID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "banlist requires buffer_id")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind != ircdb.BufferChannel {
		writeWSErr(ctx, c, cmd.ReqID, "banlist only works on channel buffers")
		return
	}
	if err := s.Manager.Mode(networkID, name, "+b"); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdKickban(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	target := strings.TrimSpace(cmd.Target)
	if cmd.BufferID == uuid.Nil || target == "" {
		writeWSErr(ctx, c, cmd.ReqID, "kickban requires buffer_id and target")
		return
	}
	networkID, name, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil || kind != ircdb.BufferChannel {
		writeWSErr(ctx, c, cmd.ReqID, "kickban only works on channel buffers")
		return
	}
	reason := strings.TrimSpace(cmd.Content)
	if err := s.Manager.Mode(networkID, name, "+b", target+"!*@*"); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	if err := s.Manager.Kick(networkID, name, target, reason); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdIgnore(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	mask := strings.TrimSpace(cmd.Target)
	if cmd.NetworkID == uuid.Nil || mask == "" {
		writeWSErr(ctx, c, cmd.ReqID, "ignore requires network_id and target")
		return
	}
	if err := ircdb.CreateIgnore(ctx, s.Stores.Control, cmd.NetworkID, mask); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdUnignore(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	mask := strings.TrimSpace(cmd.Target)
	if cmd.NetworkID == uuid.Nil || mask == "" {
		writeWSErr(ctx, c, cmd.ReqID, "unignore requires network_id and target")
		return
	}
	if err := ircdb.DeleteIgnore(ctx, s.Stores.Control, cmd.NetworkID, mask); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func (s *Server) cmdIgnorelist(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.NetworkID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "ignorelist requires network_id")
		return
	}
	masks, err := ircdb.ListIgnores(ctx, s.Stores.Control, cmd.NetworkID)
	if err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	if masks == nil {
		masks = []string{}
	}
	_ = wsjson.Write(ctx, c, ignoreListResult{
		Type: "ignorelist_result", ReqID: cmd.ReqID,
		NetworkID: cmd.NetworkID, Masks: masks,
	})
}

// resolveTargetNetworkKind tries to resolve the kind of the network this
// command targets, by inspecting cmd.NetworkID first then cmd.BufferID. The
// boolean return distinguishes "resolved to <kind>" from "could not resolve"
// (in which case the caller should not gate, and the inner handler will fail
// with a domain-specific error).
func (s *Server) resolveTargetNetworkKind(ctx context.Context, cmd clientCmd) (string, bool) {
	if s.Stores == nil {
		return "", false
	}
	var networkID uuid.UUID
	switch {
	case cmd.NetworkID != uuid.Nil:
		networkID = cmd.NetworkID
	case cmd.BufferID != uuid.Nil:
		nid, _, _, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
		if err != nil {
			return "", false
		}
		networkID = nid
	default:
		return "", false
	}
	n, err := ircdb.GetNetwork(ctx, s.Stores.Control, networkID)
	if err != nil {
		return "", false
	}
	if n.Kind == "" {
		return ircdb.NetworkKindIRC, true
	}
	return n.Kind, true
}

func writeWSAck(ctx context.Context, c *websocket.Conn, reqID string) {
	_ = wsjson.Write(ctx, c, ackEnvelope{Type: "ack", ReqID: reqID})
}

func writeWSErr(ctx context.Context, c *websocket.Conn, reqID, msg string) {
	_ = wsjson.Write(ctx, c, errorEnvelope{Type: "error", ReqID: reqID, Message: msg})
}

var _ = json.Marshal

type closeFunc func() error

// Close adapts a function to io.Closer.
func (f closeFunc) Close() error { return f() }
