# Lurker architecture

This document is for AI agents and other technical readers. It gives the system overview and links to focused documents for each subsystem.

## Document index

- [storage.md](storage.md) — control DB, per-network log DBs, preview cache DB, network + buffer models
- [rest-api.md](rest-api.md) — HTTP endpoints under `/api/*` plus `/whoami` and health checks
- [websocket-protocol.md](websocket-protocol.md) — `/api/stream` commands, acks, event shapes
- [irc-runtime.md](irc-runtime.md) — `irc.Manager`, handler, event persistence, URL preview pipeline, event hub
- [frontend.md](frontend.md) — web UI layout, source-of-truth rules, hydration model
- [macos.md](macos.md) — native SwiftUI client, endpoint policy, state model, packaging
- [theming.md](theming.md) — YAML themes, loader, `GET /api/themes`, how to add a theme
- [tui.md](tui.md) — terminal UI client, configuration, keyboard controls, API usage
- [keyboard-shortcuts.md](keyboard-shortcuts.md) — channel switcher and keyboard shortcut UX spec
- [testing-and-build.md](testing-and-build.md) — testing strategy and Taskfile workflow
- [operations.md](operations.md) — operational notes including image update checking

## Purpose and scope

Lurker is a private, single-user IRC bouncer plus web client backend.

Current in-repo scope:

- Go backend service
- disk-served web UI
- native SwiftUI client for macOS and iOS (shared sources)
- terminal UI client
- control-plane SQLite database
- one SQLite log database per network
- REST API for state, history, search, and network management
- WebSocket stream for live updates and client commands

Non-goals unless explicitly requested:

- multi-user support
- public-internet deployment assumptions
- app-layer authentication
- SaaS-style tenancy or account models

## High-level architecture

Main components:

- `main.go`: process setup, config loading, DB open, IRC manager startup, HTTP server startup
- `config.go`: environment and `config.yaml` bootstrap loading
- `api/`: REST and WebSocket API surface
- `irc/`: persistent IRC connection lifecycle and event handling
- `db/`: control DB, per-network log DBs, migrations, and query helpers
- `hub/`: in-process pub/sub used to fan live events to WebSocket clients
- `updates/`: background checker for published Linux container image metadata
- `web/`: Vite + TypeScript frontend
- `macos/`: Xcode project for the native SwiftUI client (one target builds macOS and iOS)
- `cmd/tui/`: terminal UI client for the backend

Runtime flow:

1. process loads config from env and optional `config.yaml`
2. control DB and configured per-network log DBs are opened
3. bootstrap networks from `config.yaml` are upserted into control DB and started
4. HTTP API and web UI are served from the same Go process
5. IRC events resolve buffers through `MultiStore.EnsureBuffer`, persist to SQLite with UUIDv7 IDs, and publish to the hub
6. background update checker polls GHCR image metadata and caches latest status in memory
7. web, terminal, and native (macOS/iOS) clients consume the existing REST and WebSocket APIs
8. WebSocket clients receive hub events and issue commands back to the backend

## Configuration model

Primary config inputs:

- `DATA_DIR` default `./data`
- `ADDR` default `:8080`
- `CONFIG_PATH` default `./config.yaml`
- CLI flag `--web-dir` to serve built frontend from disk
- `UPDATE_CHECK_*` env vars for optional GHCR image update polling, default daily and clamped to no more than once per hour
- `UPLOAD_DIR` default `./data/uploads`
- `UPLOAD_MAX_BYTES` default `20971520`
- `UPLOAD_BASE_URL` optional override for returned upload URLs

### Important invariant: `config.yaml` is the source of truth at boot

Every backend startup returns to the state defined by `config.yaml`. Changes
made through the API are ephemeral — they apply immediately but only last
until the next restart.

On startup:

- YAML-defined networks are upserted into control DB (connection fields
  overwritten from YAML, `disabled` reset to `0`) and started
- networks absent from YAML (including any created via the API) are marked
  `disabled=1`; their log DBs are kept
- runtime-owned state survives: `sort_order`, `created_at`

After startup:

- network definitions can be created/edited through the API and stored in
  `control.db`, but they revert per the rules above on the next boot
- connect/disconnect and the `disabled` toggle are managed through the API,
  effective until restart
- clients see `in_config` on each network (from `Server.ConfigNetworkNames`)
  and warn the user when state won't survive a restart
- settings with no config key (per-buffer view options etc.) are UI/DB-owned
  and are not touched by boot reconciliation

## Process startup and lifecycle

From `main.go`:

- load config
- open `MultiStore`
- create event hub
- create IRC manager
- start bootstrap IRC networks if configured
- serve API and UI
- on shutdown, stop HTTP server and wait for IRC runtimes to exit

Web serving modes:

- default: API-only mode; no web UI served from `/`
- `--web-dir ./web/dist`: serve built frontend from disk
- container image: starts with `--web-dir /app/web/dist`

## Privacy and deployment assumptions

Assumptions baked into the current architecture:

- single user
- trusted private network only
- no app-layer authentication
- not intended for public exposure

Typical deployment expectations:

- run on loopback or Tailscale-reachable host
- persist the entire `data/` directory
- optionally serve built frontend from the same Go binary

## Important invariants and gotchas

- `config.yaml` is the boot source of truth; API-made network changes are ephemeral and revert on restart
- backend message logs are retained indefinitely; do not add automatic message retention or periodic cleanup that deletes history
- preserve `data/` to keep control DB and all log DBs
- network names should be treated as stable
- buffer/network/message IDs are UUIDv7 values serialized as strings in API payloads
- buffer IDs are shared between `control.db.buffer_registry` and each per-network `buffers` table; `MultiStore.EnsureBuffer` is the authoritative creation path
- deleting a network does not delete its historical log DB file
- sidebar network order is persisted in `networks.sort_order`
- `/whoami` is the preferred endpoint for identifying a running instance
