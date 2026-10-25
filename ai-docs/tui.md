# Text-mode UI

The text-mode UI is a terminal client for a running Lurker backend. It lives in `cmd/tui/` and is built as a separate binary from the main backend service.

## Purpose and scope

The TUI is a private-network client for the existing single-user backend. It does not open IRC connections or read SQLite directly. All state and live events come through the backend HTTP/WebSocket API.

Current capabilities:

- connect to `/api/stream`; the state snapshot (or a replay) arrives over it in answer to `hello`
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

The Bubble Tea model in `cmd/tui/model*.go` renders an alternate-screen terminal UI:

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
2. connect to `/api/stream` and send `hello {since: lastSeq}` as the first frame (`lastSeq` is 0 on startup)
3. the server answers with a replay of the missed events or with a `state` frame; `applyState` installs the latter (networks, buffers, topics, counts, members, `initial_messages`)
4. consume WebSocket events until disconnect or quit

Every reconnect repeats steps 2–3 with the current cursor, so state missed while the socket was down comes back either as a replay or as a fresh snapshot. `readWSEvent` decodes the `state` frame separately (its `networks`/`buffers`/`members` keys collide with other events' shapes) and `handleWSEvent` applies one rule to every sequenced frame: `seq` at or below `lastSeq` is dropped, anything newer applies and advances the cursor. There is no pending queue, snapshot boundary, optimistic-ack bookkeeping or per-connection generation anymore — stream order makes them unnecessary (`formal/ClientSync.tla`). Until the first frame lands the status line reads "Syncing state…". Applying a snapshot resets history-loading flags, so pagination is available again after a reconnect.

Consumed WebSocket events:

- `message`: insert in ID order, skip rows already loaded through history, and refresh the active viewport
- `buffer_update`: update joined state and topic
- `buffer_reorder`: apply live per-network channel ordering updates
- `network_state`: update displayed network state
- `network_created` / `network_updated` / `network_deleted` / `network_reorder`: upsert, drop (with its buffers) or reorder networks in the sidebar
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
- `cmd/tui/client.go` — WebSocket connection, frame decoding (`state` vs events), event reader, send command
- `cmd/tui/model.go` — Bubble Tea model struct, `Init`/`Update`, WS commands and the hello handshake, state application, read tracking
- `cmd/tui/model_input.go` — key and mouse handling, switcher keys, input submit, history, nick autocomplete
- `cmd/tui/model_events.go` — WebSocket event application (seq gate, state frame, messages, buffers, networks, history results)
- `cmd/tui/model_render.go` — viewport, unread bar, message formatting, `View` and pane rendering
- `cmd/tui/sidebar.go` — sidebar item construction, navigation, archive fold, selection
- `cmd/tui/types.go` — API DTOs, WebSocket event union, Bubble Tea messages
