# Data source: Threads (deferred)

**Status**: deferred from MVP. Not implemented.

This document records why Threads is not part of the first cut and what would have to change for it to make sense as a Lurker "network".

## Why deferred

The Meta Threads API is a *publishing-and-monitoring* API for an account's own presence on Threads. It does **not** expose a home/following timeline — i.e. there is no endpoint that returns posts from the accounts the authenticated user follows.

What the API *does* expose (read-only, OAuth, long-lived token):

- `GET /me/threads` — the authenticated user's own posts
- `GET /me/replies` — the authenticated user's replies
- Replies / mentions on the authenticated user's posts
- `GET /me/threads_publishing_limit` — quota info
- `GET /threads_keyword_search` — public keyword search (subject to media-type filtering, follower thresholds, and rate limits)
- Insights endpoints (likes, views) for the user's own posts

What the API *does not* expose:

- A "following" feed
- A "for you" feed
- Per-account post history for arbitrary users (only the authenticated user, via `/me/*`)
- Real-time push or webhooks for inbound timeline activity

Compared to Bluesky's `app.bsky.feed.getTimeline` and Mastodon's `/api/v1/timelines/home` + streaming, Threads has no equivalent. An IRC-channel-style "what my follows are saying" pane is not buildable on the public API today.

The user explicitly chose to skip Threads in the MVP rather than ship a thin "mentions only" view that would not match the mental model of a channel.

## What a future Threads adapter would look like

If Meta ships a follower feed, or if the user later decides mentions/own-posts is worth wiring, the work is small because the cross-cutting `DataSource` abstraction (see [datasource-mastodon.md](datasource-mastodon.md)) is intentionally stateless about source semantics.

Sketched channel layout:

| Threads concept | Lurker concept |
|---|---|
| Account | One row in `networks` (`kind="threads"`). |
| `/me/threads` | `#mine` channel — the user's own posts. |
| Replies + mentions on user's posts | `#mentions` channel. Closest thing to an inbound feed today. |
| `threads_keyword_search` per saved query | optional `#search:<term>` channel per configured query. |
| Hypothetical future home/following feed | `#home` channel (this is the channel that's blocked today). |

Mapping fields:

| Threads field | `datasource.Post` field |
|---|---|
| `id` | `ExternalID` |
| `username` | `Sender` |
| `permalink` (or user numeric id) | `Account` |
| `text` + space-joined media URLs | `Content` |
| `timestamp` | `TS` |

Auth: Threads-only OAuth (Meta dev portal app, long-lived token). Token pasted into `config.yaml` under `data_sources.threads[]`. No streaming — poll `/me/replies` every ~5 minutes; dedup by post `id`.

Files that *would* be added:
- `datasource/threads/source.go`
- `datasource/threads/client.go`
- `datasource/threads/types.go`

No changes needed to `datasource/ingest.go`, the hub, the WebSocket protocol, or the frontend. The adapter only has to build `datasource.Post` values and call `IngestPost`. This is the same shape as the Bluesky and Mastodon adapters.

## Configuration sketch (not implemented)

```yaml
data_sources:
  threads:
    - network: threads
      access_token: ${THREADS_LONG_LIVED_TOKEN}
      user_id: 1234567890
      channels:
        - kind: mentions
        - kind: mine
        - kind: search
          name: lurker
          query: lurker
```

## When to revisit

Reasons to come back to this:
1. Meta exposes a home/following-feed endpoint on the Threads API (watch the [Threads API changelog](https://developers.facebook.com/docs/threads/changelog/)).
2. The user changes their mind and wants `#mentions` + `#mine` even without a home feed.
3. A reliable third-party gateway for the home feed appears (currently only unofficial scraper libs exist; not robust enough for a always-on bouncer).

Until then, this file exists to prevent re-investigation.
