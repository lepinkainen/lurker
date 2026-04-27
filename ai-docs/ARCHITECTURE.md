# Lurker architecture

This document is for AI agents and other technical readers. It gives the system overview and links to focused documents for each subsystem.

## Document index

- [storage.md](storage.md) — control DB, per-network log DBs, preview cache DB, network + buffer models
- [rest-api.md](rest-api.md) — HTTP endpoints under `/api/*` plus `/whoami` and health checks
- [websocket-protocol.md](websocket-protocol.md) — `/api/stream` commands, acks, event shapes
- [irc-runtime.md](irc-runtime.md) — `irc.Manager`, handler, event persistence, URL preview pipeline, event hub
- [frontend.md](frontend.md) — web UI layout, source-of-truth rules, hydration model
- [testing-and-build.md](testing-and-build.md) — testing strategy and Taskfile workflow
- [operations.md](operations.md) — operational notes including image update checking

## Purpose and scope

Lurker is a private, single-user IRC bouncer plus web client backend.

Current in-repo scope:

- Go backend service
- disk-served web UI
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
- `updates/`: background checker for published container image metadata
- `web/`: Vite + TypeScript frontend

Runtime flow:

1. process loads config from env and optional `config.yaml`
2. control DB and configured per-network log DBs are opened
3. bootstrap networks from `config.yaml` are upserted into control DB and started
4. HTTP API and web UI are served from the same Go process
5. IRC events are persisted to SQLite and published to the hub
6. background update checker polls GHCR image metadata and caches latest status in memory
7. WebSocket clients receive hub events and issue commands back to the backend

## Configuration model

Primary config inputs:

- `DATA_DIR` default `./data`
- `ADDR` default `:8080`
- `CONFIG_PATH` default `./config.yaml`
- CLI flag `--web-dir` to serve built frontend from disk
- `UPDATE_CHECK_*` env vars for optional GHCR image update polling

### Important invariant: `config.yaml` is bootstrap-only

`config.yaml` is seed input, not runtime source of truth.

On startup:

- YAML-defined networks are inserted into control DB if missing
- those seeded networks are started

After startup:

- network definitions are managed through the API and stored in `control.db`
- connect/disconnect is managed through the API
- YAML is not the live config database

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

- `config.yaml` is seed input only after startup
- preserve `data/` to keep control DB and all log DBs
- network names should be treated as stable
- global buffer IDs come from control DB, but messages live in per-network DBs
- deleting a network does not delete its historical log DB file
- sidebar network order is persisted in `networks.sort_order`
- `/whoami` is the preferred endpoint for identifying a running instance
