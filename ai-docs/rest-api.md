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

- expose the update checker's cached comparison of the running build's commit vs the latest published release

See [operations.md](operations.md) for the full response shape and configuration.

## `GET /api/state`

Purpose:

- full snapshot for initial client hydration

Returns:

- `networks`
- `buffers`
- `initial_messages` keyed by buffer ID
- `members` keyed by buffer ID (same entry shape as the `member_list` WS event, including the IRCv3 `bot` flag)

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
  "sort_order": 0,
  "show_embeds": true,
  "show_presence_events": true,
  "collapse_presence_events": false,
  "pinned": false,
  "archived": false,
  "pin_order": 0,
  "unread": 3,
  "mentions": 1
}
```

The settings fields (`show_embeds`, `show_presence_events`, `collapse_presence_events`, `pinned`, `archived`, `pin_order`) are persisted server-side in the control DB and included in `/api/state` and streamed `buffer_settings` events. `sort_order` is the manual channel ordering position (see `POST /api/networks/{id}/buffers/reorder`); channels sort by `(sort_order, name)` — the default 0 keeps untouched sets alphabetical. Clients that don't support manual ordering may ignore it. `pin_order` is the analogous position within the global Pinned section (see `POST /api/buffers/pinned/reorder`); pinned channels sort by `(pin_order, name)`, and pinning a buffer assigns it `MAX(pin_order among pinned)+1` so new pins append. Pinned channels remain listed under their network group as well — the Pinned section is an additional view, not a move. `archived` drives the sidebar Archive section (see `ai-docs/websocket-protocol.md`); clients bucket by it, not by `joined`.

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

- store a locally uploaded image, optimize it server-side, and return its public URL plus metadata

Request:

- `multipart/form-data`
- file field name: `file` (filename required)

Response (`201 Created`):

```json
{
  "url": "https://host.tailnet.ts.net/uploads/0123456789abcdef.jpg",
  "mime": "image/jpeg",
  "width": 2048,
  "height": 1365,
  "bytes": 412873
}
```

`width`/`height`/`bytes` describe the stored (optimized) file, not the upload.

Server-side optimization (`media/transcode.go`, pure Go, CGO off):

- JPEG/WebP: decoded, downscaled so the long edge is ≤ `2048px` (never upscaled), re-encoded as JPEG q82. Stored as `.jpg`.
- PNG/GIF: passed through unchanged (re-encoding a GIF would flatten animation). Stored as `.png` / `.gif`.
- The stored key is a random 10-char base62 id + the extension of the *stored* format (from the optimizer), not the client-supplied name.

Validation / errors:

- Content-type is sniffed via `http.DetectContentType`. `video/mp4` and `video/quicktime` are classified but return `415` (video path is a reserved scaffold, not implemented). Non-image, HEIC, and unrecognized payloads fail the decode and return `415`. WebP is not reliably sniffed, so the real gate is the decode attempt in `optimizeImage`, not the sniff.
- Decompression-bomb guard: decoded area > `50 MP` returns `415`.
- Over the size limit returns `413`.
- No storage backend configured (no `media:` block in `config.yaml`) returns `404`.
- Storage backend configured but failing (bad credentials, bucket gone, network down) returns `502`, and nothing is recorded — no metadata row and no stored object. A half-published upload is never left behind, and the bytes are never diverted to another backend.

Storage backend:

The backend is named explicitly in the `media:` block and there is **no fallback between backends** (see [operations.md](operations.md#media-storage) and `S3_SETUP.md`):

- `backend: s3` — variants are PUT to an S3-compatible bucket with `Cache-Control: public, max-age=31536000, immutable`; nothing is written to local disk, so `GET /uploads/{name}` serves nothing.
- `backend: disk` — variants are written under `media.disk.dir` and served by `GET /uploads/{name}`.
- block absent — uploads disabled (`404` above).

Returned URL:

- With `backend: s3` the URL is always `{media.s3.public_base_url}[/{prefix}]/{name}` — the CDN domain, since the bytes are only reachable there.
- With `backend: disk`, `media.disk.base_url` wins when set: `{base}/{name}`. Otherwise the URL is made **absolute** from the incoming request (`scheme://host/uploads/{name}`) so the pasted link resolves for other IRC clients — a relative path is useless once it leaves this origin. Scheme honors `X-Forwarded-Proto` (only `http`/`https` accepted; a spoofed scheme like `javascript` is ignored) then the connection's TLS state. If the `Host` header is untrustworthy (spaces, CRLF, quotes) it falls back to a relative `/uploads/{name}`.

