# irc-service

Personal IRCCloud-style bouncer + web client backend for a single user on a
private network.

See also:
- [PLAN.md](PLAN.md) for the current architecture overview
- [IMPLEMENTATION_SPEC.md](IMPLEMENTATION_SPEC.md) for the storage refactor details

## Status

Current implemented scope includes:
- persistent IRC connections per network
- control-plane SQLite database plus one log SQLite database per network
- REST API for state, history, search, and network CRUD
- WebSocket stream for live events and commands
- minimal in-repo web UI with history loading, slash commands, persisted read state, and search

In-repo clients are intentionally limited to the web UI for v1.

## Storage layout

State lives under `DATA_DIR` (default `./data`):

- `control.db` — control-plane metadata
  - network definitions
  - stable global `network_id`
  - stable global `buffer_id` registry
- `<network>.db` — one per-network log database
  - buffers
  - messages
  - FTS indexes
  - joined/topic/read state

Examples:
- `./data/control.db`
- `./data/libera.db`
- `./data/ircnet.db`

### Network naming rules

Network names must match:

```regex
^[A-Za-z0-9_-]+$
```

That means:
- allowed: letters, digits, `_`, `-`
- rejected: spaces, dots, slashes, and unicode-heavy names
- uniqueness is case-insensitive

Filename derivation is:
- lowercase the network name
- append `.db`

Examples:
- `Libera` → `libera.db`
- `IRCNet` → `ircnet.db`

## Backup and restore

Because SQLite runs in WAL mode, backup should include:
- `control.db`
- any `*.db-wal` and `*.db-shm` side files present
- every per-network `*.db` plus matching `-wal` / `-shm` files

Safer options:
- stop the service, then copy the whole `DATA_DIR`
- or use SQLite `.backup` tooling per database file

Restore is just restoring the full `DATA_DIR` contents.

## Run locally

```bash
make dev
# or
DATA_DIR=./data ADDR=:8080 go run .
```

## Run in Docker

```bash
make up
make down
```

The service expects `./data` to be persisted as a bind mount so both
`control.db` and per-network log DBs survive restarts.

## Config

Bootstrap config comes from:
- `DATA_DIR` — default `./data`
- `ADDR` — default `:8080`
- `CONFIG_PATH` — default `./config.yaml`

`config.yaml` is bootstrap-only seed input.

On startup:
- networks from YAML are inserted into `control.db` if missing
- seeded networks are started

After startup:
- network definitions are managed through the API
- connect/disconnect is managed through the API
- YAML and env/config are not treated as the runtime source of truth

## API

### REST

- `GET /healthz`
- `GET /api/state`
- `GET /api/buffers/:id/history`
- `GET /api/search?q=...`
- `POST /api/networks`
- `PATCH /api/networks/:id`
- `DELETE /api/networks/:id`
- `POST /api/networks/:id/connect`
- `POST /api/networks/:id/disconnect`

Delete semantics:
- remove the network from `control.db`
- remove associated global buffer registry entries
- stop the live IRC connection
- retain the per-network log DB file on disk by default

### WebSocket

Endpoint:
- `/api/stream`

Used for:
- live message and buffer/network state events
- send/join/part/history/mark_read commands

## Web UI

The in-repo client is a small no-build web UI under `web/`.

Current behavior includes:
- network and buffer sidebar
- live message stream
- infinite-scroll-up history loading
- `/join` and `/part` slash commands
- persisted read state via `mark_read` and `last_seen_id`
- topic/joined-state live updates from `buffer_update`
- minimal search UI

## Deployment and privacy assumptions

This project is intentionally designed for private single-user deployment.

Assumptions:
- no in-app authentication
- service should not be exposed on the public internet
- access should be limited to loopback, Tailnet, or equivalent private network paths

Recommended deployment pattern:
- bind/publish locally
- expose only through Tailscale or another trusted private network layer

For Docker, prefer loopback publishing such as:
- `127.0.0.1:8080:8080`

## Scope notes

Out of scope for the current repo scope:
- in-repo TUI client
- in-repo macOS/Swift client
- mobile clients
- multi-user auth
- public internet deployment
