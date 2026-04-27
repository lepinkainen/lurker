# Data source: Bluesky

This is an **MVP read-only data-source plan**. It describes how to surface a Bluesky account's home timeline (and optional list/feed views) inside Lurker as a fake IRC "network" with one or more channel buffers. See [ARCHITECTURE.md](ARCHITECTURE.md) for context, [storage.md](storage.md) for the buffer/message model, and [websocket-protocol.md](websocket-protocol.md) for the wire shapes the frontend already understands.

For the cross-cutting `DataSource` abstraction shared by all non-IRC sources, see [datasource-mastodon.md](datasource-mastodon.md). This document only covers the Bluesky-specific parts.

## Scope

- **Read-only**. Posting, liking, reposting, replying — all out of scope.
- **Hard-coded network**. Configured in `config.yaml`, never created via REST.
- **Best-effort fidelity**. Avatars, image galleries, threading, like counts, repost markers — dropped or collapsed to plain text in the first cut. URL/image links flow through the existing preview pipeline, so OG cards and inline images "just work".

## Mapping to Lurker concepts

| Bluesky concept | Lurker concept |
|---|---|
| Account | One row in `networks` (`kind="bluesky"`). Synthetic `host="bsky.social"`, `port=0`, `tls=false`. |
| Home timeline (`getTimeline`) | One channel buffer named `#home`. |
| Notifications (`listNotifications`) | One channel buffer named `#notifications`. |
| Saved/list feed (`getListFeed`, `getFeed`) | Optional channel buffer per configured feed URI, named `#feed:<short-name>`. |
| Post (`app.bsky.feed.defs#feedViewPost`) | One row via `db.InsertLogMessage` + `MessageEvent`. |
| `post.author.handle` | `MessageEvent.Sender`. |
| `post.author.did` | `MessageEvent.Account`. |
| `record.text` (+ extracted embed URLs joined with space) | `MessageEvent.Content`. |
| `post.uri` | `MessageEvent.MsgID` (used as the dedupe key). |
| `post.indexedAt` | `MessageEvent.TS`. |
| Reposts | Sender is the original `post.author.handle`; `Content` is prefixed with `[RT by <reposter>] ` if `reason.$type == app.bsky.feed.defs#reasonRepost`. |
| Replies | Top-level only in `#home`. The thread parent URI is appended to `Content` as a link so the existing URL-preview pipeline can fetch it; we do not render a thread tree in MVP. |

`MessageEvent.Kind` is set to `"privmsg"` for posts so the existing preview enqueuer (`irc/handler.go:426-434`) picks them up. System messages (auth-failed, rate-limited, etc.) go to a `status` buffer with `Kind="notice"`.

## Authentication

Use **App Passwords**, not the account password. Generated at <https://bsky.app/settings/app-passwords>.

- Endpoint: `com.atproto.server.createSession` on the user's PDS (default `https://bsky.social`).
- Request body: `{ "identifier": "<handle-or-did>", "password": "<app-password>" }`.
- Response carries `accessJwt` (~2 h TTL) and `refreshJwt` (longer TTL).
- Refresh via `com.atproto.server.refreshSession` using the refresh JWT.
- On refresh failure: log to `#status`, sleep with backoff, retry. If repeated 4xx, mark the network `disconnected` via `NetworkStateEvent` and stop.

OAuth-for-AT-Protocol is the long-term direction but out of scope for the MVP.

## Polling loop

Polling is sufficient for personal use; firehose / Jetstream is deferred.

```
for {
  posts, nextCursor := getTimeline(cursor=stored)
  sort ascending by indexedAt
  for _, p := range posts {
    if seen(p.uri) { continue }
    IngestPost(networkID, "#home", toPost(p))
  }
  storeCursor(nextCursor)
  sleep(60s)  // overridable per-channel
}
```

Per-channel poll intervals (defaults):
- `#home`: 60 s
- `#notifications`: 90 s
- `#feed:*`: 120 s

Cursor persistence: piggyback on the existing `buffers.last_seen_id` column by storing the Bluesky cursor in a new `meta TEXT` column on the per-network log DB's `buffers` table, or in a sibling `buffer_cursors(buffer_id, cursor)` table. **Recommended**: sibling table, so we don't overload an existing column with a string payload that has nothing to do with read state.

Dedup: keep an in-memory LRU of the last ~500 `post.uri` values per buffer to avoid double-ingestion on cursor jitter.

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

Validation in `buildNetworks`-equivalent: identifier required, password required, at least one channel.

## SDK choice

Two options:

1. **`github.com/bluesky-social/indigo/api/bsky`** — official, full coverage, depends on a large lex-generated client. Adds significant module bloat.
2. **Hand-rolled HTTP** — we need only `createSession`, `refreshSession`, `getTimeline`, `listNotifications`, optionally `getFeed`/`getListFeed`. ~150 LoC of typed HTTP plus generated structs for the ~5 response shapes we use.

**Recommendation**: hand-rolled. The endpoints are stable, JSON-only, and the indigo dependency tree is heavier than the rest of the project combined. Vendor the response structs only for the fields we actually consume.

## Files added / modified

Added:
- `datasource/bluesky/source.go` — implements `datasource.Source`. Owns the poll goroutines and session refresh.
- `datasource/bluesky/client.go` — narrow HTTP client, no third-party SDK.
- `datasource/bluesky/types.go` — minimal request/response structs.
- `datasource/bluesky/source_test.go` — table-driven tests for `feedViewPost → datasource.Post` mapping using fixture JSON pulled from a real response.

Modified (cross-cutting, also referenced from the Mastodon doc):
- `config.go` — `data_sources:` block, parsed into a typed config.
- `main.go` — wire up `datasource.Manager` after `irc.Manager`.
- `db/control_migrations/000N_networks_kind.sql` — add `kind TEXT NOT NULL DEFAULT 'irc'` to `networks`.
- `db/network_types.go` — add `Kind` to `Network`.
- `api/state.go`, `api/networks.go`, `api/ws.go` — propagate `Kind`; reject mutating WS commands when `Kind != "irc"`.

## Verification

1. **Connect**: with credentials in `config.yaml`, run `task run`. Logs should show `data source connected source=bluesky network=bluesky`.
2. **Hydrate**: `curl http://localhost:8080/api/state | jq '.networks[] | select(.kind=="bluesky")'` — one network row, channel buffers present, recent posts in each.
3. **Live**: post something from `bsky.app` in another tab. Within 60 s the post arrives over `/api/stream` as a `message` event with the right `network_id` / `buffer_id`.
4. **Read-only**: `{"type":"send","buffer_id":<id>,"content":"x"}` over WS → `error` envelope, no crash. Same for `join`/`part` against a Bluesky buffer.
5. **Previews**: a post containing an image URL or external link → URL preview event arrives shortly after, and reload via `/api/state` shows the preview attached.
6. **Restart**: stop and restart lurker; the cursor resumes correctly (no duplicate posts in the buffer).
7. `task test` and `task lint` pass.