Clients (web drag&drop, macOS/iOS paperclip + drag&drop) upload here and auto-insert the returned URL into the composer, ready to send.

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

## `POST /api/networks/{id}/buffers/reorder`

Purpose:

- persist manual channel ordering within a network's sidebar section

Request body:

```json
{ "ids": ["0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1", "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20"] }
```

Behavior:

- IDs must be **channel** buffers belonging to the network — a **partial** set is allowed (unlike network reorder)
- listed IDs form the ordered prefix (`sort_order = 0..n-1`); channels omitted from the request are renumbered onto the **tail**, keeping their previous `(sort_order, name)` relative order
- the whole channel set is rewritten transactionally, so `sort_order` stays dense and collision-free — a partial set can never leave two channels sharing a position
- this is what makes a client safe to submit only the channels it renders: an archived channel omitted from the request lands at the end rather than reappearing mid-list on a stale position when unarchived
- 404 unknown network; 400 on empty list, duplicates, foreign IDs, or non-channel kinds
- response is the `buffer_reorder` event shape (also broadcast to all WebSocket clients), carrying **all** channel buffers of the network:

```json
{
  "type": "buffer_reorder",
  "network_id": "...",
  "buffers": [{ "id": "...", "sort_order": 0 }, { "id": "...", "sort_order": 1 }]
}
```

Channels display-sort by `(sort_order, name)`; queries and archived groups stay alphabetical. The Pinned section has its own ordering (`POST /api/buffers/pinned/reorder`).

## `POST /api/buffers/pinned/reorder`

Purpose:

- persist manual ordering of the sidebar's global Pinned section

Request body:

```json
{ "ids": ["0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1", "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20"] }
```

Behavior:

- IDs must be currently **pinned** buffers (any network) — a **partial** set is allowed
- listed IDs form the ordered prefix (`pin_order = 0..n-1`); pinned buffers omitted from the request are renumbered onto the **tail**, keeping their previous `(pin_order, name)` relative order — same dense, collision-free guarantee as channel reorder
- 400 on empty list, duplicates, or unpinned/unknown IDs
- response is the `pinned_reorder` event shape (also broadcast to all WebSocket clients), carrying **all** pinned buffers:

```json
{
  "type": "pinned_reorder",
  "buffers": [{ "id": "...", "pin_order": 0 }, { "id": "...", "pin_order": 1 }]
}
```

Pinned channels display-sort by `(pin_order, name)`. Unpinning resets `pin_order` to 0; re-pinning appends to the end of the section.

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
  "pinned": false,
  "archived": false
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
  "pinned": false,
  "archived": false,
  "pin_order": 0
}
```

The same event is published to the hub so other connected WebSocket clients stay in sync. Settings are supported for channel and query buffers; status buffers return 400. `archived` can also be toggled over WS via `archive_buffer`/`unarchive_buffer`, and the IRC runtime maintains it automatically for channels (set on self-part/kick, cleared on self-join).

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

Serves previously uploaded files from local disk by their generated filename (base62 id + extension).

Only active under `media.backend: disk`. With `backend: s3` there is no local copy, so every request here is a `404` and clients fetch from the CDN URL returned by `POST /api/upload` instead.

## Preview attachment on reads

`/api/state` and `/api/buffers/{id}/history` attach URL previews inline on each `message` under a `previews` field via `Server.attachPreviews` — groups messages by network, reads `message_previews` from each log DB, batch-loads URL rows from `previews.db` in one `GetMany`. No network calls on the read path. See [irc-runtime.md](irc-runtime.md) for the pipeline that populates these.
