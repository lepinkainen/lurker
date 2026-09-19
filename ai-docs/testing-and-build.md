# Testing and build

## Browser test fixture

Frontend browser tests use a seeded SQLite backend plus a small runtime-state overlay. See [test-fixtures.md](test-fixtures.md) for how `data-test/`, `cmd/seedtest`, and `LURKER_TEST_FIXTURE_RUNTIME` work.

The suite runs in Vitest browser mode against headless chromium via `@vitest/browser-playwright`, so it needs the Playwright browser binaries, not just the npm package. pnpm blocks Playwright's postinstall script — `web/pnpm-workspace.yaml` allowlists builds for `esbuild` only — so the binaries are fetched explicitly with `pnpm exec playwright install --with-deps chromium`. Do that once locally, and note that the CI `lint` job runs it on every job because runners start with an empty `~/.cache/ms-playwright`.

Two tiers, and only the first belongs in CI:

- `task test-web` — unit tier, `vitest.config.ts`, no backend. Some tests hit proxied endpoints and log `ECONNREFUSED` against the backend port; that is expected noise, not a failure.
- `task test-web-integration` — integration tier, `vitest.integration.config.ts`, starts a seeded backend through `tests/globalSetup.ts`. Local only.

## Live IRC verification

All socket-level IRC integration and live-client verification uses a real
Ergo server. `task ergo` starts a disposable container on `127.0.0.1:16667`
and removes it on Ctrl-C. In another terminal, `task irc-test-client` connects
as `bob`, joins `#verify`, and exposes HTTP control on `127.0.0.1:16668`.
Override the channel with `task irc-test-client -- -channel '#timeline-scroll'`.

Point a Lurker network at `127.0.0.1:16667` with TLS off and join the same
channel. Wait for both Lurker's join and the sender's readiness before sending:

```sh
curl -fsS http://127.0.0.1:16668/ready
curl -fsS http://127.0.0.1:16668/message --data-binary 'hello from bob'
```

`POST /message` accepts one nonempty line within girc's negotiated event limit,
minus the `PRIVMSG <channel> :` overhead and one byte to stay below girc's
splitting threshold. Oversized bodies receive HTTP 400 with the current byte
limit; accepted bodies remain one message with their content intact. HTTP 202 means
queued by the IRC client; verify delivery in Lurker's history/UI. The sender
stays connected, answers PINGs through girc, and exits if its IRC connection
closes. Presence, membership, sender identity, timestamps, and message IDs all
come from Ergo. Use an ordinary second IRC client for other commands or query
messages. No synthetic IRC server or arbitrary server-line injection is used.

`task test-apple-ui-live` owns an Ergo instance, sender, and temporary backend,
seeds 50 messages from `bob`, and runs the live scrolling regression by default.
Its optional `TEST_FILTER` selects another live UI test by method name. Cleanup
stops its processes and removes its temporary data. These commands require
Docker; the manual and automated stacks use the same ports and run separately.

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

## Real IRCv3 server integration tests (Ergo)

