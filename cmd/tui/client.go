package main

import (
	"context"
	"errors"
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

func (c *apiClient) fetchState(ctx context.Context) (*stateResponse, error) {
	var state stateResponse
	if err := c.hjc.DoJSON(ctx, httpjson.Request{URL: c.baseURL + "/api/state"}, &state); err != nil {
		var herr *httpjson.Error
		if errors.As(err, &herr) {
			preview := string(herr.Body)
			if len(preview) > 200 {
				preview = preview[:200]
			}
			return nil, fmt.Errorf("fetch state: server returned %d: %s", herr.Status, preview)
		}
		return nil, fmt.Errorf("fetch state: %w", err)
	}
	return &state, nil
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
	return conn, nil
}

// startWSReader launches a goroutine that reads events from conn into the
// returned channel. The channel is closed when the connection is lost.
func startWSReader(ctx context.Context, conn *websocket.Conn) <-chan wsEvent {
	ch := make(chan wsEvent, 64)
	go func() {
		defer close(ch)
		for {
			var ev wsEvent
			if err := wsjson.Read(ctx, conn, &ev); err != nil {
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
