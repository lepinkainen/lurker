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

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/internal/closeutil"
)

// clientCmd is the set of verbs a client can send. We union-decode from
// JSON into this struct and dispatch on Type; fields irrelevant to the
// chosen verb are simply ignored.
type clientCmd struct {
	Type      string `json:"type"`
	ReqID     string `json:"req_id,omitzero"`
	BufferID  int64  `json:"buffer_id,omitzero"`
	NetworkID int64  `json:"network_id,omitzero"`
	Channel   string `json:"channel,omitzero"`
	Content   string `json:"content,omitzero"`
	Before    int64  `json:"before,omitzero"`
	Limit     int    `json:"limit,omitzero"`
	MessageID int64  `json:"message_id,omitzero"`
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
	BufferID int64        `json:"buffer_id"`
	Messages []messageDTO `json:"messages"`
}

// stream is the WebSocket endpoint. It subscribes to the event hub and
// forwards every published event as JSON; it also reads client commands
// and dispatches them to the IRC manager or the SQLite store.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	// The service is expected to be reachable only via loopback or a
	// Tailnet IP, so we don't bother with Origin checks.
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

	// writer goroutine: forwards hub events to the socket. A 10s write
	// deadline prevents one wedged client from hanging the server.
	done := make(chan struct{})
	go func() {
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
	}()

	// reader loop: run in the request goroutine so we exit cleanly when
	// the client disconnects.
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

func (s *Server) handleCmd(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	switch cmd.Type {
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
	default:
		writeWSErr(ctx, c, cmd.ReqID, "unknown command: "+cmd.Type)
	}
}

func (s *Server) cmdSend(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	content := strings.TrimSpace(cmd.Content)
	if cmd.BufferID == 0 || content == "" {
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
	if cmd.BufferID == 0 {
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
	if cmd.NetworkID == 0 || cmd.Channel == "" {
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
	if cmd.BufferID == 0 {
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
	if cmd.BufferID == 0 || cmd.MessageID == 0 {
		writeWSErr(ctx, c, cmd.ReqID, "mark_read requires buffer_id and message_id")
		return
	}
	if err := s.Stores.MarkBufferLastSeen(ctx, cmd.BufferID, cmd.MessageID); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, err.Error())
		return
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

func writeWSAck(ctx context.Context, c *websocket.Conn, reqID string) {
	_ = wsjson.Write(ctx, c, ackEnvelope{Type: "ack", ReqID: reqID})
}

func writeWSErr(ctx context.Context, c *websocket.Conn, reqID, msg string) {
	_ = wsjson.Write(ctx, c, errorEnvelope{Type: "error", ReqID: reqID, Message: msg})
}

// Unused but exposed so json.Marshal errors in hub events surface in
// tests rather than silently dropping bytes on the wire.
var _ = json.Marshal

type closeFunc func() error

// Close adapts a function to io.Closer.
func (f closeFunc) Close() error { return f() }
