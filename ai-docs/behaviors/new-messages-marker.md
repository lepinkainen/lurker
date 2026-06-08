# New Messages Marker

## Purpose

Horizontal divider line ("New messages") in a channel buffer that marks the boundary between messages the user has already seen and messages that arrived while they weren't looking. Lets the user resume reading at the right spot after stepping away.

## Trigger

A channel accumulates new messages while **either**:

- the browser tab/window hosting the web UI is not focused (no `document.hasFocus()` / `visibilitychange` = visible), **or**
- the user is focused on the web UI but viewing a different channel (active buffer is not this channel).

The marker is inserted at the position of the first unseen message for that channel.

## Expected

- Marker is per-channel, not global. Every channel tracks its own "last read" position.
- Marker is visible on **all** channels that have unread messages, not only the active one.
- Marker disappears once the channel is marked as read.
- A channel is marked as read when the user views it **while the web UI is focused** and then navigates away from it (switches to another channel, or the buffer otherwise loses active status). Re-entry to that channel should show no marker unless new messages have arrived since.
- If the tab is unfocused, viewing a channel does not mark it as read — the user might not actually be looking.

## Cases

### Case A — tab regains focus

1. Tab is unfocused. New messages arrive in channel X (the active buffer). Marker appears above the first new message.
2. User refocuses the tab, sees the "New messages" line in channel X, reads through it. Marker stays visible (channel not yet marked read — user is still on it).
3. User switches to channel Y. Channel X is now marked read; its marker is cleared.
4. User switches back to channel X. No marker (no new messages since mark-read).

### Case B — channel switch while focused

1. User is focused on the web UI, viewing channel A. New messages arrive in channel B → channel B gets a "New messages" marker at the first unseen message.
2. User switches to channel B. Marker is visible. Channel B is **not** marked read on entry.
3. User switches to channel A. Channel B is now marked read; marker cleared.
4. User switches back to channel B. No marker if no new messages arrived since step 3. If new messages did arrive, a new marker appears at the first of those.

## Edge cases

- **Tab unfocused, switching channels**: switching channels while the tab is unfocused does not mark anything as read. Markers persist until focus returns and the user navigates away.
- **Active channel, tab focused, new messages arrive**: do **not** add a marker — the user is actively watching. Mark-read position advances with each arriving message.
- **Initial load**: on first load of the UI, no markers. The first time a channel is viewed establishes its read position.
- **Reconnect / history replay**: messages arriving via history backfill (older than known position) must not trigger a marker.
- **Empty channel becomes non-empty**: marker appears above the very first message if the channel was never visited; otherwise above the first new one.
- **Marker position is sticky**: once placed, the marker does not move as more messages arrive — it represents the position at the moment unread state began. Only mark-read clears it.

## Non-goals

- No persistence across reloads is required for v1 (mark-read state may live in memory only). Document this as a known limitation; revisit if users complain.
- No per-message read receipts. Only one marker per channel.
- No notification badges interaction defined here (see separate behavior if/when added).

## Related

- `ai-docs/frontend.md` — buffer/channel UI structure
- `ai-docs/websocket-protocol.md` — message arrival events
- Code: `web/src/` buffer rendering and focus tracking (`visibilitychange`, `focus`/`blur` listeners)
