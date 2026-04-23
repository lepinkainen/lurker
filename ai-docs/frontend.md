# Frontend architecture

Frontend lives in `web/` and is intentionally simple.

Current characteristics:

- Vite + TypeScript
- mostly single-file app logic in `web/src/main.ts`
- local UI state in memory plus some localStorage-backed layout preferences
- server is source of truth for networks, buffers, messages, and read state

## Source-of-truth rules

Server-backed state:

- networks
- network ordering via `sort_order`
- buffers
- messages
- read state
- member lists

Client-only persisted layout state:

- collapsed sections
- pinned buffers

Important invariant:

- network ordering is no longer a frontend-only preference; it is shared persistent state stored in the control DB

## Hydration model

On load:

1. fetch `/api/state`
2. populate maps for networks, buffers, messages, and members
3. infer unread counts client-side from `last_seen_id`
4. connect WebSocket stream
5. apply incoming events incrementally

See [rest-api.md](rest-api.md) for `/api/state` and [websocket-protocol.md](websocket-protocol.md) for incremental events.
