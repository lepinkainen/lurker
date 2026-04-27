# Data source: Matrix

This is an **MVP read-only data-source plan**. It describes how to surface Matrix rooms inside Lurker as a fake IRC "network" with one channel buffer per joined room. See [ARCHITECTURE.md](ARCHITECTURE.md), [storage.md](storage.md), and [websocket-protocol.md](websocket-protocol.md) for the existing pieces this plugs into. The cross-cutting `DataSource` / `IngestPost` abstraction is defined in [datasource-mastodon.md](datasource-mastodon.md).

## Scope

- **Read-only**. No sending, reacting, redacting.
- **Hard-coded network**. Configured in `config.yaml`; no REST CRUD.
- **Plaintext rooms only** in MVP. E2E-encrypted rooms are deferred (see below).
- **Best-effort fidelity**. Formatted messages, reactions, replies, read receipts, presence — all dropped or collapsed to plain text. Media URLs flow through the existing preview pipeline unchanged.

## Why Matrix fits especially well

Matrix rooms the account has already joined become channels without any user-side configuration. Compared to Bluesky (requires configuring which feeds to track) or Mastodon (requires configuring timelines), Matrix's "what should the channels be?" question answers itself: every joined room is a channel. No feed lists, no saved searches.

## Mapping to Lurker concepts

| Matrix concept | Lurker concept |
|---|---|
| Homeserver + `@user:homeserver` | One row in `networks` (`kind="matrix"`, `host=<homeserver>`, `port=443`, `tls=true`). |
| Joined room | One channel buffer named `#<room-alias>` or `#<room-id>` (see naming below). |
| `m.room.message` event (`msgtype: m.text`) | One row via `IngestPost`. |
| `m.room.message` with `msgtype: m.emote` | `Kind="action"`, same as IRC `/me`. |
| `m.room.message` with `msgtype: m.notice` | `Kind="notice"`. |
| `m.room.message` with `msgtype: m.image/m.file/m.video/m.audio` | `Kind="privmsg"`, `Content` is the `mxc://` URL converted to HTTPS media URL (so the preview pipeline can fetch the image). |
| `event.sender` (`@nick:homeserver`) | `Sender`. Short form (localpart only) preferred if unambiguous within the room. |
| `event.sender` | `Account`. |
| `event.event_id` | `ExternalID` / `MsgID` (dedupe key). |
| `event.origin_server_ts` | `TS`. |
| `m.room.redaction` | Ignored in MVP. |
| `m.room.member`, `m.room.topic`, etc. | Never emitted as messages. Topic changes could map to `BufferUpdateEvent.Topic`; deferred. |

### Room naming

Room buffers are named:

1. `#<canonical-alias>` if the room has a canonical alias (e.g. `#lurker:matrix.org` → buffer name `#lurker:matrix.org`).
2. Falling back to `#<room-id-short>` (first 8 chars of the localpart, e.g. `#!aBcDeFgH`).

The `:` in aliases is valid in a buffer name (it is just a string to Lurker). The `#` prefix makes it look like an IRC channel to the frontend.

## Authentication

Two options:

1. **Password login** (`m.login.password`): username + password. Simple; generates an access token that persists across restarts if saved.
2. **Access token directly**: user generates a token via Element or `curl`, pastes it into config. Most robust for a long-running service.

**Recommendation**: support both in config. Store the resulting access token to disk (in the per-network log DB or a config-adjacent file) so the session survives restarts without re-authing.

Device registration is automatic on first login. A stable `device_id` should be persisted and reused to avoid accumulating stale sessions on the homeserver.

## Sync loop (ingest mechanism)

Matrix uses **`/sync`** (long-poll), not WebSocket streaming. This is the primary event delivery mechanism.

```
POST /login → { access_token, device_id }

loop:
  GET /_matrix/client/v3/sync?since=<since_token>&timeout=30000
  for each room in response.rooms.join:
    for each event in timeline.events:
      if event.type == "m.room.message":
        IngestPost(networkID, roomBufferName, toPost(event))
  persist next_batch as new since_token
  sleep(0)  // /sync with timeout=30000 blocks server-side; no client sleep needed
```

On first sync, `since` is empty. The server returns the full current state + recent timeline. Set `full_state=false` and rely on `timeline.limited` + `prev_batch` for backfill if needed.

