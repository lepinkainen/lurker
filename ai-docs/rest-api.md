# REST API

Base routes currently exposed:

- `GET /health`
- `GET /healthz`
- `GET /whoami`
- `GET /api/state`
- `GET /api/buffers/{id}/history`
- `GET /api/search?q=...`
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
{ "ids": [2, 1, 3] }
```

Behavior:

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
