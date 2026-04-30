# Text-mode UI

The text-mode UI is a terminal client for a running Lurker backend. It lives in `cmd/tui/` and is built as a separate binary from the main backend service.

## Purpose and scope

The TUI is a private-network client for the existing single-user backend. It does not open IRC connections or read SQLite directly. All state and live events come through the backend HTTP/WebSocket API.

Current capabilities:

- fetch initial state from `/api/state`
- connect to `/api/stream` for live events
- render networks and buffers in a left sidebar
- render the active buffer's recent messages in the main pane
- show channel topics in the header when available
- send messages to the active buffer over the WebSocket `send` command
- reconnect the WebSocket after disconnects

Non-goals unless explicitly requested:

- standalone/offline mode
- direct database access
- app-layer authentication
- multi-user identity management

## Running and building

Preferred Taskfile targets:

```bash
task tui

task build-tui
```

`task tui` runs the client with `go run ./cmd/tui`.
`task build-tui` writes `build/lurker-tui`.

Linux release-style builds use:

```bash
task build-tui-linux
```

which writes `build/lurker-tui-linux-amd64`.

The main `task build` target also builds the TUI via its `build-tui` dependency.

## Configuration

The TUI accepts one optional flag:

```bash
go run ./cmd/tui --config ./path/to/tui.yaml
```

Config lookup order:

1. explicit `--config` path, if provided
2. `./tui-config.yaml`
3. `~/.config/lurker/tui.yaml`
4. built-in defaults if no config file exists

Only one option is currently supported:

```yaml
backend_url: http://localhost:8080
```

`backend_url` defaults to `http://localhost:8080`. The client derives the WebSocket URL by converting `http://` to `ws://`, `https://` to `wss://`, and appending `/api/stream`.

A template is available at `tui-config.yaml.example`.

## UI layout

The Bubble Tea model in `cmd/tui/model.go` renders an alternate-screen terminal UI:

- left sidebar: enabled networks and their buffers
- header: active buffer name and topic
- message viewport: formatted recent messages
- input area: one-line message entry
- status line: backend connection and error/reconnect state

Message formatting mirrors IRC-style conventions:

- `privmsg`: `<sender> content`
- `notice`: `-sender- content`
- `action`: `* sender content`
- other events: fallback system-style lines

Timestamps are displayed in local time as `[HH:MM]`.

## Keyboard controls

| Key | Behavior |
| --- | --- |
| `Ctrl+C` | Quit and close the WebSocket |
| `Tab` | Toggle focus between input and sidebar |
| `Up` / `Down` in sidebar focus | Move buffer selection, skipping network headers |
| `Enter` in sidebar focus | Activate selected buffer and return focus to input |
| `Enter` in input focus | Send non-empty input to the active buffer |
| `Up` / `Down` in input focus | Scroll the message viewport |
| `PgUp` / `PgDown` | Half-page scroll the message viewport |

## Backend API usage

Startup flow:

1. load TUI config
2. fetch `/api/state`
3. populate networks, buffers, topics, and `initial_messages`
4. connect to `/api/stream`
5. consume WebSocket events until disconnect or quit

Consumed WebSocket events:

- `message`: append to the buffer's message list and refresh the active viewport
- `buffer_update`: update joined state and topic
- `network_state`: update displayed network state
- `buffer_created`: append new buffer and rebuild the sidebar

Sent WebSocket commands:

```json
{
  "type": "send",
  "buffer_id": 123,
  "content": "hello"
}
```

The client currently ignores ack responses except as generic WebSocket events with no UI effect.

## Source map

- `cmd/tui/main.go` — CLI flag parsing, config load, Bubble Tea program startup
- `cmd/tui/config.go` — YAML config lookup and defaults
- `cmd/tui/client.go` — `/api/state`, WebSocket connection, event reader, send command
- `cmd/tui/model.go` — Bubble Tea model, layout, key handling, rendering, event application
- `cmd/tui/types.go` — API DTOs, WebSocket event union, Bubble Tea messages
