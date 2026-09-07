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
- active channel rows honor the backend's manual `(sort_order, name)` order; queries and archived buffers remain alphabetical
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
2. connect to `/api/stream`
3. fetch `/api/state` (subscribe-then-snapshot, so nothing published in between is missed)
4. populate networks, buffers, topics, and `initial_messages`
5. consume WebSocket events until disconnect or quit

Every reconnect repeats steps 3–4: the snapshot replaces local networks, buffers, unread counts, members and each buffer's recent message window, so state missed while the socket was down is recovered. Live events arriving between connect and snapshot are queued and replayed after the snapshot is applied. The guards against double-applying live in the event handlers themselves, so they also cover events still in flight when the snapshot lands: message events at/below the per-buffer newest snapshot id are dropped (in the window, or older and already in its unread totals — the server reads both in one transaction so they agree), mark_read echoes whose `last_seen_id` does not advance past the current one are ignored (max-wins), and `buffer_created` for a known buffer is a no-op. Snapshot results are tagged with a per-connection generation; a result or retry from a superseded connection is ignored. If the pending queue overflows (1000 events) the in-flight snapshot is superseded and a fresh one is fetched. A failed snapshot fetch retries every 5s and shows "State sync failed" in the status line (also on the initial loading screen). Until the snapshot lands the status line reads "Syncing state…".

An equal-position `mark_read` response is accepted while a local optimistic acknowledgement awaits confirmation, so server-provided residual unread counts and the marker can be restored. Applying a snapshot clears that pending acknowledgement and resets history-loading flags, including requests lost on disconnect or queue overflow. Pagination is available again after synchronization finishes. A disconnect invalidates outstanding snapshot results and retries immediately.

Consumed WebSocket events:

- `message`: insert in ID order, skip rows already loaded through history, and refresh the active viewport
- `buffer_update`: update joined state and topic
- `buffer_reorder`: apply live per-network channel ordering updates
- `network_state`: update displayed network state
- `buffer_created`: append a new buffer with its server-assigned channel order and rebuild the sidebar

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
