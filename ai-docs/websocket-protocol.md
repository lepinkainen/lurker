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
- `target` — nick or channel for commands that target a user (`msg`, `whois`, `invite`, `kick`, `notice`, `ctcp`, `query`, `op`, `deop`, `voice`, `devoice`, `ban`, `unban`, `kickban`, `ignore`, `unignore`)
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

#### History and state

- `history` — fetch recent or older messages for a buffer
- `mark_read` — persist last seen message for a buffer

#### Ignore management

- `ignore` — add an ignore mask to a network (uses `network_id` + `target`)
- `unignore` — remove an ignore mask from a network (uses `network_id` + `target`)
- `ignorelist` — list all ignore masks for a network (uses `network_id`)

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

`ignorelist_result` — response to `ignorelist` command:

```json
{
  "type": "ignorelist_result",
  "req_id": "r1",
  "network_id": "...",
  "masks": ["*!*@spam.example.com"]
}
```

## Stream event types

Currently published events:

- `message` — inbound or locally-logged outbound message
- `buffer_created` — first-time buffer registration
- `buffer_update` — topic, joined state, or last-seen-ID changes
- `network_state` — connection state transitions
- `member_list` — full channel member list snapshot
- `preview` — URL previews ready for a message
- `presence` — lightweight join/part/quit/kick/nick-change events
- `buffer_settings` — per-buffer display preferences changed
- `channel_list` — streaming /LIST results
- `netsplit` — retroactive netsplit annotation for already-published messages

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
- `display_kind`, `is_self`, `mentions_me`, `counts_as_unread` — server-computed semantics (`irc.ComputeMessageSemantics`); clients consume verbatim
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

`buffer_update`

- `id`
- `network_id`
- `topic`
- `joined`
- `last_seen_id`

`network_state`

- `network_id`
- `state` — `"connecting"`, `"connected"`, or `"disconnected"`

`member_list`

- `network_id`
- `buffer_id`
- `channel`
- `members` — array of `{nick, prefix?, realname?, away, self, color}`; `realname` is pre-stripped of mIRC codes server-side, `color` is the nick-color palette index

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

`buffer_settings`

- `id` — buffer UUID
- `show_embeds`
- `show_presence_events`
- `collapse_presence_events`
- `pinned`

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
