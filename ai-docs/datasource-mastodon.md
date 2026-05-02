# Data source: Mastodon

This is an **MVP read-only data-source plan**. It describes how to surface a Mastodon account's home timeline, optional public/list timelines, and notifications inside Lurker as a fake IRC "network" with one or more channel buffers. See [ARCHITECTURE.md](ARCHITECTURE.md), [storage.md](storage.md), and [websocket-protocol.md](websocket-protocol.md) for the existing pieces this plugs into.

This document defines the cross-cutting `DataSource` abstraction shared by all non-IRC sources (Bluesky reuses it).

## Scope

- **Read-only**. No posting, boosting, favoriting, replying.
- **Hard-coded network**. One row per Mastodon account in `config.yaml`. No REST CRUD.
- **Best-effort fidelity**. Avatars, custom emoji, content warnings, polls, threading collapse to plain text. Media URLs are kept and flow through the existing preview pipeline.

## Cross-cutting `DataSource` abstraction

All non-IRC sources share a small package. This is referenced from [datasource-bluesky.md](datasource-bluesky.md) and the deferred [datasource-threads.md](datasource-threads.md).

```
package datasource

type Post struct {
    ExternalID string    // wire dedupe key (status ID, post URI, etc.)
    TS         time.Time
    Sender     string    // human handle
    Account    string    // stable id (acct, DID, …)
    Kind       string    // "privmsg" | "notice" | "action"
    Content    string    // plain text; URLs preserved for the preview pipeline
}

type Source interface {
    Name() string                   // e.g. "mastodon", "bluesky"
    NetworkID() uuid.UUID           // resolved at startup
    Start(ctx context.Context) error
    Wait()                          // blocks until goroutines drain
}

// IngestPost is the shared funnel — the analogue of irc.handler.storeEvent.
// It resolves the buffer via stores.EnsureBuffer (same shared-UUID path IRC uses),
// inserts via db.InsertLogMessage, then publishes a MessageEvent on the hub.
func IngestPost(ctx context.Context, deps Deps, networkID uuid.UUID, channelName string, p Post) error
```

`Deps` carries the `*db.MultiStore`, `*hub.Hub`, and the per-network log DB handle. Adapters never touch the DB or hub directly — they only build a `Post` and call `IngestPost`.

`MessageEvent.Kind` for normal posts is `"privmsg"` so the existing `enqueuePreviews` branch (`irc/handler.go:426-434`) picks them up and the URL preview pipeline runs unchanged.

## Mapping to Lurker concepts

| Mastodon concept | Lurker concept |
|---|---|
| Instance + account | One row in `networks` (`kind="mastodon"`, `host=<instance host>`, `port=0`, `tls=false`). |
| Home timeline | `#home` channel buffer. |
| Local timeline | optional `#local` channel buffer. |
| Federated timeline | optional `#federated` channel buffer. |
| Notifications | optional `#notifications` channel buffer. |
| List timeline | one optional `#list:<slug>` per configured list. |
| Status | one row via `IngestPost`. |
| `status.account.acct` | `Sender`. |
| `status.account.id` | `Account`. |
| HTML-stripped `status.content` + ` ` + media URLs | `Content`. |
| `status.id` | `ExternalID` (also stored as `MessageEvent.MsgID`). |
| `status.created_at` | `TS`. |
| Boost (`reblog` populated) | Sender is the boosted author; `Content` is prefixed with `[RT by <booster>] `. |
| Reply (`in_reply_to_id`) | Top-level only; we do not reconstruct threads in MVP. |
| Content warning (`spoiler_text`) | Prefix `Content` with `[CW: <spoiler_text>] `. |

## Authentication

Personal access token from instance Settings → Development → New Application (scopes: `read`, `read:notifications` minimum; `read:list` if list timelines are wanted). Pasted into `config.yaml`.

No OAuth dance, no app review.

## Live ingest: streaming

Mastodon's WebSocket streaming is the right primary path. `github.com/mattn/go-mastodon`'s `Client.StreamingWSUser(ctx)` returns a `chan mastodon.Event`.

