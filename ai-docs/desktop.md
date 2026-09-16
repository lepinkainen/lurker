# Desktop shell (Tauri PoC)

`desktop/` is a minimal Tauri 2 desktop shell for Lurker, currently a proof of concept. It is a native window (Rust + the OS's system webview — WKWebView on macOS, WebKitGTK on Linux) that points at a running Lurker backend, rather than a full Electron-style bundled app.

## Design: remote URL, no bundled frontend

The Lurker web frontend (`web/src/`) only ever talks to relative URLs: `fetch("/api/state")`, and the WebSocket is opened as `` `${proto}//${location.host}/api/stream` `` (`web/src/connection.ts:268`). Nothing in the frontend hardcodes an origin.

Because of that, the desktop shell does not bundle `web/dist` or embed any frontend assets. Instead its window is pointed directly at the backend's own URL (e.g. `http://localhost:8080`), which already serves the built frontend at `/`, the REST API at `/api/*`, and the WebSocket at `/api/stream`. Since the page runs from that origin, every relative fetch and the WebSocket connection resolve correctly against the backend with zero frontend changes. This is the entire point of the design — the desktop shell is a window, not a packaged copy of the app.

`desktop/dist/index.html` is a tiny stub required only because Tauri's `frontendDist` config option wants a directory to point at even when it's never shown — the real window is created in `src/main.rs` with `WebviewUrl::External(url)`, not from that stub.

## Config resolution order

The backend URL is resolved in `desktop/src/main.rs` (`resolve_backend_url`), in this order:

1. `LURKER_URL` environment variable
2. `backend_url:` key in `~/.config/lurker/desktop.yaml`
3. default `http://localhost:8080`

This mirrors the `backend_url` convention already used by the TUI client (`cmd/tui/config.go`, `~/.config/lurker/tui.yaml`). The YAML file is parsed with a plain line scan for a `backend_url:` prefix rather than a real YAML library, since it's a single key — see the `ponytail:` comment in `main.rs` for the upgrade path if the config ever grows.

## Running it

- `task desktop-dev` — runs the shell in dev mode (`cargo tauri dev` in `desktop/`)
- `task build-desktop` — builds the release bundle (`cargo tauri build` in `desktop/`)

Point it at a non-default backend with either `LURKER_URL=http://localhost:8099 task desktop-dev` or a `~/.config/lurker/desktop.yaml` containing `backend_url: http://localhost:8099`. The backend itself must already be running — the desktop shell is just a window, it doesn't start or manage the Go process.

## External links

Links in the frontend are rendered with `target="_blank"` (message-text linkification in `web/src/format.ts`, link previews in `web/src/preview.ts`) — correct behavior for a real browser, and not something the desktop shell should ask the frontend to change. But the OS system webviews Tauri embeds (WKWebView on macOS, WebKitGTK on Linux) have no default handler for `target="_blank"`: without intervention the click is just silently dropped, and nothing opens.

`desktop/src/main.rs` works around this with two pieces on the `WebviewWindowBuilder`:

- An `initialization_script` installs a capturing `click` listener that finds the closest `a[href]`. If the link is `target="_blank"`, or its resolved origin differs from `location.origin`, the listener cancels the click and navigates the window to a same-origin sentinel URL (`/__open_external?url=<encoded target>`) instead — init scripts can't call Rust directly, so the sentinel is how the click's target URL crosses into `on_navigation`. Same-origin `target="_blank"` links (e.g. uploaded media served by the backend) are included deliberately: without this they'd otherwise navigate in-app and replace the chat UI, which is also wrong.
- An `on_navigation` handler recognizes the sentinel URL, extracts and percent-decodes the `url` query param, and cancels the in-app navigation (returns `false`) so the sentinel URL itself is never loaded. All other navigations are allowed through unchanged.

Before handing the extracted URL to the system opener, the scheme is checked to be exactly `http` or `https` — the URL originates from page content (including remote IRC message text), so this is a real trust boundary; `file:`, `javascript:`, and other schemes are rejected outright. The browser is launched with `open` (macOS) or `xdg-open` (Linux) via `std::process::Command::spawn`, not `status`/`output`, so the UI thread is never blocked waiting on it.

## macOS: cleartext HTTP backends

`desktop/Info.plist` sets `NSAppTransportSecurity` / `NSAllowsArbitraryLoads`, which Tauri merges into the generated bundle `Info.plist` (per the [macOS bundle docs](https://tauri.app/distribute/macos-application-bundle/), any `Info.plist` dropped in the `desktop/` folder is merged over Tauri's generated values). Without it, WKWebView refuses to load any `http://` origin and the window comes up blank — `localhost` is exempt from ATS by default, which is why this was easy to miss in local testing, but a Tailscale `ts.net` hostname (`http://box.tailnet.ts.net`) is not. `NSAllowsLocalNetworking` doesn't cover it either, since it only exempts `.local`/link-local names and `ts.net` names resolve to CGNAT `100.x` addresses. Because `backend_url` is arbitrary user config, there's no fixed set of hostnames to list as `NSExceptionDomains`, so this allows arbitrary cleartext loads outright. This is a deliberate tradeoff, not an oversight: Lurker is private-network-only (loopback, Tailscale, or another trusted network — see `AGENTS.md`), so an HTTP-only Tailnet backend is a first-class supported config, not a mistake to guard against.

## Native drag-and-drop is disabled

Tauri's webview has its own native drag-and-drop handler, enabled by default, which would otherwise intercept a dropped file before the page's own JS ever sees the `drop` event. `desktop/src/main.rs` calls `.disable_drag_drop_handler()` on the `WebviewWindowBuilder` so dropped files fall through to the frontend's own HTML5 `dragover`/`dragleave`/`drop` handling on the composer (`web/src/input-upload.ts`), which is what actually uploads the file. Leaving the native handler on doesn't error — it just silently swallows the drop, so drag-to-upload does nothing.

## Icons

The Tauri icon set under `desktop/icons/` is generated from `web/public/icon-512.png` via `cargo tauri icon ../web/public/icon-512.png` (run from `desktop/`) and committed, so it doesn't need regenerating unless the source icon changes.

## Known caveat: Linux/WebKitGTK

The PoC has only been built and exercised on macOS (WKWebView) so far. The eventual target also includes Linux via WebKitGTK, which is known to have rendering problems on some distributions — notably Fedora-family systems, where DMA-BUF rendering in WebKitGTK can produce a blank or broken webview. The workaround, if this is hit, is to run with `WEBKIT_DISABLE_DMABUF_RENDERER=1` set in the environment. This hasn't been verified against Lurker's desktop shell yet since no Linux build has been attempted; it's noted here so it isn't a surprise when someone does.
