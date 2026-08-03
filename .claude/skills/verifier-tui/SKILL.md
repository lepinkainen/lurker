---
name: verifier-tui
description: Verify TUI client changes end-to-end by driving build/lurker-tui in an isolated tmux session against a seeded backend. Use when verifying changes under cmd/tui/ (rendering, keybindings, sidebar, buffer switching, slash commands, mouse support, markers) at the real terminal surface. Not for backend-only changes (use curl against the API instead) and not for running unit tests (task test-tui is CI's job).
---

# Verifier: TUI client

Runtime-verify `cmd/tui/` changes by launching the real TUI in tmux against a seeded backend and capturing panes as evidence. Unit tests (`task test-tui`) are CI's job — this skill is for observing the change at the surface a user sees.

## Build & launch

```bash
task seed-test        # reset + seed ./data-test
task dev-test-api     # background this: backend on :8080, API only (no Vite build)
task build-tui        # build/lurker-tui
```

- Readiness probe: `curl -s localhost:8080/whoami`. Check `lsof -i :8080 -sTCP:LISTEN` first — a leftover server serves stale data.
- **Stale-build trap:** the TUI is a compiled binary. Re-run `task build-tui` after every `cmd/tui/` edit or you are verifying the old build.
- Use `dev-test-api`, not `dev-test`: the latter depends on `web-build` → `gen-palette`, which runs Vite and rewrites tracked `web/src/nick-palette.ts`, dirtying the worktree for a TUI-only run.
- Run the TUI on an **isolated tmux server** (`-L lurker-verify`). Works identically whether or not you are yourself inside tmux; never use bare `tmux`, that lands on the user's server:

```bash
tmux -L lurker-verify new-session -d -s tui -x 200 -y 50
tmux -L lurker-verify send-keys -t tui \
  "./build/lurker-tui -url http://localhost:8080 -state-dir /tmp/lurker-verify" Enter
sleep 3
tmux -L lurker-verify capture-pane -t tui -p     # evidence
```

- **`-state-dir` is mandatory.** Without it the TUI reads and rewrites `<user config dir>/lurker/tui-state.json` (`cmd/tui/state.go`) — clobbering the user's real last-viewed buffer, and inheriting it on the next run so startup-buffer assertions become non-deterministic.
- `-url` beats config discovery, which otherwise probes `./tui-config.yaml` then `~/.config/lurker/tui.yaml` (`cmd/tui/config.go`); default backend is already `http://localhost:8080`. There is no debug flag, no log file, and no env-var overrides.
- 200x50 gives sidebar + viewport room. The sidebar is a fixed 26 cols and there is **no "terminal too small" guard** (`cmd/tui/model.go` `resizeComponents`): below ~28 cols (~51 with the members pane open) the right pane collapses to 1 column and rendering overflows instead of degrading.

## Drive it

Keys, from `handleKey` in `cmd/tui/model.go`. Only two focus states (input, sidebar) plus the switcher overlay — **there is no help overlay and no `?` binding**.

| Key | Effect |
|---|---|
| `C-k` | Buffer switcher. Send `C-k`, type a fragment (`go-nuts`), `Enter`. Most reliable buffer switch; while open it swallows all other keys. |
| `Esc` | Two things at once: acks the active buffer (`mark_read`) **and** toggles input↔sidebar focus. |
| `Up`/`Down` (sidebar focus) | Moves **and auto-activates** the landing row — no `Enter` needed. Skips header and Archives-fold rows. `Enter` additionally returns focus to the input. |
| `Up`/`Down` (input focus) | Input history recall when the input is empty, otherwise viewport scroll by 3 lines. |
| `PgUp`/`PgDn` | Viewport half-page; `PgUp` at top requests older history. |
| `C-u` | Toggle members pane. |
| `Tab` | Nick autocomplete (input focus only); any other key ends the cycle. |
| `C-d` twice | Quit, double-tap within 1s. **`C-c` does not quit** — alt-screen, no handler. |

Slash commands live in `cmd/tui/slash.go` (~35, each mapping to one WS command). Useful ones: `/join`, `/query`, `/me`, `/topic`, `/archive`, `/unarchive`, `/delete`, `/raw`. `/archives` is client-only (toggles the Archives fold, never hits the server) and is the only keyboard route into the fold. A bad command sets status `Unknown command: /foo` — grep for that as the failure signal.

Mouse is on (`tea.WithMouseCellMotion`), so terminal-native scroll and link-clicking are replaced by hit-testing (`handleMouse`, `cmd/tui/model.go`). Geometry matters when sending clicks:

- Sidebar rows: `x < 26`, row index `y - 2`. Archive-toggle rows **are** clickable (only headers are rejected).
- Unread-bar ack: exactly `y == 2`, `x > 26`.
- URLs in the viewport: `relX = x - 27`, `relY = y - 2` (minus 1 more when the unread bar is visible). A hit sets status `Opening <url>` and shells out to `open` — expect a real browser launch.

## Read the screen

`capture-pane -t tui -p` strips SGR; add `-e` when the signal is color-only (mention gold `#F0B33C`, connection dot). Literals worth grepping:

- Connection: `● Connected`, `● Connecting…`, `● Reconnecting…`, `● Offline`.
- Sidebar: `⚑ Pinned` / `⚑ <name>`, `▸ Archives (N)` closed / `▾ Archives (N)` open (fold resets to closed each launch), `(status)`, `(no networks)`.
- Marker: in-scroll divider label ` New messages `; pinned bar label ` 1 new message ` / ` N new messages ` / ` new since HH:MM ` (the last one when unread ≥ 1000 or the marker message isn't in loaded history).
- Panes/overlays: `Members (N)`, `(no members)`, `Jump to buffer: <query>_`, `(no matches)`.
- Startup/status: `Initialising…`, `Loading from <url>…`, `Press Ctrl+D again to quit`, `WS error: … — reconnecting in 5s…`, `Error: fetch state: …`.
- Messages: `[15:04] <nick> text`, notices `[15:04] -nick- text`, actions `* nick text`, events `→ joined`, `← left`, `⇠ quit`, `⛔ kicked`, `⚙ set mode`, `📌 set topic`, collapsed runs `+ N presence events: …`.

**Badge trap:** unread badges render as `" (N)"` / `" (99+)"`, and pinned rows appear twice (Pinned section **and** their network section) so every badge shows up twice. Rows styled as **archived/dim or as the Archives fold render the bare label — never a badge** (`renderSidebar`, `cmd/tui/model.go`), and mentions get no badge at all, only gold color. Timestamps are local (`TZ`-dependent), so pin `TZ` if you assert on them.

## Fakes & fixtures

`cmd/seedtest` (`task seed-test`) creates: network `libera` with `#lurker` (7 messages), `#go-nuts` (3), `#retired` (archived), query `alice` (3), query `spammer` (archived); network `oftc` with `#debian` (2); a 4-line status buffer per network. Pin order is deliberately non-alphabetical: `#go-nuts`, `#debian`, `#lurker` — so a fresh run should land on `libera/#go-nuts`.

- **No unread/marker state is seeded** and members are populated in the fixture struct but never inserted. So: members panes are empty, and there is no preset "N unread mid-backlog" fixture — everything reads unread on first launch. To get a real marker mid-backlog, ack once (Esc), then inject new messages.
- Seeded networks point at `127.0.0.1:1` and never connect. For **live arrivals** use `cmd/fakeircd` (`task fake-ircd`: IRC on :6667, control on :6668) and attach a network via REST:

```bash
NET=$(curl -s -X POST localhost:8080/api/networks -H 'Content-Type: application/json' \
  -d '{"name":"testnet","host":"127.0.0.1","port":6667,"tls":false,"nick":"lurkertest","connect_commands":["JOIN #verify"]}' | jq -r .id)
curl -s -X POST "localhost:8080/api/networks/$NET/connect"
echo "#verify :hello from bob" | nc -w1 127.0.0.1 6668     # inject a live PRIVMSG
```

- The control port speaks one thing only: `<target> :<text>` → a PRIVMSG **always from `bob!bob@fake.host`**, broadcast to every connected client. Lines without ` :` are silently dropped. No join/part/quit injection exists; `bob` appears in NAMES synthetically but never sends real presence events. Target may be a nick, which gives you a query buffer.
- API-created networks are ephemeral (reverted at boot by the config invariant). To make one survive a restart, **append** it to `data-test/config.yaml` — a separate config file would disable the seeded networks.
- Server-side assertions: `curl -s localhost:8080/api/state` and inspect `buffers[].unread` / `marker_id` / `last_seen_id` to check what the TUI actually told the backend.

## Testability seams

`model.sendWS func(cmd wsCmd) error` intercepts **only `sendCmdAsync`** — in practice just `mark_read` (`cmd/tui/model.go`). Slash commands and `requestHistory` write to the raw connection and bypass it, so a recorder-based test sees nothing from them; see `markerModel` in `cmd/tui/model_test.go`. There is no inbound-injection hook and no clock seam — inbound events are unit-tested by calling `handleWSEvent(wsEvent{...})` directly.

## Marker behavior (since 2026-07-29)

Server-derived, explicit-ack-only (`ai-docs/behaviors/new-messages-marker.md`): marker/badges come from `/api/state` `marker_id`/`unread` and survive restarts. Entering a buffer, scrolling, and reconnecting send **nothing**. The only `mark_read` triggers are `Esc` and a click on the unread bar (`y == 2`, `x > 26`). An ack clears badge + divider + bar together, on all clients. Server-side check: `buffers[].last_seen_id` advances **only** after an ack — if it moves on mere buffer entry, that's a regression.

## Cleanup

```bash
tmux -L lurker-verify send-keys -t tui C-d; sleep 0.3
tmux -L lurker-verify send-keys -t tui C-d     # double-tap within 1s; C-c does NOT quit
tmux -L lurker-verify kill-server
rm -rf /tmp/lurker-verify                      # scratch state dir
# stop the background dev-test-api task (TaskStop <id>)
```

`data-test/` is gitignored and disposable; re-seed freely.
