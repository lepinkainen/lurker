# Startup Active Channel

## Purpose

On client startup, restore the user's last viewed channel so they resume where they left off. Applies to both Web UI and TUI clients.

## Trigger

Client (Web UI or TUI) boots and finishes loading the buffer list.

## Expected

Client picks the active buffer on startup using this priority chain:

1. **URL hash / deep-link.** If the URL points to a buffer that exists, use it. Lets the user bookmark a specific channel; reloading the page restores that buffer.
2. **Persisted "last viewed" buffer.** Whenever the active buffer changes, the client persists its id. On startup, if the persisted value resolves to an available buffer, use it. Lets the user close the tab, type the root URL, and resume where they left off.
3. **First channel in sort order, pinned first.** Pinned channels come first (sorted by their network's `sort_order`). Then per network in `sort_order`, joined channels by name.
4. **Any buffer, status windows last.** Per network in `sort_order`: channels → queries → parted channels → status window. The server/status buffer is the absolute last resort.

If there are no buffers at all, no buffer is active; UI shows empty state.

## Cases

### Case A — last channel available

1. User was on `#foo` on network `libera` when client closed.
2. Client starts. `#foo` on `libera` is in the buffer list and joined.
3. Active buffer = `libera/#foo`.

### Case B — last channel unavailable

1. User was on `#foo` on network `libera`. Before next start, `#foo` is parted (or `libera` is removed/disabled).
2. Client starts. Persisted buffer no longer resolves.
3. Active buffer = first pinned channel by network `sort_order`. If none pinned, first joined channel of the first network in `sort_order`. Status windows are skipped unless they are the only buffers available.

### Case C — no persisted value

1. Fresh install or cleared state.
2. Active buffer follows the same fallback as Case B: first pinned channel; else first channel of the top network; else status as last resort.

### Case D — no buffers

1. No networks configured, or no joined channels.
2. No active buffer. Empty state.

## Edge cases

- **Race with buffer list load**: do not apply fallback before the buffer list has finished loading from the server. Premature fallback would pick the wrong buffer.
- **Network connecting**: if the persisted network is still in the process of connecting/joining at startup, prefer to wait briefly for the target buffer to appear rather than falling back immediately. Reasonable cap (e.g. a few seconds) before fallback.
- **Persistence write frequency**: persist on active-buffer change, not on every render. Debounce if needed.
- **Storage location**:
  - Web UI: `localStorage` (per-browser).
  - TUI: `os.UserConfigDir()/lurker/tui-state.json` (atomic tmp+rename write, mode 0600). Not in `data/` server dir — this is client state.
- **Stable identifier**: persist a stable composite key (e.g. `network_id` + `buffer_name`), not display name. Network rename is out of scope per project invariants, but buffer names can change case across IRC servers — match case-insensitively where appropriate.

## Non-goals

- No cross-client sync (Web UI and TUI persist independently).
- No history of last-N channels; only the most recent.
- No per-network "last viewed" memory; just one global value.

## Related

- `ai-docs/frontend.md` — Web UI buffer/sidebar
- `ai-docs/tui.md` — TUI buffer navigation
- [[new-messages-marker]] — marker behavior interacts with active-buffer changes
