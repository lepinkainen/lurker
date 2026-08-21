# WebSocket protocol

Endpoint:

- `/api/stream`

The stream serves two roles:

- server-to-client event stream
- client-to-server command channel

See [rest-api.md](rest-api.md) for the REST surface and [irc-runtime.md](irc-runtime.md) for how server events are produced.

## Client commands

Current client command envelope fields:

- `type`
- `req_id`
- `buffer_id` — UUIDv7 string
- `network_id` — UUIDv7 string
- `channel`
- `target` — nick or channel for commands that target a user (`msg`, `whois`, `invite`, `kick`, `notice`, `ctcp`, `query`, `op`, `deop`, `voice`, `devoice`, `ban`, `unban`, `kickban`, `ignore`, `unignore`, `mute`, `unmute`)
- `content`
- `before` — UUIDv7 message ID string for history pagination
- `limit`
- `message_id` — UUIDv7 string

### Supported command types

#### Core messaging

- `send` — send a PRIVMSG to a non-status buffer (uses `buffer_id` + `content`)
- `msg` — send a PRIVMSG to a specific nick (uses `network_id` + `target` + `content`)
- `me` — send a CTCP ACTION to a buffer (uses `buffer_id` + `content`)
- `notice` — send a NOTICE to a nick (uses `network_id` + `target` + `content`)
- `ctcp` — send a raw CTCP request (`network_id` + `target`, `content` = `COMMAND [args]`)

#### Channel management

- `join` — join a channel on a network (uses `network_id` + `channel`)
- `part` — part the channel identified by buffer ID (uses `buffer_id`, optional `content` for part reason)
- `rejoin` — rejoin a previously parted channel (uses `buffer_id`)
- `topic` — set channel topic (uses `buffer_id` + `content`)
- `invite` — invite a nick to a channel (uses `network_id` + `target` + `channel`)
- `kick` — kick a nick from a channel (uses `buffer_id` + `target`, optional `content` for reason)
- `kickban` — ban and kick a nick (uses `buffer_id` + `target`, optional `content` for reason)

#### Channel modes

- `mode` — set arbitrary channel modes (uses `buffer_id` + `content` = `[+/-]modes [params...]`)
- `op` / `deop` — grant/revoke op on a channel (uses `buffer_id` + `target`)
- `voice` / `devoice` — grant/revoke voice on a channel (uses `buffer_id` + `target`)
- `ban` / `unban` — set/remove a ban mask on a channel (uses `buffer_id` + `target`)
- `banlist` — list bans on a channel (uses `buffer_id`)

#### User-level IRC commands

- `nick` — change nick on a network (uses `network_id` + `content`)
- `whois` — query WHOIS for a nick (uses `network_id` + `target`)
- `away` — mark self as away (uses `network_id`, optional `content` for away message)
- `back` — mark self as back from away (uses `network_id`)
- `quit` — disconnect from IRC with optional quit message (uses `network_id`, optional `content`)
- `raw` — send a raw IRC line (uses `network_id` + `content`)
- `list` — request channel LIST from server (uses `network_id`, optional `content` for filter)

#### Query management

- `query` — open a new query buffer for a nick (uses `network_id` + `target`)

#### Buffer lifecycle

- `archive_buffer` / `unarchive_buffer` — set/clear the persisted `archived` flag on a channel or query buffer (uses `buffer_id`; status buffers are rejected). Broadcasts a `buffer_settings` event. Channels are normally archived implicitly by part/kick and unarchived by join — this is the manual path (queries, `/archive`, `/unarchive`). Allowed on non-IRC networks.
- `delete_buffer` — permanently delete a buffer and its entire message history (uses `buffer_id`). Only **archived** buffers may be deleted; status buffers never. Errors: missing `buffer_id`, unknown buffer, status kind, not archived. On success the server acks and broadcasts `buffer_deleted`. Allowed on non-IRC networks.

#### History and state

- `history` — fetch recent or older messages for a buffer
- `mark_read` — persist last seen message for a buffer (`buffer_id` + `message_id`). Sent **only** on explicit user ack (Esc / unread-bar activation) — never implicitly on buffer switch, scroll, or focus (see `ai-docs/behaviors/new-messages-marker.md`). The server validates that `message_id` exists in that buffer (error otherwise) and never regresses the position: a stale ack is acked as a no-op and the `buffer_update` echo carries the newer effective `last_seen_id`, so racing clients converge forward (max-wins)

#### Ignore management

Two-tier ignore: `hide` masks are dropped before storage (never persisted, never shown); `mute` masks are stored and shown normally but excluded from unread counts and the "New messages" marker — mentions/highlights from a muted sender still count. `CreateIgnore` upserts by `(network_id, mask)`, so re-adding an existing mask at a different level promotes/demotes it rather than erroring.