`since_token` persistence: stored in a `buffer_cursors(buffer_id, cursor TEXT)` table in the per-network log DB (same pattern planned for Bluesky). The global `since_token` is per-network (not per-buffer), so store it in a `network_meta(network_id, since_token TEXT)` table or as a synthetic status-buffer cursor.

Reconnect: the sync loop naturally retries on HTTP error. On 401 (token expired / revoked): log to the status buffer, emit `NetworkStateEvent{State: "disconnected"}`, stop.

## E2E encryption (deferred)

`mautrix` supports E2E via `mautrix/crypto` + `libolm` (CGo). For plaintext rooms this entire subsystem is unused.

Reasons to defer:
- CGo dependency complicates cross-compilation and the container build.
- Key storage (Megolm session keys, cross-signing) adds meaningful complexity.
- Many community and self-hosted rooms are not encrypted.

When E2E is added, the wiring point is `mautrix`'s `OlmMachine`, which slots in between `/sync` response parsing and event dispatch. `IngestPost` itself does not change.

## Configuration

Extend `FileConfig` in `config.go`:

```yaml
data_sources:
  matrix:
    - network: matrix-home          # becomes networks.name
      homeserver: https://matrix.org
      user_id: "@alice:matrix.org"
      # supply one of:
      access_token: ${MATRIX_ACCESS_TOKEN}
      # or:
      # password: ${MATRIX_PASSWORD}
      # optionally restrict which rooms become channels:
      rooms:                        # if empty, all joined rooms appear
        - "!roomid:matrix.org"
        - "#lurker:matrix.org"
```

If `rooms:` is absent, every joined room creates a buffer. If it is present, only listed rooms (by ID or canonical alias) are ingested.

## SDK choice

Use **`maunium.net/go/mautrix`**.

- Actively maintained (used by mautrix-whatsapp, mautrix-telegram, mautrix-signal bridges).
- Handles `/sync` parsing, room state accumulation, event dispatch, login, token refresh.
- No CGo required when E2E crypto is not enabled.
- Well-typed event structs for all `m.room.*` types we consume.
- Modest dependency footprint compared to a full bridge setup (we only import the core client, not the bridge or crypto subpackages).

Alternatives considered:
- `github.com/matrix-org/gomatrix` — older, less maintained, lower-level; would require hand-rolling more sync logic.
- Hand-rolled HTTP — `/sync` response parsing is complex enough (nested room state, limited timelines, to-device events) that it is not worth it here, unlike the simpler Bluesky endpoints.

## Files added / modified

Added:
- `datasource/matrix/source.go` — implements `datasource.Source`. Owns the `/sync` goroutine, handles login/token persistence, maps events to `datasource.Post`.
- `datasource/matrix/client.go` — thin wrapper around `mautrix.Client` setup (base URL, HTTP client timeout, user agent).
- `datasource/matrix/source_test.go` — fixture-driven tests: raw `m.room.message` JSON → `datasource.Post` mapping for each `msgtype`.

Modified (shared with Bluesky / Mastodon work):
- `config.go` — `data_sources.matrix[]` parsing; support both `access_token` and `password` fields.
- `main.go` — register Matrix adapter in `datasource.Manager`.
- `db/control_migrations/000N_networks_kind.sql` — add `kind TEXT NOT NULL DEFAULT 'irc'` to `networks` *(if not already added by Bluesky/Mastodon work)*.
- `db/network_types.go` — `Kind` field on `Network` *(shared)*.
- `api/ws.go`, `api/networks.go`, `api/state.go` — propagate `Kind`; reject mutating commands *(shared)*.

## Verification

1. **Connect**: `task run` with a valid config. Logs show `data source connected source=matrix network=matrix-home`. Status buffer receives a synthetic "connected" message.
2. **Hydrate**: `/api/state` returns one network with `kind="matrix"` and one channel buffer per joined room.
3. **Live**: send a message in a joined room from another client (e.g. Element). Within ~1 s it arrives via `/api/stream` as a `message` event.
4. **Media**: a room member posts an image. The `mxc://` URL is converted to the HTTPS media endpoint and appears in `Content`; the preview pipeline resolves and pushes a `preview` event.
5. **Read-only**: `send` / `join` / `part` WS commands targeting a Matrix buffer → `error` envelope, no crash.
6. **Restart**: `since_token` persists; no duplicate messages after restart; sync resumes from the stored token.
7. **Room filter**: with a `rooms:` list in config, only listed rooms create buffers; others are ignored.
8. `task test` and `task lint` pass.
