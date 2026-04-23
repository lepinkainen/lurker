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
- `buffer_id`
- `network_id`
- `channel`
- `content`
- `before`
- `limit`
- `message_id`

Supported command types:

- `send`
- `history`
- `join`
- `part`
- `mark_read`

Command semantics:

- `send`: send message to a non-status buffer
- `history`: fetch recent or older messages for a buffer
- `join`: join a channel on a network
- `part`: part the channel identified by buffer ID
- `mark_read`: persist last seen message for a buffer

## Generic command responses

Ack envelope:

```json
{ "type": "ack", "req_id": "r1" }
```

Error envelope:

```json
{ "type": "error", "req_id": "r1", "message": "..." }
```

## Stream event types

Currently published events include:

- `message`
- `buffer_created`
- `buffer_update`
- `network_state`
- `member_list`
- `preview`
- lightweight presence-style events may also be published

Important event shapes from `irc/handler.go`:

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
- `state`

`member_list`

- `network_id`
- `buffer_id`
- `channel`
- `members`

`preview`

- `message_id`
- `network_id`
- `buffer_id`
- `previews` — array of resolved preview objects (`url`, `kind`, `title?`, `description?`, `image_url?`, `site_name?`, `width?`, `height?`, `mime?`)

Only previews with `kind` = `image` or `opengraph` are published. Negative results (`none`, `error`) are cached server-side but never pushed to clients. `/api/state` and history responses attach the same preview shape inline on each `message` under a `previews` field so reloads don't need a follow-up event.