`task test-ergo` runs `irc/ergo_integration_test.go` and `cmd/irctestclient/ergo_test.go` (build tag `ergo`) against a real [Ergo](https://ergo.chat) server started in docker from `testdata/ergo/ircd.yaml` (plaintext :16667, throttling off, in-memory history with CHATHISTORY enabled). Use this layer for behavior that depends on real server-side protocol flows: CAP negotiation outcomes (`HasCapability`), CHATHISTORY request/replay, and future echo-message / SASL / multiline work. Division of labor:

- **unit tests** (synthetic `girc.Event`s): lurker's translation layer, persistence, hub publication
- **`task ergo` + `task irc-test-client`**: manual live-client verification using a real sender and server
- **`task test-ergo`**: protocol conformance against a reference IRCv3 implementation; requires docker, not part of `task test`

Assertion caveats: Ergo timestamps/msgids are nondeterministic (assert on content/order/counts), and without the `event-playback` cap Ergo replays join/quit history as PRIVMSGs from `HistServ` — filter by sender.

### Workflow

- One-shot: `task test-ergo` (starts container, waits for the port, runs tests, removes container even on failure).
- Iterating on a test: start the server once and keep it running, then run tests directly against it:

  ```sh
  task ergo  # keep running in a separate terminal
  task test-one -- -tags=ergo ./irc/ ./cmd/irctestclient/ -run TestErgo -count=1 -v
  # Ctrl-C the task ergo terminal when done
  ```

  `ERGO_ADDR` (default `127.0.0.1:16667`) points a direct `go test` run at any reachable Ergo instance. `task ergo` and `task test-ergo` run their own container and set it themselves, so it has no effect there.

### Writing new tests

- Put protocol tests in `irc/` (sender tests in `cmd/irctestclient/`) with the `//go:build ergo` tag; name them `TestErgo*` so the task target's `-run TestErgo` picks them up.
- Reuse `dialRaw` (`ergo_integration_test.go`) for scripted counterpart clients — it registers, answers PINGs, and offers `send`/`waitFor`.
- Use unique channel names per run (e.g. time-based suffix): the container keeps in-memory history for its whole lifetime, so a rerun against a kept-alive server sees earlier messages. Restarting the container resets all state (history is RAM-only, datastore is throwaway).
- Server behavior knobs live in `testdata/ergo/ircd.yaml` (e.g. `history.chathistory-maxmessages`, `limits.multiline`); it's a trimmed Ergo default.yaml, so new sections can be copied from upstream when a test needs them.

## Build and developer workflow

Preferred commands come from `Taskfile.yml`:

- `task dev`
- `task dev-web`
- `task web-install`
- `task web-dev`
- `task web-build`
- `task lint-apple` — check Swift formatting against the Airbnb style guide via SwiftFormat, config in `apple/airbnb.swiftformat` (macOS only)
- `task format-apple` — apply that formatting in place (macOS only)
- `task test-apple` — run native unit tests (macOS only)
- `task test-apple-ui` — run the fixture-driven native UI smoke test (macOS only)
- `task build-apple` — build the unsigned Apple silicon debug app (macOS only)
- `task package-apple` — sign, notarize, and staple a release DMG (macOS only)
- `task test`
- `task lint`
- `task lint-mermaid` — parse Mermaid diagrams in root-level documentation and `ai-docs/`; included in `task lint`
- `task build`
- `task generate` — regenerate sqlc Go code from `db/{control,log,preview}_queries/*.sql`
- `task up`
- `task down`

CI's `lint` job runs `task lint-web`, then `task test-web`, then `task lint-mermaid` and the Go linters; the `test` job runs `task test-ci`. Frontend tests live in `lint` because that is the only job that installs frontend dependencies.

On macOS, `task build` includes Swift lint, native unit tests, and the native app build. CI runs those checks in a separate `apple` job on a `macos-26` runner. The UI smoke test is kept as an explicit local check because it launches an application and takes control of the desktop session.

The Tauri shell gets a `desktop` job on `ubuntu-latest`: `cargo fmt --check`, `cargo clippy --all-targets -- -D warnings`, `cargo test`. It installs Tauri's Linux prerequisites (`libwebkit2gtk-4.1-dev`, `libxdo-dev`, `libssl-dev`, `libayatana-appindicator3-dev`, `librsvg2-dev`) because wry links webkit2gtk-4.1. `desktop/dist/index.html` is committed, so `frontendDist` is satisfied without building the frontend first. `build` lists `desktop` in `needs`, so a Rust failure blocks the merge.

Dependabot's cargo updates are excluded from auto-merge in `dependabot-auto-merge.yml`, on top of that job. Below 1.0 the minor digit carries the breaking change, but Dependabot reports the bump as `semver-minor` anyway, so a breaking cargo bump looks safe to a rule that only reads the update type — that is how `sha2` 0.10 → 0.11 auto-merged and broke the desktop build. CI catches a compile break; it cannot catch a behavioral one, so Rust bumps get a human either way.

Mermaid lint validates Markdown `mermaid` fences, HTML `pre.mermaid` elements and `script[type="text/plain"]` elements whose IDs start with `source-`, and `.mmd` files. It uses `mermaid.parse()` in Node with jsdom for label sanitization; it does not launch a browser or render diagrams. Invalid syntax fails lint with the file and diagram line number. Dependencies are installed by `task web-install`. Keep the pinned Mermaid dependency in `web/package.json` and the HTML CDN import at the same version; lint checks for mismatches. There are two CDN pins and only one is covered: `ai-docs/architecture.html` is checked because lint reads `.md`, `.html` and `.mmd` files, while `ai-docs/diagrams/viewer.js` is a `.js` file and is not, so bump it by hand. To validate specific files, use `task lint-mermaid -- path/to/document.md`.

Lint only parses. It never renders, so it cannot catch a Mermaid release that changes the rendered SVG structure — `viewer.js` reaches into that structure with `.node` and `.edgePath, .flowchart-link` selectors for its highlight feature. On a major Mermaid bump, render a diagram in a real browser and confirm those selectors still match.

## SQL codegen (sqlc)

The `db` package uses [sqlc](https://github.com/sqlc-dev/sqlc) to compile SQL into typed Go. See [storage.md](storage.md#sql-query-layer-sqlc) for the full pattern. Regenerate after editing any `.sql` file under `db/{control,log,preview}_queries/` or any migration file under `db/{control,log,preview}_migrations/`.

Install: `brew install sqlc`. Generated code (`db/internal/*`) is committed.
