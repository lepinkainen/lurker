package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/internal/httpjson"
)

type apiClient struct {
	baseURL string
	hjc     *httpjson.Client
}

func newAPIClient(baseURL string) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		hjc:     &httpjson.Client{HTTP: &http.Client{}},
	}
}

func (c *apiClient) connectWS(ctx context.Context) (*websocket.Conn, error) {
	wsURL := c.baseURL
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/api/stream"

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	// Default limit is 32 KiB; a history page or a large channel's member_list
	// exceeds that and would close the connection.
	conn.SetReadLimit(8 << 20)
	return conn, nil
}

// startWSReader launches a goroutine that reads events from conn into the
// returned channel. The channel is closed when the connection is lost.
func startWSReader(ctx context.Context, conn *websocket.Conn) <-chan wsEvent {
	ch := make(chan wsEvent, 64)
	go func() {
		defer close(ch)
		for {
			ev, err := readWSEvent(ctx, conn)
			if err != nil {
				return
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// readWSEvent decodes one frame. The `state` frame is the full snapshot and
// shares top-level keys (networks, buffers, members) with other events in
// incompatible shapes, so it is decoded into stateResponse on its own.
func readWSEvent(ctx context.Context, conn *websocket.Conn) (wsEvent, error) {
	var raw json.RawMessage
	if err := wsjson.Read(ctx, conn, &raw); err != nil {
		return wsEvent{}, err
	}
	var head struct {
		Type string `json:"type"`
		Seq  uint64 `json:"seq"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return wsEvent{}, err
	}
	if head.Type == "state" {
		var snap stateResponse
		if err := json.Unmarshal(raw, &snap); err != nil {
			return wsEvent{}, err
		}
		return wsEvent{Type: "state", Seq: head.Seq, Snapshot: &snap}, nil
	}
	var ev wsEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return wsEvent{}, err
	}
	return ev, nil
}

func sendMessage(ctx context.Context, conn *websocket.Conn, bufferID uuid.UUID, content string) error {
	return wsjson.Write(ctx, conn, map[string]any{
		"type":      "send",
		"buffer_id": bufferID,
		"content":   content,
	})
}

// sendWSCmd sends an arbitrary command payload over the websocket.
func sendWSCmd(ctx context.Context, conn *websocket.Conn, cmd map[string]any) error {
	return wsjson.Write(ctx, conn, cmd)
}