Lifecycle:

1. **Backfill**: `GetTimelineHome(ctx, &Pagination{Limit: 40})` ascending by `id`. Push each through `IngestPost`. Persist the highest `id` seen as the cursor.
2. **Stream**: open `StreamingWSUser`. For each event:
   - `*UpdateEvent` → `#home` (or appropriate buffer based on stream filter).
   - `*NotificationEvent` → `#notifications`.
   - `*DeleteEvent` → ignored in MVP (we don't expose deletions).
   - `*ErrorEvent` → log to `#status`; trigger reconnect.
3. **Reconnect**: on stream close or error, exponential backoff (1 s → 60 s cap, mirror `irc.Manager`). On reconnect, do one `GetTimelineHome` since the stored cursor to fill the gap before re-attaching the stream.

For `#local` / `#federated` / `#list:*` we open additional streams (`StreamingWSPublic`, `StreamingWSList`) only if those channels are configured. Each stream is its own goroutine with its own reconnect loop.

Since v4.2, anonymous streaming was removed — the access token is mandatory.

## Configuration

Extend `FileConfig` in `config.go`:

```yaml
data_sources:
  mastodon:
    - network: mastodon-fosstodon  # becomes networks.name
      instance: https://fosstodon.org
      access_token: ${MASTODON_TOKEN}
      channels:
        - kind: home
        - kind: notifications
        - kind: local           # optional
        - kind: list
          name: musicians
          id: 1234
```

Validation: instance and access_token required, at least one channel.

## SDK choice

Use **`github.com/mattn/go-mastodon`**. It is small, well-maintained, has working streaming including WS, and covers everything we need without re-implementing pagination + auth. The dependency footprint is acceptable.

## Files added / modified

Added:
- `datasource/types.go` — `Source`, `Post`, `Deps`.
- `datasource/ingest.go` — `IngestPost`. Single funnel that resolves the buffer and publishes the `MessageEvent`.
- `datasource/manager.go` — `Manager` that owns all source goroutines, mirrors `irc.Manager` shape (`main.go:55,119`).
- `datasource/mastodon/source.go` — `mastodon.Source` implementing `datasource.Source`. Owns the streaming goroutines.
- `datasource/mastodon/source_test.go` — fixture-driven tests for `*mastodon.Status → datasource.Post`.

Modified:
- `config.go` — `data_sources.mastodon[]` parsing, validation, env-var expansion.
- `main.go` — instantiate `datasource.Manager`, register the Mastodon adapter, `defer mgr.Wait()`.
- `db/control_migrations/000N_networks_kind.sql` — add `kind TEXT NOT NULL DEFAULT 'irc'` to `networks`.
- `db/network_types.go` — add `Kind` to `Network`; default to `"irc"` for legacy rows.
- `api/state.go`, `api/networks.go` — propagate `Kind` to clients; restrict `PATCH`/`DELETE` on non-IRC networks to safe fields (rename, sort_order).
- `api/ws.go` — for any mutating command (`send`, `join`, `part`, `me`, `msg`, `topic`, `whois`, `mode`, `kick`, `invite`, `raw`) targeting a non-IRC network, reply with an `error` envelope.

## Verification

1. **Connect**: `task run` → logs `data source connected source=mastodon network=mastodon-<instance>`.
2. **Hydrate**: `/api/state` returns one network with `kind="mastodon"` and the configured channel buffers populated.
3. **Live**: from another client, post on the same instance. Within ~2 s the post arrives via `/api/stream`.
4. **Notifications**: have someone reply to/mention you → arrives in `#notifications`.
5. **Boost**: a boost in your home timeline shows correct `Sender` (original author) and `[RT by …]` prefix.
6. **Read-only**: `send`/`join`/`part` over WS → `error` envelope.
7. **Reconnect**: kill network briefly (`sudo iptables` rule, then drop) → stream reconnects, gap filled via `GetTimelineHome`, no duplicate posts.
8. **Restart**: cursor persists; no duplicates after restart.
9. `task test` and `task lint` pass.
