# irc-service — Personal IRCCloud-style IRC client

A self-hosted, single-user IRC bouncer + web client backend. The backend stays
connected 24/7, logs everything to SQLite, and exposes a streaming API over a
private network.

## Goals

- Keep IRC connections alive when no client is attached.
- Persist all history to SQLite with FTS-backed search.
- Support modern IRC features where practical: IRCv3 tags, SASL, server-time,
  msgid dedup, echo-message/labeled-response.
- Keep clients thin: they render backend state and ask for history from the API.
- Ship a usable in-repo web UI as the v1 client.

## Non-goals (current scope)

- No authentication layer in the app itself.
- No multi-user support.
- No public internet deployment story.
- No in-repo TUI or macOS client implementation.
- No mobile, push notifications, uploads, or integrations.

## Architecture

```
                   ┌──────────────────────────────┐
                   │      irc-service (Go)        │
                   │                              │
  IRC servers ◄──► │  IRC manager                │
                   │  - one runtime per network  │
                   │  - connect/disconnect API   │
                   │              │               │
                   │              ▼               │
                   │     in-proc event hub        │
                   │       │              │       │
                   │       ▼              ▼       │
                   │  SQLite stores    WebSocket  │
                   │  - control.db        hub     │
                   │  - <network>.db              │
                   └──────────┬───────────┬───────┘
                              │           │
                        /data │           │ :8080
                              ▼           ▼
                 control.db + per-network  Web UI
                     log databases
```

### Main components

1. **IRC manager**
   - owns one logical runtime per network
   - supports runtime connect/disconnect
   - tracks `connecting`, `connected`, `disconnected`
   - performs reconnect with backoff

2. **Control DB**
   - stores network definitions
   - stores the global buffer registry
   - owns stable public `network_id` and `buffer_id`

3. **Per-network log DBs**
   - one SQLite DB per network
   - store local buffers, messages, FTS, topics, joined state, and last seen ids

4. **HTTP + WebSocket API**
   - REST for state, history, search, and network management
   - WebSocket for live events and client commands

5. **Web UI**
   - plain HTML/CSS/JS
   - no build step
   - current in-repo client for v1

## Storage model

All state lives under `/data`.

### `control.db`

Purpose:

- network definitions
- case-insensitive network uniqueness
- stable global `network_id`
- global `buffer_registry` for stable public `buffer_id`

### Per-network log DBs

Path shape:

- `/data/<network-name-lowercased>.db`

Examples:

- `/data/libera.db`
- `/data/ircnet.db`

Purpose:

- local buffers
- message history
- FTS indexes and triggers
- per-buffer topic
- per-buffer joined state
- per-buffer `last_seen_id`

### Network naming and filenames

Validation rule:

```regex
^[A-Za-z0-9_-]+$
```

Rules:

- names are case-insensitively unique
- filename is lowercase name + `.db`
- no extra slugging/normalization in v1

Examples:

- `Libera` → `libera.db`
- `OFTC-test` → `oftc-test.db`

## API surface

### REST

- `GET /api/state`
- `GET /api/buffers/:id/history?before=<id>&limit=<n>`
- `GET /api/search?q=...&network=...&buffer=...`
- `POST /api/networks`
- `PATCH /api/networks/:id`
- `DELETE /api/networks/:id`
- `POST /api/networks/:id/connect`
- `POST /api/networks/:id/disconnect`

Delete behavior is intentionally conservative:

- remove control-plane metadata
- stop runtime connection
- retain the per-network log DB by default

### WebSocket `/api/stream`

Server events include:

- `message`
- `buffer_created`
- `buffer_update`
- `network_state`

Client commands include:

- `send`
- `join`
- `part`
- `history`
- `mark_read`

## Web UI scope

The current v1 web UI supports:

- initial snapshot via `/api/state`
- live updates via `/api/stream`
- buffer switching
- send message
- `/join` and `/part`
- scrollback loading
- persisted unread/read tracking via `last_seen_id`
- minimal search UI

## Deployment and privacy assumptions

The app is meant for a trusted private environment.

Assumptions:

- single user
- no in-app auth
- not internet-facing
- exposed only through loopback, Tailnet, or equivalent private networking
- TODO: Make the web UI "tailnet" indicator functional or relabel/remove it; it is currently a static decorative badge, not actual connectivity/detection state.

Typical deployment:

- container or local binary
- persistent `/data`
- local bind/publish
- optional Tailscale exposure

## Backup and restore

The state to preserve is the entire `/data` directory:

- `control.db`
- every per-network `*.db`
- any SQLite `-wal` and `-shm` side files

Safer backup strategies:

- stop the service and copy `/data`
- or use SQLite `.backup` per DB file

## Current scope summary

This repo currently centers on:

- one backend service
- one in-repo web UI
- control-plane metadata in `control.db`
- log/history isolation via one DB per network
- runtime-manageable network lifecycle through the API

Future clients may exist later, but they are not part of the current in-repo
scope or implementation plan.
