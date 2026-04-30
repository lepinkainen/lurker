# IRCCloud parity notes

This document tracks IRCCloud-inspired UI and behavior ideas for Lurker. These are reference points, not requirements to match IRCCloud exactly.

## Per-channel options storage

Add persisted per-buffer preferences in the control DB, keyed by global buffer ID. Prefer a dedicated table rather than adding UI preference columns to the per-network log DB.

Suggested table:

```sql
CREATE TABLE buffer_settings (
  buffer_id INTEGER PRIMARY KEY REFERENCES buffer_registry(id) ON DELETE CASCADE,
  show_embeds INTEGER NOT NULL DEFAULT 1,
  show_presence_events INTEGER NOT NULL DEFAULT 1,
  collapse_presence_events INTEGER NOT NULL DEFAULT 0,
  pinned INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
```

Notes:

- `show_presence_events` covers nick changes, joins, parts, and quits where available.
- `collapse_presence_events` is only meaningful when `show_presence_events = 1`.
- Implement collapsed presence display only if it is straightforward. If not, create a low-priority GitHub issue and ship the rest without it.
- `show_embeds` is one combined setting for uploaded files, external media, text snippets, Twitter/X links, Mastodon links, Bluesky links, Reddit links, etc.; do not expose separate per-embed-type toggles for now.
- `pinned` is stored server-side so it follows the app across browsers/devices.

Suggested API shape:

- Include `settings` or flattened setting fields on each buffer in `/api/state`.
- Add a small mutating endpoint such as `PATCH /api/buffers/{id}/settings` for partial updates.
- Stream a `buffer_update` or dedicated `buffer_settings` event after settings changes so other open tabs update immediately.

## Per-channel options menu

Add an IRCCloud-style channel options menu/context panel.

### Channel actions

- Set topic
- Invite
- Leave / part
- Archive
- Ignore list
- Download logs
- Clear backlog
- Delete

### Per-channel toggles

Implement these persisted toggles first:

- Show embeds
- Show nick changes, joins and parts
  - Extra option when enabled: show collapsed, if easy
- Pin

Potential later toggles:

- Show members
- Show unread message indicator
- Mark as read automatically
- Notify on all messages
- Mute notifications
- Collapsed
- Format colours

## Pinned channels

Pinned channels should appear twice:

1. under a synthetic top-level sidebar section/network named with a pin icon, e.g. `📌 Pinned`
2. under their regular network, unchanged

The pinned section is a UI grouping only. It is not a real network and should not appear in backend network records.

Ordering/behavior:

- Place `📌 Pinned` above all real networks.
- Show pinned buffers using their normal unread/mention badges and joined/parted state.
- Selecting a pinned row activates the same underlying buffer ID as selecting it under the real network.
- Avoid duplicating message state; both rows point to the same buffer.
- For keyboard navigation, pinned buffers should have priority for Alt+A / “jump to first active channel” selection.

## Channel navigation semantics

“Active” channel navigation means channel buffers only.

- Alt+A and any next/previous active-channel commands should select only `kind = "channel"` buffers.
- Do not select network status buffers from these flows, even if they have unread messages.
- Do not select query/PM buffers from these flows unless a separate explicit “next unread conversation” command is added later.
- Pinned channels should be considered before non-pinned channels when choosing an active channel.

## Presence display behavior

When `show_presence_events` is disabled for a channel:

- Hide nick/join/part/quit-style system messages in that channel’s message view.
- Prefer leaving stored history unchanged; this is a display preference, not a logging preference.
- Apply the setting consistently to initial history and live WebSocket messages.

QUIT detail:

- IRC QUIT events do not name channels directly, but `girc` tracks remote user channel membership.
- For other users’ QUITs, Lurker can infer affected channels from tracked membership before the user is removed.
- Self-quits/disconnects should not fan out as per-channel user quit messages; the existing joined/parted state handling is separate.

If collapsed presence is implemented:

- Collapse runs of presence-only messages into a compact summary row.
- Keep regular message ordering intact.
- Expand-on-click is nice to have, not required for the first pass.

## Channel mode/status details

Display read-only channel mode/status details in the menu when available, for example `+snl 455` with descriptions for mode flags.
