# Lurker

Personal IRCCloud-style bouncer + web client backend for a single user on a private network (Tailscale). Inspired by [IRCCloud](https://www.irccloud.com/) and [ZNC](https://znc.in/).

See also:

- [PROJECT.md](PROJECT.md) for project purpose and direction
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

## Run locally

```bash
task dev
# or
DATA_DIR=./data ADDR=:8080 go run .
```

## Run in Docker

```bash
task up
task down
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

- `GET /health`
- `GET /healthz`
- `GET /whoami`
- `GET /api/state`
- `GET /api/buffers/:id/history`
- `GET /api/search?q=...`
- `POST /api/networks`
- `PATCH /api/networks/:id`
- `DELETE /api/networks/:id`
- `POST /api/networks/:id/connect`
- `POST /api/networks/:id/disconnect`

### WebSocket

Endpoint:

- `/api/stream`

Used for:

- live message and buffer/network state events
- send/join/part/history/mark_read commands

## Build and CI

Preferred local workflow uses `task`:

```bash
task lint
task test
task build
```

Build artifacts are written to `build/`:

- `build/irc-service`
- `build/irc-service-linux-amd64`

Service metadata is available at `/whoami` and includes:

- name
- version
- git hash
- build time

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
