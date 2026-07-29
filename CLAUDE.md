# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Lurker: single-user IRCCloud-style bouncer + web client backend. Go service + Vite/TypeScript web UI. SQLite storage. Private network only (Tailscale), no app-layer auth, no multi-user. Greenfield — no backwards-compat constraints.

See `PROJECT.md`, `AGENTS.md`, `ai-docs/ARCHITECTURE.md` (and subdocs: `storage.md`, `rest-api.md`, `websocket-protocol.md`, `irc-runtime.md`, `frontend.md`, `testing-and-build.md`, `operations.md`). Runtime behavior specs hard to unit-test live in `ai-docs/behaviors/`.

## Commands

Always use `task` (Taskfile.yml), not direct `go`/`pnpm`.

- `task dev` — run backend (`go run .`), API only, no web UI at `/`
- `task dev-web` — build frontend, run backend serving `./web/dist`
- `task web-install` / `task web-dev` — pnpm install / Vite dev server (:5173)
- `task web-build` — build frontend to `web/dist` (depends on `gen-palette`)
- `task test` — Go tests, excludes `llm-shared/`
- `task test-web` — frontend tests (Vitest)
- `task lint` — goimports + golangci-lint + frontend type-check + eslint fix (depends on `lint-web`)
- `task lint-web` — `tsc --noEmit` + `pnpm lint:fix`
- `task build` — lint + test + web-build + Go binary → `build/lurker`
- `task build-linux` — `build/lurker-linux-amd64`
- `task seed-test` / `task dev-test` — seed `./data-test`, run backend against it
- `task up` / `task down` — docker compose
- `task push` — push branch + watch CI via `scripts/push-and-watch.sh`
- `task tidy` — `go mod tidy`
- `task generate` — `sqlc generate` (regenerate `db/internal/{controldb,logdb,previewdb}/*` after editing `db/{control,log,preview}_queries/*.sql` or any migration)
- `task clean` — remove `build/`, `web/dist/`, `data/`
- `task docker` — build Docker image locally
- `task test-ci` — tests with `-tags=ci -cover -v`, outputs `coverage.out`

Single Go test: `go test ./irc/ -run TestName`. Single web test: `cd web && pnpm test -- <pattern>`.

**Never** run `go test -tags=integration ./irc/...` — hangs.

## Architecture

Process boot (`main.go`): load config → open `MultiStore` (control DB + per-network log DBs) → create event hub → create IRC manager → start bootstrap networks from `config.yaml` → serve HTTP. Shutdown stops HTTP, waits IRC runtimes.

Packages:
- `api/` — REST endpoints `/api/*`, `/whoami`, WebSocket `/api/stream`
- `irc/` — `irc.Manager`, per-network connection lifecycle, event persistence, URL preview pipeline
- `db/` — control DB, per-network log DBs, migrations, queries
- `hub/` — in-process pub/sub fanning IRC events to WebSocket clients
- `preview/` — URL preview pipeline (fetch, SSRF guard, YouTube/Fediverse extractors)
- `updates/` — background GHCR image metadata polling
- `web/` — Vite + TS frontend
- `cmd/seedtest/` — fake-data seeder

Config inputs: `DATA_DIR` (default `./data`), `ADDR` (`:8080`), `CONFIG_PATH` (`./config.yaml`), `--web-dir` flag, `UPDATE_CHECK_*` env.

### Invariants (do not break)

- Config errors fail fast: missing, misplaced, or unparseable `config.yaml` exits before anything starts — never boot a half-configured backend. An empty-but-valid config means zero networks run (all disabled at boot).
- `config.yaml` is the source of truth at boot: every startup returns networks to the YAML-defined state. UpsertNetwork overwrites connection fields (nick, host, servers, SASL, channels) and resets `disabled` from YAML; networks absent from YAML are marked disabled in DB, not deleted. DB preserves `sort_order` and `created_at`. Networks can be added/edited via API but those changes are ephemeral (revert on restart); the API exposes `in_config` so UIs can warn about this. UI-only settings without a config key (e.g. per-buffer view options) are DB-owned and untouched by boot reconciliation.
- Per-network log DB per network under `data/`. Global buffer IDs from control DB; messages in per-network DBs.
- Network names stable after creation. No casual rename flows.
- Deleting a network does not delete its log DB file.
- Sidebar order persisted in `networks.sort_order`.
- `/whoami` identifies running instance.
- IRC servers do **not** echo messages back — outbound messages need local echo or explicit persistence. Never assume echo-message capability.
- Preserve entire `data/` directory across deploys.

### Client/backend version skew

The backend is deployed as a Docker image from GHCR, which may lag behind locally built clients (web/tui/apple). Whenever a change spans both a client and the backend, say explicitly in your summary which of these applies:

- **Requires backend update** — the client feature is broken or errors against an older backend (new endpoint, new WS event, changed payload).
- **Nice to have** — clients still work against an older backend, but something is slightly degraded (missing field falls back, optional enrichment absent).

This prevents wasted debugging of a "missing" feature that is actually a stale backend image. Clients show the backend's build time (from `/whoami` `build_time`) in the web settings dialog and the macOS Settings → About section — point the user there when in doubt.

### Constraints

- `llm-shared/` is a git submodule. Do not edit. Exclude when scanning. Report issues, don't fix.
- No auth, no multi-user, no public-internet assumptions unless asked.
- Web UI scope minimal, v1-aligned.
- `README.md` stays concise/human; do not document features/config/APIs there. Use `ai-docs/`.

## Stack

Go 1.26 backend. Vite + TypeScript frontend in `web/`. SQLite storage. WebSocket `/api/stream`. Docker image publishes to `ghcr.io/lepinkainen/lurker` on push to `main`, serves frontend from `/app/web/dist`.

## Ignored / generated

`build/`, `data/`, `data-test/`, `web/dist/`, `web/node_modules/`, `.env`, `config.yaml`.
