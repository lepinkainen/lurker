# Testing and build

## Browser test fixture

Frontend browser tests use a seeded SQLite backend plus a small runtime-state overlay. See [test-fixtures.md](test-fixtures.md) for how `data-test/`, `cmd/seedtest`, and `LURKER_TEST_FIXTURE_RUNTIME` work.

The suite runs in Vitest browser mode against headless chromium via `@vitest/browser-playwright`, so it needs the Playwright browser binaries, not just the npm package. pnpm blocks Playwright's postinstall script — `web/pnpm-workspace.yaml` allowlists builds for `esbuild` only — so the binaries are fetched explicitly with `pnpm exec playwright install --with-deps chromium`. Do that once locally, and note that the CI `lint` job runs it on every job because runners start with an empty `~/.cache/ms-playwright`.

Two tiers, and only the first belongs in CI:

- `task test-web` — unit tier, `vitest.config.ts`, no backend. Some tests hit proxied endpoints and log `ECONNREFUSED` against the backend port; that is expected noise, not a failure.
- `task test-web-integration` — integration tier, `vitest.integration.config.ts`, starts a seeded backend through `tests/globalSetup.ts`. Local only.

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

## Real IRCv3 server integration tests (Ergo)

`task test-ergo` runs `irc/ergo_integration_test.go` (build tag `ergo`) against a real [Ergo](https://ergo.chat) server started in docker from `testdata/ergo/ircd.yaml` (plaintext :16667, throttling off, in-memory history with CHATHISTORY enabled). Use this layer — not `cmd/fakeircd`, and not unit tests — for behavior that depends on real server-side protocol flows: CAP negotiation outcomes (`HasCapability`), CHATHISTORY request/replay, and future echo-message / SASL / multiline work. Division of labor:

- **unit tests** (synthetic `girc.Event`s): lurker's translation layer, persistence, hub publication
- **`cmd/fakeircd`**: manual verification and adversarial/edge-case line injection (a real server never sends malformed input)
- **`task test-ergo`**: protocol conformance against a reference IRCv3 implementation; requires docker, not part of `task test`

Assertion caveats: Ergo timestamps/msgids are nondeterministic (assert on content/order/counts), and without the `event-playback` cap Ergo replays join/quit history as PRIVMSGs from `HistServ` — filter by sender.

### Workflow

- One-shot: `task test-ergo` (starts container, waits for the port, runs tests, removes container even on failure).
- Iterating on a test: start the server once and keep it running, then run tests directly against it:

  ```sh
  docker run -d --name lurker-ergo-test -p 16667:6667 \
    -v $PWD/testdata/ergo/ircd.yaml:/ircd/ircd.yaml:ro ghcr.io/ergochat/ergo:stable
  ERGO_ADDR=127.0.0.1:16667 go test -tags=ergo ./irc/ -run TestErgo -count=1 -v
  docker rm -f lurker-ergo-test   # when done
  ```

  `ERGO_ADDR` (default `127.0.0.1:16667`) points tests at any reachable Ergo instance.

### Writing new tests

- Put them in `irc/` with the `//go:build ergo` tag; name them `TestErgo*` so the task target's `-run TestErgo` picks them up.
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
