# Keyboard shortcuts and channel switcher spec

This document defines the initial UX spec for:

- issue #25: channel switcher fuzzy-search overlay across all buffers
- issue #29: keyboard shortcuts and help overlay

The goal is to add a small, reliable v1 shortcut set that improves navigation without interfering with normal message typing.

## Scope

In scope for v1:

- global keyboard shortcut handling in the web UI
- a searchable channel switcher overlay
- a keyboard shortcuts help overlay
- buffer-to-buffer navigation shortcuts
- unread and mention navigation shortcuts
- first-unread-channel shortcut
- sidebar toggle shortcut
- input focus shortcut

Out of scope for v1:

- history search
- multi-key chord navigation like `g h`
- customizable shortcuts
- mobile gesture support
- TUI parity work
- slash-command palette behavior beyond the channel switcher

## Design principles

- Prefer a small set of memorable shortcuts.
- Avoid shortcuts that commonly conflict with browser or OS defaults.
- Do not hijack normal typing in the message input.
- Make every global shortcut discoverable in the help overlay.
- Let `Esc` consistently close the active transient UI first.

## v1 shortcut set

### Global shortcuts

- `?` — open keyboard shortcuts help overlay
- `Ctrl+K` / `Cmd+K` — open channel switcher
- `Esc` — close the active overlay or dialog

### Buffer navigation

- `Alt+ArrowUp` — go to previous visible buffer
- `Alt+ArrowDown` — go to next visible buffer
- `Alt+Shift+ArrowUp` — go to previous unread channel buffer
- `Alt+Shift+ArrowDown` — go to next unread channel buffer
- `Alt+A` — jump to first unread channel buffer
- `Alt+M` — go to next channel buffer with a mention/highlight
- `Alt+S` — go to the current network's status buffer

### Layout and input

- `Ctrl+B` / `Cmd+B` — toggle sidebar visibility
- `Ctrl+L` / `Cmd+L` — focus the message input for the active buffer

## Input focus rules

Global shortcuts must not interfere with normal text entry.

When focus is inside a text-editing control, the app must ignore global shortcuts except for the following:

- `Esc` may still close the topmost overlay if focus is inside that overlay
- switcher-local keys remain active while focus is in the switcher input

Text-editing controls include:

- message composer
- search or filter inputs
- modal form inputs
- textareas
- any element with editable text semantics

`?` should only open the help overlay when the user is not typing into a text-editing control.

## Overlay priority and `Esc`

When `Esc` is pressed, close or cancel the highest-priority active UI in this order:

1. channel switcher
2. shortcuts help overlay
3. other modal dialogs
4. otherwise no action

`Esc` should not change buffers by itself.

## Channel switcher spec

## Purpose

Provide fast fuzzy navigation across all buffers from anywhere in the app.

## Open behavior

When opened via `Ctrl+K` / `Cmd+K`:

- show a centered overlay with a text input and result list
- focus the switcher input immediately
- prefill the input as empty
- show the current buffer in results like any other buffer
- default-select the first result

## Search domain

The switcher searches across all navigable buffers, including:

- network status buffers
- channels
- direct-message/query buffers

It must not show non-buffer UI destinations.

## Matching behavior

v1 matching should be simple and tolerant:

- case-insensitive
- substring match is acceptable for first implementation
- fuzzy ranking is allowed but not required for v1

Fields considered for matching:

- buffer name
- network name
- combined display text such as `#channel — libera`

## Result ordering

Recommended ordering for v1:

1. best textual match
2. buffers with unread or mention state ranked above otherwise equal matches
3. current network buffers slightly preferred over others when scores tie
4. stable fallback order based on existing sidebar/buffer ordering

Exact scoring is implementation-defined as long as results feel stable.

## Result display

Each result row should display enough context to disambiguate similarly named buffers:

- buffer name
- network name
- optional unread badge
- optional mention/highlight indicator
- status buffer label when applicable

## Keyboard behavior inside the switcher

- `ArrowUp` — move selection up
- `ArrowDown` — move selection down
- `Enter` — jump to selected buffer and close switcher
- `Esc` — close switcher without changing buffer