- `ignore` — add a **hide**-tier ignore mask to a network (uses `network_id` + `target`)
- `unignore` — remove an ignore mask from a network, regardless of level (uses `network_id` + `target`)
- `ignorelist` — list all ignore entries (mask + level) for a network (uses `network_id`)
- `mute` — add a **mute**-tier ignore mask to a network (uses `network_id` + `target`)
- `unmute` — remove an ignore mask from a network; identical to `unignore` since mask removal is level-agnostic (uses `network_id` + `target`)
- `mutelist` — alias for `ignorelist`; same combined hide+mute list (uses `network_id`)

## Generic command responses

Ack envelope:

```json
{ "type": "ack", "req_id": "r1" }
```

Error envelope:

```json
{ "type": "error", "req_id": "r1", "message": "..." }
```

## Command-specific response types

`history_result` — response to `history` command:

```json
{
  "type": "history_result",
  "req_id": "r1",
  "buffer_id": "...",
  "messages": [ /* messageDTO[] */ ]
}
```

`ignorelist_result` — response to both `ignorelist` and `mutelist` commands. `entries` carries every configured mask for the network with its level (`hide` or `mute`):

```json
{
  "type": "ignorelist_result",
  "req_id": "r1",
  "network_id": "...",
  "entries": [
    { "mask": "*!*@spam.example.com", "level": "hide" },
    { "mask": "weatherbot", "level": "mute" }
  ]
}
```

## Stream event types

Currently published events:

- `message` — inbound or locally-logged outbound message
- `buffer_created` — first-time buffer registration
- `buffer_deleted` — buffer and its history permanently deleted
- `buffer_update` — topic, topic setter/set-time, joined/archived state, or last-seen-ID changes
- `network_state` — connection state transitions
- `member_list` — full channel member list snapshot
- `preview` — URL previews ready for a message
- `presence` — lightweight join/part/quit/kick/nick-change events
- `avatar` — a user's IRCv3 metadata avatar appeared, changed, or cleared
- `buffer_settings` — per-buffer display preferences changed
- `buffer_reorder` — manual channel ordering changed for one network
- `pinned_reorder` — manual ordering of the global Pinned section changed
- `channel_list` — streaming /LIST results
- `netsplit` — retroactive netsplit annotation for already-published messages
- `highlights` — global highlight pattern list changed (`{patterns: [...]}`); matching itself stays server-side, the event only lets open settings UIs refresh
- `history_backfill` — `{network_id, buffer_id, count}`: a CHATHISTORY replay inserted `count` older messages into the buffer (no per-message `message` events are sent for replays). Clients with the buffer loaded refetch its recent window (`history` command without `before`) and merge by id; the recovered rows also feed unread/marker bookkeeping. Buffers not yet loaded see the rows on their normal first load

Important event shapes:

`message`

- `id`
- `network_id`
- `buffer_id`
- `msgid`
- `ts`
- `sender`
- `account`
- `kind`
- `target`
- `content`
- `display_kind`, `is_self`, `mentions_me`, `counts_as_unread` — server-computed semantics (`irc.ComputeMessageSemantics`); clients consume verbatim. Server-originated messages (`sender` containing `.` or `:`, e.g. a server hostname on numerics like the 001 welcome) never set `mentions_me`, even if the content embeds the user's nick
- `highlight`, `highlight_pattern` — set when `content` matches a user-defined highlight pattern (`irc/highlights.go`, configured via `PUT /api/settings/highlights`); word-boundary case-insensitive matching, self-authored messages never highlight, nor do server-originated messages (see `mentions_me` above). Clients treat `highlight` like `mentions_me` for badges/styling but can distinguish the two
- `sender_color` — nick-color palette index for `sender` (Go `nickcolor` package; omitted when no sender)
- `target_color` — palette index for `target` when it is a nick (`kick`/`nick` kinds only)
- `netsplit` — `{id, server_a, server_b}` on quit/join messages belonging to a collapsed netsplit group (server-side clustering, `irc/netsplit_tracker.go`)
- `segments` — parsed mIRC formatting of `content` as `[{text, bold?, italic?, underline?, strike?, mono?, fg?, bg?}]` (Go `mirc` package); omitted for plain content — clients render `content` directly then

`buffer_created`

- `id`
- `network_id`
- `name`
- `kind`
- `created_at`
- `sort_order` — 0 while the network's channel order is untouched; MAX+1 once any channel has a manual position, so new channels append to the end of the user's order

`buffer_deleted`

- `id`
- `network_id`

Broadcast after a successful `delete_buffer`. Clients drop the buffer and all per-buffer state (messages, members, unread/mention counts, marker); if it was the active buffer, they reselect using their startup fallback order. Deletion is never optimistic — this event drives the state change.

`buffer_update`

Partial update: every field below except `id`/`network_id` is optional, and an absent field means "unchanged" — clients must only apply keys present in the JSON. A present-but-empty `topic` means the topic was cleared.

Two server-side variants share this type:

