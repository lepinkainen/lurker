# Data source: Bluesky

This is an **MVP read-only data-source plan**. It describes how to surface a Bluesky account's home timeline, notifications, and optional list/feed views inside Lurker as a fake IRC "network" with channel buffers. See [ARCHITECTURE.md](ARCHITECTURE.md) for context, [storage.md](storage.md) for the buffer/message model, and [websocket-protocol.md](websocket-protocol.md) for the wire shapes the frontend already understands.

For the cross-cutting `DataSource` abstraction shared by all non-IRC sources, see [datasource-mastodon.md](datasource-mastodon.md). This document only covers the Bluesky-specific parts plus Bluesky-driven implementation notes.

## Scope

- **Read-only**. Posting, liking, reposting, replying — all out of scope.
- **Hard-coded network**. Configured in `config.yaml`, never created via REST.
- **Best-effort fidelity**. Avatars, image galleries, threading, like counts, repost markers — dropped or collapsed to plain text in the first cut. URL/image links flow through the shared preview pipeline, so OG cards and inline images work through the same path as IRC messages.

## Mapping to Lurker concepts

| Bluesky concept | Lurker concept |
|---|---|
| Account | One row in `networks` (`kind="bluesky"`). Synthetic `host=<pds-host>`, `port=0`, `tls=false`, `nick=<identifier>`. |
| Home timeline (`app.bsky.feed.getTimeline`) | One channel buffer named `#home`. |
| Notifications (`app.bsky.notification.listNotifications`) | One channel buffer named `#notifications`. |
| Saved/list feed (`app.bsky.feed.getListFeed`, `app.bsky.feed.getFeed`) | Optional channel buffer per configured feed/list URI, named `#feed:<short-name>`. |
| Post (`app.bsky.feed.defs#feedViewPost`) | One row via `datasource.IngestPost` → `db.InsertLogMessage` + `MessageEvent`. |
| `post.author.handle` | `MessageEvent.Sender`. |
| `post.author.did` | `MessageEvent.Account`. |
| `record.text` (+ extracted embed URLs joined with space) | `MessageEvent.Content`. |
| `post.uri` | `MessageEvent.MsgID` / `datasource.Post.ExternalID` (dedupe key). |
| `post.indexedAt` | `MessageEvent.TS`. |
| Reposts | Sender is original `post.author.handle`; `Content` is prefixed with `[RT by <reposter>] `. |
| Replies | Top-level only in `#home`. Append a parent post link when available; do not render a thread tree in MVP. |

`MessageEvent.Kind` is set to `"privmsg"` for posts. Status/system messages such as auth failure, rate limiting, or repeated refresh failure go to the status buffer with `Kind="notice"`.

Important preview note: datasource messages do **not** pass through `irc.handler.storeEvent`, so `datasource.IngestPost` must enqueue previews directly after storing and publishing a message. Do not rely on the IRC-only preview hook.

## Authentication

Use **App Passwords**, not the account password. Generated at <https://bsky.app/settings/app-passwords>.

- Endpoint: `com.atproto.server.createSession` on the user's PDS (default `https://bsky.social`).
- Request body: `{ "identifier": "<handle-or-did>", "password": "<app-password>" }`.
- Response carries `accessJwt` (~2 h TTL) and `refreshJwt` (longer TTL).
- Refresh via `com.atproto.server.refreshSession` using the refresh JWT.
- On 401 from feed APIs: refresh once and retry request.
- On refresh failure: log to `#status`, sleep with exponential backoff, retry. If repeated 4xx indicates revoked/bad credentials, publish `NetworkStateEvent{State:"disconnected"}` and stop.

OAuth-for-AT-Protocol is the long-term direction but out of scope for MVP.

## Polling loop

Polling is sufficient for personal use; firehose / Jetstream is deferred.

Important ATProto cursor behavior: feed/list cursors page **older** records. Do **not** persist `nextCursor` and reuse it as a "since" cursor for new posts. Steady polling should fetch the newest page each time, then rely on per-buffer DB dedupe plus an in-memory LRU.

```go
for {
    posts := getNewestPage(channel)
    sort.Slice(posts, byIndexedAtAscending)
    for _, p := range posts {
        if lruSeen(bufferID, p.URI) { continue }
        IngestPost(ctx, deps, networkID, channelName, toPost(p))
        markLRU(bufferID, p.URI)
    }
    sleep(interval)
}
```

Per-channel poll intervals (defaults):

