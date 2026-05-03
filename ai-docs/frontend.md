# Frontend architecture

Frontend lives in `web/` and is intentionally simple.

Current characteristics:

- Vite + TypeScript
- mostly single-file app logic in `web/src/main.ts`
- local UI state in memory plus some localStorage-backed layout preferences
- server is source of truth for networks, buffers, messages, and read state

## Source-of-truth rules

Server-backed state:

- networks (including `disabled` flag)
- network ordering via `sort_order`
- buffers
- buffer settings (`show_embeds`, `show_presence_events`, `collapse_presence_events`, `pinned`)
- messages
- read state
- member lists

Client-only persisted layout state:

- collapsed sections
- sidebar visibility
- members drawer state

Important invariants:

- network ordering is no longer a frontend-only preference; it is shared persistent state stored in the control DB
- `pinned` is stored server-side in `buffer_settings` so it follows the user across browsers/devices
- other buffer settings (`show_embeds`, `show_presence_events`, `collapse_presence_events`) are also server-persisted

## Hydration model

On load:

1. fetch `/api/state`
2. populate maps for networks, buffers (including settings fields), messages, and members
3. infer unread counts client-side from `last_seen_id`
4. restore sidebar visibility from localStorage
5. connect WebSocket stream
6. apply incoming events incrementally (including `buffer_settings` for live settings sync)

Buffer settings are mutated via `PATCH /api/buffers/{id}/settings` and the server publishes a `buffer_settings` hub event so all open clients stay in sync.

See [rest-api.md](rest-api.md) for `/api/state` and [websocket-protocol.md](websocket-protocol.md) for incremental events.

## Interaction specs

- [keyboard-shortcuts.md](keyboard-shortcuts.md) defines the v1 keyboard shortcut set and channel switcher behavior.
