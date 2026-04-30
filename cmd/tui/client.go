package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type apiClient struct {
	baseURL string
	http    *http.Client
}

func newAPIClient(baseURL string) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
}

func (c *apiClient) fetchState(ctx context.Context) (*stateResponse, error) {
	resp, err := c.http.Get(c.baseURL + "/api/state")
	if err != nil {
		return nil, fmt.Errorf("fetch state: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("parse state: server returned %d: %s", resp.StatusCode, preview)
	}

	var state stateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("parse state: %w (body: %s)", err, preview)
	}
	return &state, nil
}

func (c *apiClient) connectWS(ctx context.Context) (*websocket.Conn, error) {
	wsURL := c.baseURL
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/api/stream"

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
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

func sendMessage(ctx context.Context, conn *websocket.Conn, bufferID int64, content string) error {
	return wsjson.Write(ctx, conn, map[string]any{
		"type":      "send",
		"buffer_id": bufferID,
		"content":   content,
	})
}
