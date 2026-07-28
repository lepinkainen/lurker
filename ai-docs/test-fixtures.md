# Test fixtures

## Browser/Vitest backend fixture

`task test-web` / `cd web && pnpm test` run Vitest in headless Chromium against a real Lurker HTTP backend.

The fixture setup lives in `web/tests/globalSetup.ts`:

1. `go run ./cmd/seedtest --data-dir ./data-test --reset` recreates `data-test/`.
2. `go build -o build/lurker-test .` builds a backend binary.
3. The backend starts on `LURKER_TEST_PORT` (default `8099`) with:
   - `DATA_DIR=./data-test`
   - `CONFIG_PATH=./data-test/config.yaml` (written by `cmd/seedtest`; a
     missing config is a fatal boot error, and an empty one would disable
     every seeded network)
   - `LURKER_TEST_FIXTURE_RUNTIME=1`
4. Vite proxies `/api`, `/healthz`, and `/whoami` to that backend.

If a healthy backend is already running on the test port, global setup reuses it. In that case, make sure it was started with equivalent fixture data and `LURKER_TEST_FIXTURE_RUNTIME=1`; otherwise browser tests may see disconnected networks or archived channels.

## Seeded SQLite data

`cmd/seedtest` writes static data into SQLite without contacting real IRC servers:

- networks: `libera`, `oftc`
- `<data-dir>/config.yaml` declaring those networks (config is the boot
  source of truth; servers point at `127.0.0.1:1` so nothing real is dialed)
- status buffers with connect/welcome-style history
- channel and query buffers
- channel topics
- recent channel/query messages

The seeded data is intended to exercise initial frontend rendering and API calls deterministically. It is not an IRC protocol simulator.

## Fixture runtime overlay

Some frontend state is runtime-only in production and does not live in SQLite. `/api/state` asks `irc.Manager` for these values:

- network connection status
- whether a channel is currently joined
- current channel member lists

During browser tests, the backend does not open live IRC sockets: `LURKER_TEST_FIXTURE_RUNTIME=1` calls `irc.Manager.LoadFixtureRuntimeState()` at startup *and* skips starting bootstrap networks (a live runtime would immediately overwrite the fixture connection states).

That runtime overlay derives state from the seeded databases:

- seeded, non-disabled networks are marked `connected`
- seeded channel buffers are marked `joined`
- channel members are derived from recent message senders plus the configured network nick, with the configured nick marked as `self` and voiced (`+`)

Outbound IRC commands still require a real connected IRC client and may return `irc: network not connected`; the overlay is only for read-side fixture state.

## When updating frontend tests

Update `cmd/seedtest` or the fixture runtime overlay when tests need initial state that should come from `/api/state` rather than local unit-test setup.

Prefer keeping this fixture small and deterministic. Use Go IRC unit tests with synthetic `girc.Event` values for protocol/event handling behavior instead of expanding the browser fixture into a fake IRC server.
