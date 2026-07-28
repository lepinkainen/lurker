# REST API

Base routes currently exposed:

- `GET /health`
- `GET /healthz`
- `GET /whoami`
- `GET /api/themes`
- `GET /api/state`
- `GET /api/update-status`
- `GET /api/buffers/{id}/history`
- `PATCH /api/buffers/{id}/settings`
- `GET /api/search?q=...`
- `GET /api/stream`
- `POST /api/upload`
- `POST /api/networks`
- `POST /api/networks/reorder`
- `PATCH /api/networks/{id}`
- `DELETE /api/networks/{id}`
- `GET /api/networks/{id}/connect-commands`
- `PUT /api/networks/{id}/connect-commands`
- `POST /api/networks/{id}/connect`
- `POST /api/networks/{id}/disconnect`
- `GET /api/settings/highlights`
- `PUT /api/settings/highlights`
- `GET /api/config/yaml/preview`
- `POST /api/config/yaml/save`
- `GET /uploads/{name}`

See [websocket-protocol.md](websocket-protocol.md) for the live-event channel that complements these endpoints.

## `GET /whoami`

Returns service metadata such as:

- app name
- version
- git hash
- build time

Useful for identifying the running instance.

## `GET /api/themes`

Purpose:

- list available CSS themes for the web UI

Response:

```json
{ "themes": ["default", "dark", "solarized"] }
```

Returns an empty array if no themes are configured.

## `GET /api/update-status`

Purpose:

- expose the update checker's cached comparison of local vs remote container image

See [operations.md](operations.md) for the full response shape and configuration.

## `GET /api/state`

Purpose:

- full snapshot for initial client hydration

Returns:

- `networks`
- `buffers`
- `initial_messages` keyed by buffer ID
- `members` keyed by buffer ID

Current behavior:

- includes recent messages for each buffer
- includes network `sort_order`
- network list order is the server-side canonical order

### `networkDTO` shape

```json
{
  "id": "...",
  "name": "libera",
  "host": "irc.libera.chat",
  "port": 6697,
  "tls": true,
  "nick": "mynick",
  "nick_color": 27,
  "realname": "",
  "status": "connected",
  "sort_order": 0,
  "disabled": false,
  "in_config": true
}
```

`nick_color` is the server-computed nick-color palette index for the configured nick (Go `nickcolor` package; omitted when nick is empty). SASL credentials are never shipped to clients. The `disabled` field indicates whether the network is paused; disabled networks do not auto-connect on startup.

`in_config` reports whether the network is defined in `config.yaml` (case-insensitive name match). Config is the source of truth at boot: networks with `in_config: false` are marked disabled on the next backend restart, and runtime changes (including the `disabled` toggle) on `in_config: true` networks are overwritten from YAML. UIs should surface this so users know which changes are ephemeral.

### `bufferDTO` shape

```json
{
  "id": "...",
  "network_id": "...",
  "name": "#channel",
  "kind": "channel",
  "topic": "...",
  "topic_set_by": "nick",
  "topic_set_at": "2026-01-01T00:00:00.000Z",
  "joined": true,
  "last_seen_id": "...",
  "marker_id": "...",
  "marker_ts": "2026-01-01T00:00:00Z",
  "created_at": "2026-01-01T00:00:00Z",
  "show_embeds": true,
  "show_presence_events": true,
  "collapse_presence_events": false,
  "pinned": false,
  "unread": 3,
  "mentions": 1
}
```

The settings fields (`show_embeds`, `show_presence_events`, `collapse_presence_events`, `pinned`) are persisted server-side in the control DB and included in `/api/state` and streamed `buffer_settings` events.

Read state is server-derived (see `ai-docs/behaviors/new-messages-marker.md`): `unread`/`mentions` count messages past `last_seen_id` (capped at 1000; self-authored and presence/system kinds excluded). `marker_id` is the "New messages" marker — the oldest counting unread message — with `marker_ts` its RFC3339 timestamp (derived from the UUIDv7) for "new since" display; both omitted when the buffer is caught up.

### `messageDTO` shape

```json
{
  "id": "...",
  "network_id": "...",
  "buffer_id": "...",
  "msgid": "...",
  "ts": "2026-01-01T00:00:00.000Z",
  "sender": "nick",
  "account": "nick!user@host",
  "kind": "privmsg",
  "target": "#channel",
  "content": "hello",
  "previews": [],
  "display_kind": "message",
  "sender_color": 19,
  "segments": [{ "text": "hello", "bold": true }],
  "netsplit": { "id": "...", "server_a": "a.net", "server_b": "b.net" }
}
```

The `target` field records the IRC target (channel or nick) the message was addressed to. The `previews` array is populated by `attachPreviews` (see Preview attachment section below).

Server-computed display fields (shared with the WS `message` event, see `websocket-protocol.md`): `display_kind`/`is_self`/`mentions_me`/`counts_as_unread` semantics, `highlight`/`highlight_pattern` custom-highlight flags, `sender_color`/`target_color` nick-color palette indexes, `segments` (parsed mIRC formatting; omitted for plain content), and `netsplit` (collapsed-netsplit group annotation on quit/join messages, recomputed for history/state batches by `annotateNetsplits`). The `topic` field on `bufferDTO` is stripped of mIRC codes at the API boundary; the DB keeps the raw topic. `topic_set_by`/`topic_set_at` (optional, omitted when unknown) record who set the topic and when, populated from RPL_TOPICWHOTIME (333) on join and from live TOPIC changes; they ride alongside `topic` on both `bufferDTO` and the WS `buffer_update` event.