- `#home`: 60 s
- `#notifications`: 90 s
- `#feed:*`: 120 s

Dedup:

- Keep an in-memory LRU of the last ~500 `post.uri` values per buffer to reduce DB writes during cursor/feed jitter.
- Enforce persistent dedupe with a log DB unique index on `(buffer_id, msgid)` where `msgid IS NOT NULL`.
- The current network-wide `messages_msgid` unique index must be migrated before datasource work, because one Bluesky post can legitimately appear in multiple buffers (`#home`, `#feed:*`, notifications).

Cursor storage:

- Bluesky steady polling does not need persisted `nextCursor` because it is an older-page cursor.
- The shared datasource foundation may still add `buffer_cursors(buffer_id, cursor, updated_at)` for cursor-oriented sources and optional explicit backfill jobs.
- Do not overload `buffers.last_seen_id` for datasource cursors; it is read-state, not source state.

Restart behavior:

- On restart, fetch newest page for each configured channel.
- Existing messages are ignored by `(buffer_id, msgid)` dedupe.
- New messages missed while offline are ingested if still present in the newest page. Deep gap-fill/backfill is deferred.

## Configuration

Extend `FileConfig` in `config.go`:

```yaml
data_sources:
  bluesky:
    - network: bluesky          # becomes networks.name
      identifier: alice.bsky.social
      app_password: ${BLUESKY_APP_PASSWORD}
      pds: https://bsky.social  # optional; default
      channels:
        - kind: home
        - kind: notifications
        - kind: feed
          name: news
          uri: at://did:plc:xxxx/app.bsky.feed.generator/whats-hot
        - kind: list
          name: friends
          uri: at://did:plc:xxxx/app.bsky.graph.list/yyyy
```

Validation in the datasource config builder:

- `network` required
- `identifier` required
- `app_password` required
- at least one channel required
- `feed` / `list` channels require both `name` and `uri`
- expand exact `${ENV_VAR}` secret values from the environment before validation

`Config` should carry parsed Bluesky source configs separately from IRC `Networks`, for example `BlueskySources []bluesky.Config` or a grouped `DataSources` field.

## SDK choice

Two options:

1. **`github.com/bluesky-social/indigo/api/bsky`** — official, full coverage, depends on a large lex-generated client. Adds significant module bloat.
2. **Hand-rolled HTTP** — only needs `createSession`, `refreshSession`, `getTimeline`, `listNotifications`, optionally `getFeed`/`getListFeed`. Small typed HTTP client plus minimal structs for consumed fields.

**Recommendation**: hand-rolled. The endpoints are stable, JSON-only, and the Indigo dependency tree is heavier than the rest of the project. Vendor only response structs for fields Lurker consumes.

## Implementation plan

### 1. Shared DB/API foundation

Add/modify:

- `db/control_migrations/000N_networks_kind.sql` — add `kind TEXT NOT NULL DEFAULT 'irc'` to `networks`.
- `db/log_migrations/000N_buffer_cursors.sql` — add optional `buffer_cursors(buffer_id BLOB PRIMARY KEY, cursor TEXT, updated_at TEXT)` for cursor-oriented datasource adapters.
- `db/log_migrations/000N_message_msgid_per_buffer.sql` — replace network-wide `messages_msgid` unique index with unique `(buffer_id, msgid)`.
- `db/network_types.go` — add `Kind` to `Network`.
- `db/control.go`, `db/network_store.go` — read/write `Kind`; default legacy rows to `"irc"`.
- `db/logstore.go` — add cursor helpers if `buffer_cursors` is introduced.

### 2. Generic datasource package

Add:

- `datasource/types.go` — `Post`, `Source`, `Deps`.
- `datasource/ingest.go` — shared `IngestPost` funnel.
- `datasource/manager.go` — source lifecycle manager mirroring `irc.Manager` shape.

`IngestPost` responsibilities:

1. resolve/create buffer via `stores.EnsureBuffer`
2. publish `buffer_created` if the buffer was first seen
3. insert via `db.InsertLogMessage`
4. publish `MessageEvent` if inserted
5. enqueue URL previews for `privmsg`, `notice`, and `action`

Adapters should not touch DB or hub directly except through this package.

### 3. Config parsing

Modify:

- `config.go`
- `config_test.go`
- `config.yaml.example`

Add typed `data_sources.bluesky[]` parsing/validation. Keep IRC `networks:` behavior unchanged.

### 4. API and frontend kind support

Modify:

