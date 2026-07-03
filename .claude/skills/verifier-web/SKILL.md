---
name: verifier-web
description: Verify web UI changes end-to-end by driving the built frontend against a seeded backend with Playwright. Use when verifying changes under web/src/ (rendering, keyboard shortcuts, sidebar, markers, WebSocket-driven behavior) at the real browser surface. Not for backend-only changes (use curl against the API instead) and not for running unit tests.
---

# Verifier: Web UI

Runtime-verify frontend changes by launching the real app with seeded data and driving it in a browser. Unit tests (`task test-web`) are CI's job — this skill is for observing the change at the surface a user sees.

## Build & launch

```bash
task seed-test                # reset + seed ./data-test (fake networks, buffers, messages)
task dev-test                 # background this: builds web/dist, serves on :8080 with DATA_DIR=./data-test
```

- Run `task dev-test` as a background task; it depends on `web-build`, so first output is the Vite build (~10s), then Go logs.
- Readiness probe: `curl -s localhost:8080/whoami` returns `{"name":"lurker",...}`.
- Port is `:8080` (default `ADDR`). Check `lsof -i :8080 -sTCP:LISTEN` first — a leftover dev server will happily serve stale code.
- **Stale-build trap:** `dev-test` serves `./web/dist`. If you bypass the task and reuse a running server, your frontend change is NOT in it. Always relaunch via `task dev-test` after editing `web/src/`.

## Reach the surface

Navigate Playwright to `http://localhost:8080/`. The app restores the last active buffer from the URL hash (`#net/<network>/<buffer>`).

Sidebar quirks that cost time if you don't know them:

- Seeded channels sit inside **collapsed "Archive" groups** per network. Click the `▸ Archive N` button to reveal `#go-nuts`, `#lurker`, `#debian` etc. Query buffers (`alice`) are visible directly.
- Channel rows are `<button>` elements; click via `page.locator('button', { hasText: '#go-nuts' }).first()`.
- Unread badge is a small pill inside the channel button; the "New messages" marker line is a leaf element in `main` with exact text `New messages`:

```js
const markerCount = () => page.evaluate(() =>
  [...document.querySelectorAll('main *')]
    .filter(el => el.children.length === 0 && el.textContent.trim() === 'New messages').length);
```

## Fakes & fixtures

Seeded networks never connect to real IRC servers, so **no live messages ever arrive on their own**. To simulate a server-pushed event (message arrival, buffer_update, etc.), patch `WebSocket` before the app loads and dispatch synthetic `MessageEvent`s on the captured instance:

```js
await page.addInitScript(() => {
  const Orig = window.WebSocket;
  window.WebSocket = class extends Orig {
    constructor(...a) { super(...a); window.__ws = this; }
  };
});
await page.goto('http://localhost:8080/');
```

Then inject events with the wire shape cloned from real data — fetch `/api/state` in the page, grab a message from `initial_messages[bufferId]`, and clone it:

```js
// inside page.evaluate — tpl is a real message object from /api/state
const m = { ...tpl, type: 'message', id: tpl.id + '0', content: 'live test message', ts: new Date().toISOString() };
window.__ws.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(m) }));
```

- Message IDs sort by `localeCompare`; appending a suffix (`'0'`, `'1'`, …) to an existing latest ID produces strictly-later IDs.
- Required fields for an unread-counting message: `type: 'message'`, `id`, `buffer_id`, `network_id`, `ts`, `sender`, `kind: 'privmsg'`, `display_kind: 'message'`, `counts_as_unread: true`.
- **Echo caveat:** injected IDs don't exist server-side. A `mark_read` sent for an injected ID leaves the server's unread count unchanged, and the `buffer_update` echo will restore the server-side badge count. That's a fixture artifact, not a bug — note it, don't chase it.
- Event routing lives in `web/src/ws-router.ts` — check it for other injectable event types (`buffer_update`, `preview`, `member_list`, …).

Focus limitation: headless pages report `document.hasFocus() === true` and it can't be faked, so unfocused-tab behaviors (`state.uiFocused === false` paths) are not drivable here. Say so in the report and lean on unit tests for those paths.

## Evidence

- Screenshot the one frame that shows the feature (e.g. marker line in place) and Read it back to confirm it shows what you claim.
- Prefer `page.evaluate` counts/DOM text captured in tool results over memory.

## Cleanup

```bash
# stop the background dev-test task (TaskStop <id>)
rm -rf .playwright-mcp            # Playwright MCP drops snapshots in repo root
# move/remove any screenshots saved to repo root — they land in CWD by default
```

`data-test/` is gitignored and disposable; re-seed freely.
