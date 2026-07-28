---
name: verifier-tui
description: Verify TUI client changes end-to-end by driving build/lurker-tui in an isolated tmux session against a seeded backend. Use when verifying changes under cmd/tui/ (rendering, keybindings, sidebar, buffer switching, mouse support) at the real terminal surface. Not for backend-only changes and not for running unit tests (task test-tui is CI's job).
---

# Verifier: TUI client

Runtime-verify `cmd/tui/` changes by launching the real TUI in tmux against a seeded backend and capturing panes as evidence.

## Build & launch

```bash
task seed-test                 # reset + seed ./data-test
task dev-test                  # background this: serves backend on :8080 (also builds web/dist — slow first time)
task build-tui                 # build/lurker-tui
```

- Readiness probe for the backend: `curl -s localhost:8080/whoami`.
- TUI config: defaults to `backend_url: http://localhost:8080` — no config file needed against dev-test. Explicit config falls back `./tui-config.yaml` → `~/.config/lurker/tui.yaml` (see `cmd/tui/config.go`).
- Run the TUI in an **isolated tmux server** (never the user's default server):

```bash
tmux -L lurker-verify new-session -d -s tui -x 200 -y 50
tmux -L lurker-verify send-keys -t tui "./build/lurker-tui" Enter
sleep 3
tmux -L lurker-verify capture-pane -t tui -p     # evidence
```

- 200x50 gives the sidebar + viewport room; smaller panes truncate the sidebar.

## Drive it

Keys (from `cmd/tui/model.go` handleKey):

- `C-k` — channel switcher: send `C-k`, then type a fragment (e.g. `go-nuts`), then `Enter`. Most reliable way to switch buffers.
- `Escape` — acks the active buffer's "New messages" marker (sends `mark_read`) AND toggles focus between sidebar and input.
- `Up`/`Down` — navigate sidebar when it has focus; `Enter` selects.
- `PgUp`/`PgDn` — viewport scroll; `PgUp` at top requests older history.
- `C-d` twice — quit (double-tap confirm).

Sidebar renders unread counts as ` (N)` suffixes, e.g. `#go-nuts (3)` — capture-pane and grep for them.

## Fakes & fixtures

- Seeded networks never connect to real IRC. For **live message arrivals** use `cmd/fakeircd` (`task fake-ircd`: IRC on :6667, control on :6668). Point a network at it via the REST API — no config file needed:

```bash
NET=$(curl -s -X POST localhost:8080/api/networks -H 'Content-Type: application/json' \
  -d '{"name":"testnet","host":"127.0.0.1","port":6667,"tls":false,"nick":"lurkertest","connect_commands":["JOIN #verify"]}' | jq -r .id)
curl -s -X POST "localhost:8080/api/networks/$NET/connect"
echo "#verify :hello from bob" | nc -w1 127.0.0.1 6668     # inject a live PRIVMSG
```

- Alternative: extend `data-test/config.yaml` (written by `cmd/seedtest`, used by `task dev-test`) with a `networks:` entry at `127.0.0.1:6667` — YAML is authoritative, so the entry must be *added* to that file, not a separate config (a config without libera/oftc would disable the seeded networks at boot).
- Server-side state assertions: `curl -s localhost:8080/api/state` and inspect `buffers[].unread` / `last_seen_id` to check what the TUI actually told the backend.

## Testability hook

`model.sendWS func(cmd wsCmd) error` — when non-nil, all outbound WS commands route through it instead of the real connection (`sendCmdAsync` in `cmd/tui/model.go`). Unit tests install a recorder to assert on `mark_read` etc.; see `markerModel` in `cmd/tui/model_test.go`. There is no inbound-injection hook at the runtime surface — inbound events are unit-tested via `handleWSEvent(wsEvent{...})` directly.

## Marker behavior (since 2026-07-29)

Server-derived, explicit-ack-only (`ai-docs/behaviors/new-messages-marker.md`): marker/badges come from `/api/state` `marker_id`/`unread` and survive restarts; entering a buffer, scrolling, and reconnecting send NOTHING. The only `mark_read` triggers are Esc and mouse-click on the unread bar (the `── N new messages ──` line pinned above the viewport; falls back to `new since HH:MM` when the marker message isn't loaded). Ack clears badge + divider + bar together, on all clients. Grep captured panes for `New messages` (divider) and `new message` (bar). Server-side check: `/api/state` `buffers[].last_seen_id` advances ONLY after an ack — if it moves on mere buffer entry, that's a regression.

## Cleanup

```bash
tmux -L lurker-verify send-keys -t tui C-d; sleep 0.3
tmux -L lurker-verify send-keys -t tui C-d        # double-tap quit
tmux -L lurker-verify kill-server
# stop the background dev-test task (TaskStop <id>)
```