- **topic/joined variant** (IRC runtime): `topic`, `topic_set_by`, `topic_set_at`, `joined`, `archived` — never carries read-state fields.
- **mark_read echo** (broadcast to all clients, sender included): `last_seen_id`, `marker_id`, `marker_ts`, `unread`, `mentions` — never carries topic/joined. `marker_id` is **always present** on this variant; JSON `null` means the buffer is caught up and clients must drop the "New messages" marker/bar/badges.

- `id`
- `network_id`
- `topic`
- `topic_set_by` — nick that set the topic (from RPL_TOPICWHOTIME 333 or a live TOPIC change)
- `topic_set_at` — when the topic was set, storage timestamp format (`2006-01-02T15:04:05.000Z`)
- `joined` — never sent on topic-only updates: a topic reply (332/333) is not proof of membership
- `archived` — persisted archive flag; sent when it changes. Self-part/kick sets it, self-join clears it, a new message to an archived query clears it. Disconnects never archive (only real membership intent does), so reconnects don't shuffle the sidebar
- `last_seen_id`
- `marker_id` — server-derived "New messages" marker: id of the oldest unread message that counts (self-authored and presence/system kinds never count or anchor)
- `marker_ts` — RFC3339 timestamp of the marker message (from its UUIDv7), for "new since HH:MM" display when the message isn't loaded client-side; omitted when `marker_id` is null
- `unread`, `mentions` — server-recomputed counts past `last_seen_id` (capped at 1000, self-suppressed)

`network_state`

- `network_id`
- `state` — `"connecting"`, `"connected"`, or `"disconnected"`

`member_list`

- `network_id`
- `buffer_id`
- `channel`
- `members` — array of `{nick, prefix?, realname?, away, self, bot, color, has_avatar?}`; `realname` is pre-stripped of mIRC codes server-side, `color` is the nick-color palette index, `bot` is IRCv3 bot mode (see [irc-runtime.md](irc-runtime.md)), `has_avatar` (omitted when false) means the user published an IRCv3 metadata `avatar` and the client should render `/api/avatar` instead of the identicon (see [rest-api.md](rest-api.md), [irc-runtime.md](irc-runtime.md))

`preview`

- `message_id`
- `network_id`
- `buffer_id`
- `previews` — array of resolved preview objects (`url`, `kind`, `title?`, `description?`, `image_url?`, `site_name?`, `width?`, `height?`, `mime?`)

Only previews with `kind` = `image` or `opengraph` are published. Negative results (`none`, `error`) are cached server-side but never pushed to clients. `/api/state` and history responses attach the same preview shape inline on each `message` under a `previews` field so reloads don't need a follow-up event.

`presence`

- `network_id`
- `buffer_id` — present for channel-scoped events (join, part, quit, kick)
- `nick` — affected nick
- `state` — `"join"`, `"part"`, `"quit"`, `"kick"`, or `"nick"`
- `target` — new nick when `state` = `"nick"`

`avatar`

- `network_id`
- `nick` — affected nick
- `has_avatar` — `true` when the user's IRCv3 metadata `avatar` key was set/changed, `false` when cleared. Carries no URL: the client flips a per-`(network, nick)` flag and (re)points the nick icon at `/api/avatar?network=&nick=` (see [rest-api.md](rest-api.md)). Published only on an actual state transition, so repeated SYNC/GET pushes of an unchanged value are suppressed server-side

`buffer_settings`

- `id` — buffer UUID
- `show_embeds`
- `show_presence_events`
- `collapse_presence_events`
- `pinned`
- `archived` — clients bucket sidebar sections by this flag (not by `joined`): non-status buffers with `archived` render in the per-network folded "Archive" section
- `pin_order` — position within the Pinned section; pinning assigns MAX+1 so new pins append, unpinning resets to 0. Pinned channels display-sort by `(pin_order, name)` and stay listed under their network group as well

`buffer_reorder`

- `network_id`
- `buffers` — `[{id, sort_order}, ...]` covering **all** channel buffers of the network after a `POST /api/networks/{id}/buffers/reorder`. Channels display-sort by `(sort_order, name)`; clients without manual ordering ignore the event.

`pinned_reorder`

- `buffers` — `[{id, pin_order}, ...]` covering **all** pinned buffers after a `POST /api/buffers/pinned/reorder`. Pinned channels display-sort by `(pin_order, name)`; clients without manual pinned ordering ignore the event.

`netsplit`

- `network_id`
- `buffer_id`
- `netsplit` — `{id, server_a, server_b}`
- `message_ids` — earlier quit messages published before the cluster reached the netsplit threshold; clients patch these messages with the annotation and re-render

Netsplit clustering is fully server-side: a cluster forms from ≥2 quits sharing a hostname-pair reason within 60s, joins by the same nicks within 30min are matched as rejoins, and every member message carries the shared `netsplit.id`. Clients group by id only — they never parse quit reasons or match nicks. The `buffer_update` `topic` field is stripped of mIRC codes before publish (DB keeps the raw topic).

`channel_list`

- `network_id`
- `entries` — array of `{name, count, topic?}`
- `done` — `true` on the final event, closing the LIST stream
