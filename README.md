# Lurker

Personal IRCCloud-style bouncer + web client backend for a single user on a private network (Tailscale). Inspired by [IRCCloud](https://www.irccloud.com/) and [ZNC](https://znc.in/).

See also:

- [PROJECT.md](PROJECT.md) for project purpose and direction
- [ai-docs/ARCHITECTURE.md](ai-docs/ARCHITECTURE.md) for technical architecture and API notes

## Status

Current implemented scope includes:

- persistent IRC connections per network
- control-plane SQLite database plus one log SQLite database per network
- shared URL-preview cache database (images + OpenGraph) rendered inline in the web UI
- REST API and WebSocket stream for the web UI
- minimal in-repo web UI with history loading, slash commands, persisted read state, and search

In-repo clients are intentionally limited to the web UI for v1.

## Run locally

Backend only:

```bash
task dev
# or
go run .
```

Note: without `--web-dir`, backend serves API only. Root `/` will not serve web UI.

Frontend development with Vite hot reload:

```bash
task web-install
task dev      # backend on :8080
task web-dev  # frontend on :5173
```

Built frontend served by the Go binary from disk:

```bash
task web-build
task dev-web
```

## Run in Docker

```bash
task up
task down
```

The service expects `./data` to be persisted as a bind mount so both `control.db` and per-network log DBs survive restarts.

Docker image builds frontend and serves it from `/app/web/dist` automatically.

On pushes to `main`, GitHub Actions publishes image to `ghcr.io/lepinkainen/lurker` with `latest` and `sha-<commit>` tags.

## Config

Bootstrap network configuration comes from `config.yaml`.

After startup, network definitions are managed through the application itself and stored in `control.db`.

### Update checker

Server can poll GHCR for newer published image metadata and report status over API. It does not pull or restart containers. Update checks target Linux container image metadata (`linux/amd64`).

Environment variables:

- `UPDATE_CHECK_ENABLED` default `true`
- `UPDATE_CHECK_IMAGE` default `ghcr.io/lepinkainen/lurker`
- `UPDATE_CHECK_TAG` default `latest`
- `UPDATE_CHECK_INTERVAL` default `24h`, clamped to minimum `1h`
- `GHCR_USERNAME` optional
- `GHCR_TOKEN` optional

API endpoint:

- `GET /api/update-status`

Response includes current build info, remote image metadata, last check time, and `update_available`.

## Build and CI

Preferred local workflow uses `task`:

```bash
task web-install
task lint
task test
task web-build
task build
```

Default local binary output: `build/lurker`

Build artifacts are written to `build/`:

- `build/lurker`
- `build/lurker-linux-amd64`

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
