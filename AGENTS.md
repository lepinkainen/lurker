# AGENTS.md

Guidance for coding agents working in this repository.

## Project overview

Lurker is a personal IRCCloud-style bouncer + web client backend for a single user on a private network.

Primary pieces:
- Go backend service (`lurker`)
- Web frontend in `web/` built with Vite + TypeScript
- SQLite storage under `data/`
- `llm-shared/` git submodule with shared docs and guidelines

Read these first for project context:
- `PROJECT.md` — purpose and future direction
- `README.md` — human-focused local dev, Docker, and deployment notes
- `ai-docs/ARCHITECTURE.md` — technical architecture, storage model, and API notes

## Important repository constraints

### Do not modify `llm-shared/`

`llm-shared/` is a git submodule and must not be edited unless the user explicitly asks for it.

Rules:
- do not edit files in `llm-shared/`
- do not add dependencies there
- when validating or scanning the repo, exclude `llm-shared/` where practical
- if you find an issue there, report it instead of fixing it

## Architecture and product assumptions

This project is intentionally:
- single-user
- private-network only
- not internet-facing
- unauthenticated at the app layer

Assume access happens through loopback, Tailscale, or another trusted private network path.
Do not add public-internet deployment assumptions or app-level auth unless the user asks.

## Stack

- Backend: Go `1.26`
- Frontend: Vite + TypeScript in `web/`
- Storage: SQLite (`control.db` plus one DB per network)
- Streaming: WebSocket API at `/api/stream`

## Key project conventions

### Network/storage model

- `config.yaml` is bootstrap-only seed input, not runtime source of truth after startup
- runtime network definitions are managed through the API and stored in `control.db`
- per-network history lives in one SQLite DB per network under `data/`
- network names are expected to be stable after creation
- rename behavior is conservative/manual; avoid introducing casual rename flows unless requested

### Deployment model

- preserve the entire `data/` directory
- built frontend is served by the Go binary when using `--web-dir ./web/dist`
- Docker/local deployment should preserve `/data`

### API expectations

Existing endpoints, architecture notes, and behavior are documented in `ai-docs/ARCHITECTURE.md`.
Preserve the current shape unless the task explicitly changes it.

The service exposes `/whoami`; prefer checking that endpoint when identifying a running instance.

## Development workflow

Preferred commands use `task` via `Taskfile.yml`.
Always use `task` targets for linters, tests, and builds rather than invoking the underlying tools directly, unless the user explicitly asks otherwise.
For example, run `task test` for tests and `task build` for builds.

Common commands:

```bash
task dev
task dev-web
task web-install
task web-dev
task web-build
task test
task lint
task build
task up
task down
```

## File/directory map

- `main.go` — backend entrypoint
- `config.go` / `config.yaml` — bootstrap config handling
- `version.go` — version/build metadata
- `web/` — frontend source and package metadata
- `data/` — runtime SQLite data (ignored in git)
- `build/` — build outputs
- `compose.yaml` / `Dockerfile` — container workflow

## Editing guidance for agents

- Make focused changes consistent with the current architecture.
- Avoid broad refactors unless requested.
- Do not introduce authentication, multi-user concepts, or public SaaS assumptions unless asked.
- Keep the web UI scope minimal and aligned with the current v1 direction.
- Respect existing REST/WebSocket API patterns.
- Prefer updating docs when behavior, commands, or architecture changes.
- Do not use `README.md` to document individual features, configuration options, or APIs; keep it concise and human-readable.

## Validation guidance

When code changes warrant validation, prefer the smallest relevant checks:

- Go tests: `task test`
- Go lint/type/style: `task lint`
- Frontend type-check: `task lint-web`
- Frontend build: `task web-build`

Be mindful that `task lint` runs formatting and linting and depends on frontend checks.

Always run `task build` before claiming a task as "done"

## Ignore and generated content

Do not commit or rely on generated/local-only paths:
- `build/`
- `data/`
- `web/dist/`
- `web/node_modules/`
- local env/config secrets such as `.env`, `config.yaml`

## If you need more context

Consult, in order:
1. `PROJECT.md`
2. `README.md`
3. `ai-docs/ARCHITECTURE.md`
4. `Taskfile.yml`

If a task touches shared guidance only conceptually, you may reference `llm-shared/` docs, but do not modify the submodule.