Optional but acceptable in v1:

- wrap selection when moving past first/last result

## Mouse behavior inside the switcher

- clicking a result jumps to that buffer and closes the switcher
- clicking outside the overlay closes it

## Empty state

If no results match:

- keep the overlay open
- show a clear `No matching buffers` state
- `Enter` does nothing

## Buffer navigation shortcut behavior

### Previous/next visible buffer

`Alt+ArrowUp` and `Alt+ArrowDown` navigate through the same visible buffer order presented in the sidebar.

If the sidebar supports collapsed sections, navigation still uses the logical visible ordering of buffers, not raw DOM order.

### Previous/next unread buffer

Unread navigation (`Alt+Shift+ArrowUp` / `Alt+Shift+ArrowDown`) navigates through channel-kind buffers (`kind = "channel"`) with unread messages, excluding the currently active buffer.

If no unread channel buffer exists in the requested direction, wrapping around once is preferred.

### First unread buffer

`Alt+A` jumps to the first channel buffer with unread messages, following sidebar order. If the active buffer already is that buffer, or no unread channel exists, do nothing.

### Next mention buffer

`Alt+M` navigates to the next channel buffer (`kind = "channel"`) with a mention/highlight state.

Mention navigation should prefer:

- channel buffers with unread mentions/highlights
- sidebar order for tie-breaking and wraparound

If no mention buffer exists, do nothing.

### Jump to status buffer

`Alt+S` jumps to the active network's status buffer.

If the active buffer already is the status buffer, do nothing.

## Sidebar toggle behavior

`Ctrl+B` / `Cmd+B` toggles sidebar visibility.

This shortcut only affects client-side layout state and must not change buffer selection.

## Focus input behavior

`Ctrl+L` / `Cmd+L` focuses the message composer for the current active buffer.

If the current view has no writable message input, do nothing.

This shortcut should not clear existing draft text.

## Shortcuts help overlay spec

The help overlay should be intentionally small and static in v1.

Sections:

- General
- Buffer navigation
- Layout and input
- Channel switcher controls

Suggested rows:

- `?` — Open keyboard shortcuts
- `Ctrl+K` / `Cmd+K` — Open channel switcher
- `Esc` — Close active overlay
- `Alt+ArrowUp` — Previous buffer
- `Alt+ArrowDown` — Next buffer
- `Alt+Shift+ArrowUp` — Previous unread buffer
- `Alt+Shift+ArrowDown` — Next unread buffer
- `Alt+A` — First unread buffer
- `Alt+M` — Next mention buffer
- `Alt+S` — Jump to status buffer
- `Ctrl+B` / `Cmd+B` — Toggle sidebar
- `Ctrl+L` / `Cmd+L` — Focus message input
- `ArrowUp` / `ArrowDown` / `Enter` / `Esc` — Switcher navigation

The overlay may be read-only and does not need search in v1.

## Accessibility and usability notes

- Overlays should trap focus while open.
- The switcher input should have an accessible label.
- Selected result row should be announced through normal listbox or active-descendant semantics.
- Shortcut hints should use platform-aware labels (`Cmd` on macOS, `Ctrl` elsewhere) when rendered.

## Implementation notes

Recommended decomposition:

- a central keybinding registry in frontend code
- a lightweight overlay state model for `switcher` and `shortcuts-help`
- buffer navigation helpers shared by sidebar and shortcut actions
- a reusable buffer search helper for the channel switcher

## Acceptance criteria

- All listed shortcuts work from normal app view when not typing in an input.
- No listed global shortcut breaks normal text entry in the composer or modal forms.
- `Ctrl+K` / `Cmd+K` opens a buffer switcher and `Enter` navigates to the selected result.
- `?` opens a help overlay listing the v1 shortcut set.
- `Esc` closes the topmost overlay without changing buffer unless navigation was explicitly confirmed.
- Previous/next, unread, mention, and status-buffer navigation work against stable buffer ordering.
- Sidebar toggle and input focus shortcuts behave consistently across reloads and reconnects.
