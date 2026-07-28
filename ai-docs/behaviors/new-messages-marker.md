# New Messages Marker

## Purpose

Horizontal divider line ("New messages") in a channel buffer that marks the boundary between messages the user has already seen and messages that arrived since. Lets the user resume reading at the right spot after stepping away.

## Model

One lifecycle, server-owned. The read position **is** the marker position:

- The server persists `last_seen_id` per buffer (exclusive: everything with `id > last_seen_id` is unread).
- The server derives `marker_id` = id of the first unread message that counts (skipping non-unread kinds and self-authored messages) and includes it in `/api/state` buffers and in `buffer_update` events. `marker_id` absent/null means no marker.
- Clients hold **no marker state of their own**. They render the divider above the message whose id equals `marker_id`, and the unread bar whenever `marker_id` is set on the active buffer. UI is a pure function of server state plus loaded messages.
- Unread badges, the divider, and the unread bar all share this one lifecycle: they appear together and clear together.

## Clearing — explicit ack only

Nothing clears implicitly. Not opening the buffer, not scrolling, not focusing the window, not reloading, not reconnecting.

The only clear is an explicit user ack, which sends `mark_read {buffer_id, message_id: <newest loaded message>}`:

1. **Esc** (with no overlay/drawer open on top), on platforms with a keyboard.
2. Activating the **unread bar** (click/tap).

On ack the client may optimistically drop the divider/bar/badge locally; the server persists the new `last_seen_id`, recomputes counts, and broadcasts `buffer_update {id, network_id, last_seen_id, marker_id, unread, mentions}` to all clients. Because marker state is derived from `last_seen_id`, an ack on one device clears the marker and badges on **every** device — dismissal syncs by construction.

## Unread bar

A bar pinned at the top of the message area, shown whenever the active buffer has a `marker_id`.

- **Content**: the number of unread messages — "12 new messages". When the count is unreliable (at the server cap of 1000, or the marker message is outside loaded history), show the age of the boundary instead: "new since 14:02" (older than today: "new since yesterday 14:02" / date).
- **Action**: one activation = ack (clear marker + badges everywhere).
- The bar is the primary dismiss affordance on platforms without a convenient Esc (touch devices, the Apple client).
- The divider line and the bar always appear/disappear together — two views of the same server state.

## Message arrival

- New message in a buffer: server persists it; if it counts as unread and is not self-authored, unread count grows and `marker_id` stays at the first unread message (sticky by derivation — the first unread doesn't change as more arrive).
- Self-authored messages (from any client) never count toward unread and never become the marker anchor.
- Clients accumulate badge counts locally from per-message flags between syncs; server-authoritative counts arrive on `buffer_update` and `/api/state`.
- There is no "actively watching" suppression: if you are viewing a buffer and messages arrive, the marker exists (you haven't acked). The divider sits above the first unread message; for a user reading at the bottom this is simply where they stopped last acking. Clients must not auto-ack on arrival.

## Cases

### Case A — stepped away

1. Messages arrive in channel X while the user is elsewhere (other buffer, unfocused tab, app closed). Badge counts up; marker sits above the first unread message.
2. User opens channel X. Divider and unread bar are visible. Badge stays.
3. User reads, presses Esc (or taps the bar). Badge, divider, and bar clear — on this and every other client.
4. Reload/restart at any point before step 3: badge, divider, and bar all come back exactly the same (server-derived).

### Case B — multi-device

1. Channel B has 12 unread. Phone and desktop both show badge + marker.
2. User acks on the phone. Server broadcasts `buffer_update`; desktop badge and marker clear too.
3. New messages after the ack start a fresh marker at the first of them, everywhere.

### Case C — marker outside loaded history

1. Channel has 3000 unread; client loads the most recent 200 messages. `marker_id` points at a message the client never loaded.
2. No divider is drawn (its message isn't in the list), but the unread bar shows in "new since" form using server data.
3. Paging back through history eventually loads the marker message; the divider appears at it.
4. Ack works the same regardless (acks the newest loaded message).

## Edge cases

- **Empty buffer**: no messages loaded → nothing to ack; bar hidden even if server reports unread (nothing renderable). Server state is authoritative; the next sync reconciles.
- **Never-visited buffer** (`last_seen_id` null): everything unread; marker at the first countable message.
- **History replay / backfill**: derivation is by id comparison against `last_seen_id`, so replayed older messages can never create a new marker.
- **Ack while disconnected**: client may clear optimistically but must retry `mark_read` on reconnect (or resync from `/api/state`, which restores the marker if the ack never landed — visible state stays truthful to the server).
- **Marker message hidden by presence filtering**: cannot happen for self/presence kinds (they never anchor); if the anchored message is filtered by other view options, render the divider above the nearest following visible message.

## Non-goals

- No per-message read receipts. Only one marker per buffer.
- No partial acks ("mark read up to here"). Ack always moves to the newest loaded message.
- No notification badges interaction defined here (see separate behavior if/when added).

## Client parity

- **Web** (`web/src/`): divider + bar rendered from `buffer.marker_id`; `mark_read` sent only from Esc handler and bar click. No focus/scroll/entry triggers. No client-side anchor maps.
- **TUI** (`cmd/tui/`): same. Bar renders as the top line of the message viewport; mouse click on it acks; Esc acks (and keeps its pre-existing sidebar/input focus toggle). No debounce needed — acks are rare and user-initiated.
- **Apple** (`apple/`): same. Bar is the primary dismiss (tap); hardware-keyboard Esc acks where available. `markRead` must never run implicitly (no buffer-open, scenePhase, or per-message triggers). `buffer_update` updates `lastSeenID`/`markerID`/counts; rendering follows.

## Related

- `ai-docs/rest-api.md` — `/api/state` buffer fields (`last_seen_id`, `marker_id`, `unread`, `mentions`)
- `ai-docs/websocket-protocol.md` — `mark_read` command, `buffer_update` event
- `irc/semantics.go` — `counts_as_unread`, `is_self` classification