## `GET /api/buffers/{id}/history`

Purpose:

- page older messages for one global buffer ID

Query parameters:

- `before`
- `limit`

## `GET /api/search`

Query parameters:

- `q` required
- `network` optional
- `buffer` optional

Search runs against stored messages, not live IRC state.

## `POST /api/upload`

Purpose:

- store a locally uploaded file and return its public URL

Request:

- `multipart/form-data`
- file field name: `file`

Response:

```json
{ "url": "/uploads/0123456789abcdef.png" }
```

Notes:

- max size is controlled by server upload config
- when `UPLOAD_BASE_URL` is set, the returned URL uses that base instead of `/uploads/...`
- files are still stored locally under the configured upload directory

## `POST /api/networks`

Creates a network row in control DB and opens its log DB.

Expected fields include:

- `name`
- `host`
- `port`
- `tls`
- `nick`
- optional `realname`
- optional SASL fields
- optional `connect_commands` array of raw IRC lines

New networks are appended to the end of sidebar order by assigning the next `sort_order`.

## `POST /api/networks/reorder`

Purpose:

- persist sidebar network ordering

Request body:

```json
{ "ids": ["0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1", "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20"] }
```

Behavior:

- IDs are UUIDv7 strings
- expects a complete ordered list of all network IDs
- updates `sort_order` transactionally
- returns the reordered `networks` list

## `PATCH /api/networks/{id}`

Updates network properties.

Notes:

- patch semantics are partial for the existing editable fields
- optional `connect_commands` replaces the saved raw command list
- if the network name changes, the backend renames the log DB file conservatively

## Network connect commands

Explicit secret-bearing endpoints:

- `GET /api/networks/{id}/connect-commands`
- `PUT /api/networks/{id}/connect-commands`

Payload:

```json
{ "commands": ["PRIVMSG NickServ :IDENTIFY hunter2", "MODE mynick +x"] }
```

Blank lines are ignored and commands are sent after IRC registration, before autojoin. These commands are intentionally not included in `GET /api/state`.

## `DELETE /api/networks/{id}`

Behavior:

- stops the IRC runtime if running
- removes the network from control DB
- closes the log DB handle
- retains the on-disk log DB file

## Connect/disconnect routes

- `POST /api/networks/{id}/connect`
- `POST /api/networks/{id}/disconnect`

These control IRC runtime state independently of the bootstrap YAML.

## Highlight patterns

Global (all networks) user-defined highlight words:

- `GET /api/settings/highlights`
- `PUT /api/settings/highlights`

Payload (both directions):

```json
{ "patterns": ["deploy", "lurker"] }
```

PUT replaces the whole list. Validation: entries are trimmed, blanks dropped, case-insensitive duplicates removed; max 100 patterns, 64 chars each (400 otherwise). On success the server stores the list in the control DB `highlights` table, swaps the live matcher (`irc.SetHighlightPatterns`) so subsequent messages get `highlight`/`highlight_pattern` flags immediately, and publishes a `highlights` WS event. Matching is case-insensitive on word boundaries (literal words, not regex), skips self-authored messages, and a highlight counts toward `bufferDTO.mentions` like a nick mention. History recomputes flags on read, so new patterns retroactively highlight old messages.

## `PATCH /api/buffers/{id}/settings`

Purpose:

- partially update per-buffer display preferences persisted server-side

Request body (all fields optional):

```json
{
  "show_embeds": true,
  "show_presence_events": true,
  "collapse_presence_events": false,
  "pinned": false
}
```

Response is the full `buffer_settings` event shape:

```json
{
  "type": "buffer_settings",
  "id": "...",
  "show_embeds": true,
  "show_presence_events": true,
  "collapse_presence_events": false,
  "pinned": false
}
```

The same event is published to the hub so other connected WebSocket clients stay in sync. Settings are only supported for channel buffers (`kind = "channel"`).

## Config YAML endpoints

- `GET /api/config/yaml/preview` — returns the current and proposed `config.yaml` content for review before saving.
- `POST /api/config/yaml/save` — writes a new `config.yaml` to disk.

These endpoints are only available when the config exporter is configured at startup. If not configured they return `501 Not Implemented`.

### `GET /api/config/yaml/preview`

Response:

```json
{
  "current": "...",
  "proposed": "..."
}
```

### `POST /api/config/yaml/save`

Request body:

```json
{ "content": "# new config.yaml content" }
```

Response:

```json
{ "ok": true }
```

## `GET /uploads/{name}`

Serves previously uploaded files by their generated filename. Valid names are alphanumeric hex strings with an optional lowercase extension.

Note: `POST /api/upload` returns the full URL path (e.g. `/uploads/0123456789abcdef.png`) which is served by this route.

## Preview attachment on reads

`/api/state` and `/api/buffers/{id}/history` attach URL previews inline on each `message` under a `previews` field via `Server.attachPreviews` — groups messages by network, reads `message_previews` from each log DB, batch-loads URL rows from `previews.db` in one `GetMany`. No network calls on the read path. See [irc-runtime.md](irc-runtime.md) for the pipeline that populates these.
