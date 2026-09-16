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

On Linux the shell must be run with `WEBKIT_DISABLE_DMABUF_RENDERER=1` or it
exits on launch — see [Linux/WebKitGTK](#linuxwebkitgtk-verified-with-one-hard-requirement).

Point it at a non-default backend with either `LURKER_URL=http://localhost:8099 task desktop-dev` or a `~/.config/lurker/desktop.yaml` containing `backend_url: http://localhost:8099`. The backend itself must already be running — the desktop shell is just a window, it doesn't start or manage the Go process.

## External links

Links in the frontend are rendered with `target="_blank"` (message-text linkification in `web/src/format.ts`, link previews in `web/src/preview.ts`) — correct behavior for a real browser, and not something the desktop shell should ask the frontend to change. But the OS system webviews Tauri embeds (WKWebView on macOS, WebKitGTK on Linux) have no default handler for `target="_blank"`: without intervention the click is just silently dropped, and nothing opens.

`desktop/src/main.rs` works around this with two pieces on the `WebviewWindowBuilder`:

- An `initialization_script` installs a capturing `click` listener that finds the closest `a[href]`. If the link is `target="_blank"`, or its resolved origin differs from `location.origin`, the listener cancels the click and navigates the window to a same-origin sentinel URL (`/__open_external?url=<encoded target>`) instead — init scripts can't call Rust directly, so the sentinel is how the click's target URL crosses into `on_navigation`. Same-origin `target="_blank"` links (e.g. uploaded media served by the backend) are included deliberately: without this they'd otherwise navigate in-app and replace the chat UI, which is also wrong.
- An `on_navigation` handler recognizes the sentinel URL, extracts and percent-decodes the `url` query param, and cancels the in-app navigation (returns `false`) so the sentinel URL itself is never loaded. All other navigations are allowed through unchanged.

Before handing the extracted URL to the system opener, the scheme is checked to be exactly `http` or `https` — the URL originates from page content (including remote IRC message text), so this is a real trust boundary; `file:`, `javascript:`, and other schemes are rejected outright. The browser is launched with `open` (macOS) or `xdg-open` (Linux) via `std::process::Command::spawn`, not `status`/`output`, so the UI thread is never blocked waiting on it.

## macOS: cleartext HTTP backends

`desktop/Info.plist` sets `NSAppTransportSecurity` / `NSAllowsArbitraryLoads`, which Tauri merges into the generated bundle `Info.plist` (per the [macOS bundle docs](https://tauri.app/distribute/macos-application-bundle/), any `Info.plist` dropped in the `desktop/` folder is merged over Tauri's generated values). Without it, WKWebView refuses to load any `http://` origin and the window comes up blank — `localhost` is exempt from ATS by default, which is why this was easy to miss in local testing, but a Tailscale `ts.net` hostname (`http://box.tailnet.ts.net`) is not. `NSAllowsLocalNetworking` doesn't cover it either, since it only exempts `.local`/link-local names and `ts.net` names resolve to CGNAT `100.x` addresses. Because `backend_url` is arbitrary user config, there's no fixed set of hostnames to list as `NSExceptionDomains`, so this allows arbitrary cleartext loads outright. This is a deliberate tradeoff, not an oversight: Lurker is private-network-only (loopback, Tailscale, or another trusted network — see `AGENTS.md`), so an HTTP-only Tailnet backend is a first-class supported config, not a mistake to guard against.

## Uploads: drag-and-drop and paste are handled in Rust

Both upload paths run on the Rust side of the shell, not in the page. This is a
Linux/WebKitGTK requirement, not a preference — see the reasoning below before
changing it back.

**Drop.** Tauri's native drag-and-drop handler is left **on**, and
`desktop/src/main.rs` reads `WindowEvent::DragDrop(DragDropEvent::Drop { paths, .. })`.
On macOS/WKWebView you can instead call `.disable_drag_drop_handler()` and let the
frontend's HTML5 `dragover`/`drop` handling on the composer
(`web/src/input-upload.ts`) do the work, which is what this PoC originally did.
On WebKitGTK that is not an option: a dropped file reaches the page as
`text/uri-list` with `DataTransfer.files` empty, so the frontend's
`files` / `items[].kind === "file"` path is unreachable, and with the native
handler disabled the webview falls through to its own widget-level default and
**navigates the window to `file:///…`**, replacing the chat UI with the dropped
image and no way back but restarting. Keeping Tauri's handler on fixes both at
once: it consumes the drop before WebKitGTK sees it. Because that handler fires
for the whole window rather than the composer, a drop is gated — see
[Which drops are accepted](#which-drops-are-accepted).

**Paste.** `PASTE_INIT_SCRIPT` watches for a paste the page cannot handle and
hands it to Rust through a sentinel URL (`/__paste_upload`), the same trick
`EXTERNAL_LINK_INIT_SCRIPT` already uses for links. It deliberately stands aside
whenever the page *can* cope — a paste carrying text, or one with a real `File`
attached, is left completely alone. What reaches WebKitGTK depends on the source,
and none of it is usable by the page:

| Copied from | `clipboardData.types` | `files` / `items` |
|---|---|---|
| File manager (Dolphin) | `text/uri-list` | 0 files, one `kind=string` item |
| Firefox → Copy Image | `text/html` | 0 files, one `kind=string` item |
| Raw bitmap (screenshot tool) | *(empty)* | nothing at all |

The bare-bitmap row is the important one: WebKitGTK delivers a `paste` event with
an entirely empty `clipboardData` while the system clipboard demonstrably holds
the image. No change to `clipboardImage()` in `web/src/input-upload.ts` can
recover from that, which is why the fix is not in `web/`.

Rust therefore reads the clipboard itself via `wl-clipboard-rs`, preferring
`image/png` when several flavours are offered and falling back to reading
`text/uri-list` for a file reference. The URI the page managed to read is passed
along as a hint but never relied on — WebKitGTK does not always honour
`getData("text/uri-list")` during a paste, and an earlier version that trusted it
failed on exactly the file-manager case it was meant to serve.

Both paths meet at `upload_bytes`, which mirrors `uploadFile` in
`web/src/input-upload.ts` including its `>= 1 MiB` sha256 dedupe precheck against
`/api/media/exists`, then POSTs multipart to `/api/upload`. The returned URL is
inserted at the composer caret by `insert_script`, which reproduces
`insertTextAtCursor`'s leading/trailing space rules and dispatches a real `input`
event so the command popup bound in `web/src/input.ts` stays in step.

Progress and errors reuse the composer's existing `.upload-note` element rather
than inventing any styling, but `note_script` chooses where to parent it. That
element is `position: absolute` inside `.inputbar` at `z-index: 11`, so it cannot
be raised above a dialog from its own stacking context. When something is
covering the composer the same element is parented to `<body>` and pinned to the
viewport instead, so a refusal is legible rather than hidden behind the very
thing that caused it. Same element id either way, so a note never appears twice.
Errors clear themselves after 8s, matching the frontend's `NOTE_ERROR_MS`.

### Which drops are accepted

Tauri's native handler reports drops anywhere in the window, and Rust cannot see
the page's state, so the page is asked before anything is uploaded. Rust parks
the dropped path, evaluates `DROP_GATE_SCRIPT`, and the page answers with a
sentinel navigation: `/__drop_accept`, or `/__drop_reject?why=…` carrying a
reason for the note. The parked path is single-use — taken on either reply — so a
stale path can never be picked up by a later navigation.

The gate tests **state, not the drop's coordinates**. A drop anywhere in the
window is accepted, which matches how other chat clients behave and keeps the
target larger than the composer strip; what it refuses is a drop whose result
would land somewhere the user cannot see. Two details are easy to get wrong:

- **The settings view is not a `<dialog>`.** `openSettingsView`
  (`web/src/settings-dialog.ts`) builds a modeless `div[role="dialog"]`, unlike the
  channel switcher, shortcuts help, network form and sidebar dialogs, which are
  real `<dialog>`s opened with `showModal()`. A `dialog[open]` test therefore
  misses the one overlay most likely to be open when someone drops an image. The
  gate keys off `dialog[open], [role="dialog"]` so it catches both.
- **A disabled composer does not refuse a drop**, even though
  `PASTE_INIT_SCRIPT` refuses a paste then. That asymmetry is inherited rather
  than chosen: the frontend's own paste handler bails on `inputEl.disabled`
  (`web/src/input-upload.ts`), while its upload button never does — the paperclip
  uploads fine on a channel you have not joined. A drop is the same gesture as
  that button, so it follows the button. Gating it made drag-and-drop stricter
  than the control sitting next to it.

### Paste is Wayland-only, and costs more bytes than dropping

`wl-clipboard-rs` speaks the Wayland data-control protocol, so on an X11 session
the bitmap paste reports `clipboard unavailable` in the composer note. Dropping a
file still works there, as does pasting a file copied in a file manager, since
neither needs a bitmap off the clipboard. `arboard` would cover X11 but decodes to
RGBA — forcing a re-encode and pulling roughly twice the dependencies — so this is
a deliberate trade, not an oversight.

Pasting an image is also markedly heavier than dropping the file it came from,
because applications put a decoded PNG on the clipboard. The same 1000x1093 photo
measured 1.32 MB pasted as a bitmap and 139 KB dropped as its original JPEG. When
size matters, copy the file rather than the image.

## Icons

The Tauri icon set under `desktop/icons/` is generated from `web/public/icon-512.png` via `cargo tauri icon ../web/public/icon-512.png` (run from `desktop/`) and committed, so it doesn't need regenerating unless the source icon changes.

## Linux/WebKitGTK: verified, with one hard requirement

Verified on Bazzite 44 (`ghcr.io/ublue-os/bazzite-nvidia-open:stable`,
`44.20260902`), KDE Plasma on Wayland, WebKitGTK `2.52.5`, NVIDIA `610.57.04`
(issue #149).

### `WEBKIT_DISABLE_DMABUF_RENDERER=1` is required, not a fallback

This was previously noted as a possible blank-webview workaround "if this is
hit". On Plasma Wayland with the NVIDIA driver it is not optional and the symptom
is not a blank view — the shell **exits immediately on launch**:

```
wl_display#1.error(wp_linux_drm_syncobj_surface_v1#44, 4,
                   "explicit sync is used, but no acquire point is set")
Gdk-Message: Error 71 (Protocol error) dispatching to Wayland display.
```

WebKitGTK's DMA-BUF renderer binds a `wp_linux_drm_syncobj_surface_v1` and then
commits a buffer without setting an acquire point; KWin correctly kills the client
for the protocol violation. With the variable set, everything renders correctly at
the right HiDPI scale. It should be set in the environment the shell ships with
rather than left to the user to discover.

### Building on an atomic/immutable host

Bazzite's `/usr` is read-only and ships no `-devel` packages, so the build runs in
a `fedora:44` distrobox — matching the host's Fedora base so the container's
`webkit2gtk4.1-devel` (2.52.5) is the exact version the binary will run against.
Build there, then run the binary **on the host**, where it resolves every library
out of the host's `/usr/lib64`. Running it inside the container would exercise the
container's WebKitGTK and portal plumbing instead of the real session.

Fedora also ships Go with `GOTOOLCHAIN=local`, which hard-fails against this
repo's `go 1.27.0` directive; set `GOTOOLCHAIN=auto` in the container.

### AppImage bundling needs `NO_STRIP=1` on Fedora 44

`cargo tauri build` produces the binary and the deb/rpm bundles cleanly, but the
AppImage step fails twice over. `linuxdeploy` is itself a type-2 AppImage and
needs libfuse2 (`fuse` and `fuse-libs`; fuse3 alone is not enough). Once it runs,
it fails again because it vendors a 2024-vintage `strip` that rejects the
`.relr.dyn` section (`SHT_RELR`, type `0x13`) every Fedora 44 library now carries:

```
strip: unknown type [0x13] section `.relr.dyn'
```

`NO_STRIP=1 cargo tauri build` skips the stripping and the AppImage builds.