- `api/state.go`, `api/helpers.go` — include `kind` in network DTOs.
- `api/networks.go` — REST-created networks remain IRC-only; reject connect/disconnect/connect-command endpoints for non-IRC networks.
- `api/ws.go` — reject mutating IRC commands when target network `kind != "irc"`; continue allowing `history` and `mark_read`.
- `web/src/app-state.ts` — add optional `kind?: string` to `Network`.
- Frontend command/actions code — hide or disable IRC-only actions for Bluesky buffers.

Mutating WS commands to reject for non-IRC networks include `send`, `join`, `part`, `rejoin`, `me`, `msg`, `notice`, `ctcp`, `topic`, `whois`, `mode`, `op`, `deop`, `voice`, `devoice`, `ban`, `unban`, `banlist`, `kick`, `kickban`, `invite`, `nick`, `away`, `back`, `quit`, `raw`, `list`, `query`, `ignore`, `unignore`, and `ignorelist`.

### 5. Bluesky client/source

Add:

- `datasource/bluesky/client.go` — narrow XRPC client, no third-party SDK.
- `datasource/bluesky/types.go` — minimal request/response structs.
- `datasource/bluesky/source.go` — implements `datasource.Source`; owns session refresh and poll goroutines.
- `datasource/bluesky/source_test.go` — fixture/table-driven post mapping tests.

Startup flow:

1. create/refresh session
2. upsert/open network row with `kind="bluesky"`
3. ensure configured buffers (`#home`, `#notifications`, `#feed:<name>`)
4. publish `NetworkStateEvent{State:"connected"}`
5. start one poll goroutine per configured channel

### 6. Main wiring

Modify `main.go`:

- create datasource manager after stores/hub/preview service exist
- register Bluesky sources from parsed config
- start datasource manager alongside IRC manager
- include IRC network names and datasource network names when calling `MarkNonYAMLNetworksDisabled`
- wait for datasource manager during shutdown

## Files added / modified

Added:

- `datasource/types.go`
- `datasource/ingest.go`
- `datasource/manager.go`
- `datasource/bluesky/source.go`
- `datasource/bluesky/client.go`
- `datasource/bluesky/types.go`
- `datasource/bluesky/source_test.go`
- `db/control_migrations/000N_networks_kind.sql`
- `db/log_migrations/000N_buffer_cursors.sql` *(optional shared foundation)*
- `db/log_migrations/000N_message_msgid_per_buffer.sql`

Modified:

- `config.go` — `data_sources.bluesky[]` parsing, validation, env-var expansion.
- `config_test.go`, `config.yaml.example` — config coverage/examples.
- `main.go` — wire up `datasource.Manager` after `irc.Manager` / preview service.
- `db/network_types.go`, `db/control.go`, `db/network_store.go` — propagate `Kind`.
- `db/logstore.go` — cursor helpers if `buffer_cursors` is added.
- `api/state.go`, `api/helpers.go`, `api/networks.go`, `api/ws.go` — propagate `Kind`; reject non-IRC mutations.
- `web/src/app-state.ts` and command UI files — support `kind`; hide/disable IRC-only actions.

## Verification

1. **Connect**: with credentials in `config.yaml`, run `task run`. Logs should show `data source connected source=bluesky network=bluesky`.
2. **Hydrate**: `curl http://localhost:8080/api/state | jq '.networks[] | select(.kind=="bluesky")'` — one network row, channel buffers present, recent posts in each.
3. **Live-ish polling**: post something from `bsky.app` in another tab. Within the configured poll interval, the post arrives over `/api/stream` as a `message` event with the right `network_id` / `buffer_id`.
4. **Read-only**: `{"type":"send","buffer_id":<id>,"content":"x"}` over WS → `error` envelope, no crash. Same for `join`/`part` against a Bluesky buffer.
5. **Previews**: a post containing an image URL or external link → preview event arrives shortly after, and reload via `/api/state` shows the preview attached.
6. **Duplicate across buffers**: same Bluesky `post.uri` can appear in `#home` and `#feed:*`; both insert once, and repeated polls do not duplicate either buffer.
7. **Restart**: stop and restart Lurker; newest page is polled, existing post URIs are ignored by per-buffer DB dedupe, and no duplicate posts appear.
8. **Auth failure**: revoke app password; source logs notices to status buffer, backs off, then marks network disconnected on repeated 4xx refresh failure.
9. `task test`, `task lint-web`, and `task build` pass.
