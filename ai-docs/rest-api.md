# REST API

Base routes currently exposed:

- `GET /health`
- `GET /healthz`
- `GET /whoami`
- `GET /api/state`
- `GET /api/buffers/{id}/history`
- `GET /api/search?q=...`
- `POST /api/upload`
- `POST /api/networks`
- `POST /api/networks/reorder`
- `PATCH /api/networks/{id}`
- `DELETE /api/networks/{id}`
- `POST /api/networks/{id}/connect`
- `POST /api/networks/{id}/disconnect`

See [websocket-protocol.md](websocket-protocol.md) for the live-event channel that complements these endpoints.

## `GET /whoami`

Returns service metadata such as:

- app name
- version
- git hash
- build time

Useful for identifying the running instance.

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
- if the network name changes, the backend renames the log DB file conservatively

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

## Preview attachment on reads

`/api/state` and `/api/buffers/{id}/history` attach URL previews inline on each `message` under a `previews` field via `Server.attachPreviews` — groups messages by network, reads `message_previews` from each log DB, batch-loads URL rows from `previews.db` in one `GetMany`. No network calls on the read path. See [irc-runtime.md](irc-runtime.md) for the pipeline that populates these.
