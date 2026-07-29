package api

import (
	"context"
	"errors"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
)

// bufferDeletedEvent announces that a buffer and its entire history are gone.
// Clients drop all per-buffer state and reselect if it was active.
type bufferDeletedEvent struct {
	Type      string    `json:"type"`
	ID        uuid.UUID `json:"id"`
	NetworkID uuid.UUID `json:"network_id"`
}

// cmdDeleteBuffer permanently deletes a buffer. Only archived buffers may be
// deleted: status buffers never, channels must be parted (which archives
// them), queries must be explicitly archived first.
func (s *Server) cmdDeleteBuffer(ctx context.Context, c *websocket.Conn, cmd clientCmd) {
	if cmd.BufferID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, "delete_buffer requires buffer_id")
		return
	}
	networkID, _, kind, err := s.Stores.LookupBuffer(ctx, cmd.BufferID)
	if err != nil {
		writeWSErr(ctx, c, cmd.ReqID, "unknown buffer")
		return
	}
	if kind == ircdb.BufferStatus {
		writeWSErr(ctx, c, cmd.ReqID, "cannot delete status buffers")
		return
	}
	settings, err := ircdb.GetBufferSettings(ctx, s.Stores.Control, cmd.BufferID)
	if err != nil {
		writeWSErr(ctx, c, cmd.ReqID, "unknown buffer")
		return
	}
	if !settings.Archived {
		writeWSErr(ctx, c, cmd.ReqID, "buffer must be archived before it can be deleted")
		return
	}
	if err := s.Stores.DeleteBuffer(ctx, cmd.BufferID); err != nil {
		writeWSErr(ctx, c, cmd.ReqID, "delete buffer: "+err.Error())
		return
	}
	if s.Hub != nil {
		s.Hub.Publish(bufferDeletedEvent{Type: "buffer_deleted", ID: cmd.BufferID, NetworkID: networkID})
	}
	writeWSAck(ctx, c, cmd.ReqID)
}

// cmdArchiveBuffer sets or clears the archived flag over WS. Channels are
// normally archived implicitly by part/kick; this is the manual path used by
// /archive, /unarchive and query-buffer menus.
func (s *Server) cmdArchiveBuffer(ctx context.Context, c *websocket.Conn, cmd clientCmd, archived bool) {
	if cmd.BufferID == uuid.Nil {
		writeWSErr(ctx, c, cmd.ReqID, cmd.Type+" requires buffer_id")
		return
	}
	settings, err := ircdb.UpdateBufferSettings(ctx, s.Stores.Control, cmd.BufferID, ircdb.BufferSettingsPatch{Archived: &archived})
	if err != nil {
		switch {
		case errors.Is(err, ircdb.ErrBufferNotFound):
			writeWSErr(ctx, c, cmd.ReqID, "unknown buffer")
		case errors.Is(err, ircdb.ErrBufferSettingsUnsupported):
			writeWSErr(ctx, c, cmd.ReqID, "status buffers cannot be archived")
		default:
			writeWSErr(ctx, c, cmd.ReqID, "archive buffer: "+err.Error())
		}
		return
	}
	if s.Hub != nil {
		s.Hub.Publish(bufferSettingsEventFrom(settings))
	}
	writeWSAck(ctx, c, cmd.ReqID)
}
