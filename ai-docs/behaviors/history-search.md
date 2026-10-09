# History search

Full-text search over stored messages, opened as a modal overlay on top of the
chat. Backed by `GET /api/search` (FTS5 per log DB, see `rest-api.md`) and
`GET /api/buffers/{id}/history?around=` for context windows.

Web implementation: `web/src/history-search.ts` + `.hs-*` rules in
`web/src/styles/overlays.css`. Apple implementation: see `apple.md`.

## Opening

- `Ctrl+Shift+F` / `Cmd+Shift+F` (not `Ctrl+F`: browser find keeps working
  for on-screen text)
- magnifier button in the buffer header actions (the only entry point on touch
  devices without a keyboard)
- `/search [text]` slash command — opens the overlay with `text` prefilled and
  already searched

Reopening while open refocuses the input (and replaces the query when invoked
via `/search`). Opening closes the shortcuts help overlay; it refuses to open
while another `<dialog>` is open (`canOpenModal`).

## Scope

Segmented control next to the input: `[<active buffer name>] [All channels]`.

- Default is the active buffer. When the active buffer is a status buffer the
  buffer option is disabled and the scope starts at "All channels".
- Switching scope reruns the current query immediately.
- Scope is not persisted; every open starts from the default.

## Query and results

- Input is debounced 250 ms; an in-flight request is aborted when the query
  changes. Blank input clears the list.
- The client sends the raw text; the server (`db.FTSQuery`) quotes tokens and
  adds a prefix wildcard to the last one, so results refine while typing and
  FTS5 operators are never interpreted.
- Results arrive newest first in pages of 50. Scrolling near the bottom of the
  list fetches the next page with `before=<last id>`. The meta line shows
  `N matches` once the end is reached, `N+ matches` while more may exist.
- Only `privmsg`/`notice`/`action` kinds come back (server-side filter), so
  searching a nick finds their messages, not their join/part/quit rows.
- Row layout: time label, nick (nick-colored), content with every query token
  wrapped in `<mark>` (client-side, case-insensitive substring match, longest
  token first). In "All channels" scope a `network · #buffer` line is added.
- Time label: `HH:MM` today, `Mon HH:MM` within the last week, `28 Sep` older.

## Keyboard

- `↑` / `↓` move the selection (wraps). Mouse hover also selects.
- `Enter` (or click) toggles the selected hit's context panel in place.
- `Esc` collapses an expanded hit; `Esc` again closes the overlay. The
  overlay's own handler stops propagation so the global `Esc` path never acks
  the buffer's unread marker from inside search.
- Typing continues to filter; focus stays in the input throughout.

## Context panel

Expanding a hit requests `history?around=<hit id>&limit=30`, which the server
answers with up to 15 older messages, the hit, and up to 15 newer ones, oldest
first. The panel renders them in a mono block (time, nick, content), tints the
hit row and marks the query tokens in it, and scrolls the hit into view.
Only one hit is expanded at a time; paging preserves the expanded hit.

Context is read-only and lives inside the overlay. The message pane is never
scrolled or detached from the live tail. "Open in channel" (jump the real
timeline to that message) is deliberately not implemented: it needs a
detached-from-live-tail state in the message pane plus a "jump to present"
affordance, and in-overlay context covers the browsing need.

## Phone width (≤ 640 px)

The dialog becomes full-screen: scope control stretches to full width, result
rows wrap to nick/time on one line and content below, and the keyboard hint
line is hidden.

## Backend version skew

The result list works against a backend that predates `before`/`around`
support (unknown query parameters are ignored). Paging and context expansion
require the backend from commit `33d7013` or later; against an older image the
context panel shows the server's error text instead of messages.
