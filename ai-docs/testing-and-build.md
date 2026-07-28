# Testing and build

## Browser test fixture

Frontend browser tests use a seeded SQLite backend plus a small runtime-state overlay. See [test-fixtures.md](test-fixtures.md) for how `data-test/`, `cmd/seedtest`, and `LURKER_TEST_FIXTURE_RUNTIME` work.

## Fake IRC server (manual verification)

`cmd/fakeircd` (`task fake-ircd`) is a minimal IRC server for *manual* end-to-end verification, not for automated tests: it completes the girc handshake on :6667 and injects PRIVMSGs from a fake user when a control line (`#channel :message`) arrives on :6668. Point a network at `127.0.0.1:6667` (tls false) via config.yaml bootstrap to drive live-arrival behaviors (unread badges, new-messages marker) in real clients. See `.claude/skills/verifier-tui/SKILL.md` for the full recipe.

## Testing strategy

For IRC package tests, prefer unit tests that inject synthetic `girc.Event` values or fake connection hooks over socket-level fake IRC servers.

Rationale:

- Lurker should test its own translation layer and state management
- the `girc` library is treated as trusted for protocol parsing and wire-level behavior
- fast deterministic tests are preferred over in-process network servers where possible

This means tests should primarily cover:

- IRC event -> SQLite persistence
- IRC event -> hub publication
- manager lifecycle/state transitions
- retry/failover selection logic via injected connector seams

Only add true transport-level integration tests when validating behavior that is specifically about Lurker's own network integration rather than `girc` internals.

## Build and developer workflow

Preferred commands come from `Taskfile.yml`:

- `task dev`
- `task dev-web`
- `task web-install`
- `task web-dev`
- `task web-build`
- `task lint-apple` — check Swift formatting (macOS only)
- `task test-apple` — run native unit tests (macOS only)
- `task test-apple-ui` — run the fixture-driven native UI smoke test (macOS only)
- `task build-apple` — build the unsigned Apple silicon debug app (macOS only)
- `task package-apple` — sign, notarize, and staple a release DMG (macOS only)
- `task test`
- `task lint`
- `task build`
- `task generate` — regenerate sqlc Go code from `db/{control,log,preview}_queries/*.sql`
- `task up`
- `task down`

On macOS, `task build` includes Swift lint, native unit tests, and the native app build. CI runs those checks in a separate `apple` job on a `macos-26` runner. The UI smoke test is kept as an explicit local check because it launches an application and takes control of the desktop session.

## SQL codegen (sqlc)

The `db` package uses [sqlc](https://github.com/sqlc-dev/sqlc) to compile SQL into typed Go. See [storage.md](storage.md#sql-query-layer-sqlc) for the full pattern. Regenerate after editing any `.sql` file under `db/{control,log,preview}_queries/` or any migration file under `db/{control,log,preview}_migrations/`.

Install: `brew install sqlc`. Generated code (`db/internal/*`) is committed.
