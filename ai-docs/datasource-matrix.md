# Data source: Matrix

Phased plan for surfacing a Matrix account inside Lurker. Phase 1 is a read-only data source like Bluesky; later phases make Matrix the first *interactive* non-IRC network (sending, read receipts, E2EE). See [ARCHITECTURE.md](ARCHITECTURE.md), [storage.md](storage.md), and [websocket-protocol.md](websocket-protocol.md) for the pieces this plugs into. The shared `datasource.Source` / `IngestPost` abstraction lives in `datasource/` (see [datasource-bluesky.md](datasource-bluesky.md)).

Plan researched and updated 2026-06; library facts verified against mautrix-go v0.28.0.

## SDK choice

Use **`maunium.net/go/mautrix`** (MPL-2.0).

- The only seriously maintained Go Matrix library. Monthly releases (v0.28.0, 2026-05); foundation of all mautrix bridges and gomuks. `github.com/matrix-org/gomatrix` has been archived since Feb 2024 — do not use.
- Handles `/sync` long-poll loop, typed event structs, login, media, receipts, pagination (`Client.Messages` over `/rooms/{id}/messages`).
- **No CGO required.** Lurker builds with `CGO_ENABLED=0`, so when E2EE lands we must build with `-tags goolm` to select the pure-Go Olm implementation (`crypto/goolm`) instead of the default libolm CGO binding. goolm is opt-in but considered near production-ready as of the April 2026 mautrix release notes; libolm upstream is deprecated, so the default will flip eventually.
- E2EE is near-turnkey via `crypto/cryptohelper`: `cryptohelper.NewCryptoHelper(client, pickleKey, sqlitePath)` + `Init()` transparently decrypts `m.room.encrypted` in the sync loop and encrypts outbound sends. Interactive device verification (emoji SAS, QR) via `crypto/verificationhelper`; key backup restore via recovery key is supported.
- Integration note to verify at implementation time: the cryptohelper string-path convenience and `sqlstatestore` go through `go.mau.fi/util/dbutil`. Bridges typically pair it with the CGO sqlite driver, so we may need to hand it a pre-opened `dbutil.Database` backed by `modernc.org/sqlite` (Lurker's existing pure-Go driver) instead of the string path.

Alternative considered: gomuks' `hicli` (`go.mau.fi/gomuks/pkg/hicli`) is a batteries-included client framework on top of mautrix-go — architecturally the closest existing thing to "Matrix bouncer with thin frontends" and worth reading for patterns (timeline storage, send queue). Not importable: AGPL-3.0, and it owns its own SQLite schema which would fight Lurker's storage model.

## Using an existing Matrix account

Three supported ways in, all landing on the same persisted session:

1. **Password login** (`m.login.password`): `user_id` + `password` in config. Works against matrix.org even after its April 2025 migration to next-gen auth (MAS keeps a legacy compatibility layer for `/login`; the bridge ecosystem depends on it, so it is not going away soon). mautrix-go does **not** implement the native OAuth (MSC3861) or QR (MSC4108) flows — fine, the compat layer is the pragmatic path.
2. **Pasted access token** (+ `device_id`): generated in Element (or the MAS account console) and put in config. Required for SSO-only accounts (e.g. matrix.org accounts created via Google), where no password exists.
3. **Persisted session reuse**: whichever way the first login happens, persist `{access_token, device_id}` and reuse on every restart. Never re-login with the password each boot — that would accumulate stale devices on the homeserver and invalidate E2EE sessions.

Session persistence: `matrix_sessions` table in `control.db` keyed by network ID (`access_token`, `device_id`, `user_id`, `updated_at`), `ON DELETE CASCADE`. Secrets never appear in `/api/state` (same rule as connect commands). The per-network `since_token` (sync cursor) lives in the per-network log DB (`network_meta(key, value)` table), because it is log-position state, not credentials.

This is one device of the user's real account: Lurker sees every joined room, and (in later phases) messages it sends are attributed to the user and read receipts it sends clear notifications on the user's phone — the IRCCloud model, which is exactly what we want.

## Mapping to Lurker concepts

| Matrix concept | Lurker concept |
|---|---|
| Homeserver + `@user:hs` | One `networks` row (`kind="matrix"`, `host=<homeserver>`, `nick=<user_id>`). |
| Joined room (group) | Channel buffer (naming below). |
| Direct-message room (`m.direct` account data / 2-member heuristic) | Query buffer named after the other user. |
| `m.room.message` `msgtype: m.text` | `kind="privmsg"` via `IngestPost`. |
| `msgtype: m.emote` | `kind="action"`. |
| `msgtype: m.notice` | `kind="notice"`. |
| `msgtype: m.image/m.file/m.video/m.audio` | `kind="privmsg"`, content = `mxc://` converted to HTTPS download URL so the preview pipeline can render it (see media note). |
| `event.sender` | `Sender` = localpart (disambiguate with full ID on collision); `Account` = full `@user:hs`. |
| `event.event_id` | `MsgID` (dedupe key — load-bearing, see echo semantics). |
| `event.origin_server_ts` | `TS`. |
| `m.room.topic` | `BufferUpdateEvent.Topic`. |
| `m.room.member` join/leave | `presence` events + member list (Phase 4; dropped in Phase 1). |
| Edits (`m.replace`), reactions (`m.annotation`), threads, redactions | Phase 4 (see protocol changes). Dropped in Phase 1; mautrix-go parses `RelatesTo`, aggregation is ours. |

### Room naming

1. `#<canonical-alias>` if the room has one (`#lurker:matrix.org`).
2. Else `m.room.name` state, prefixed `#`.
3. Else for DMs: the other member's localpart (query buffer, no `#`).
4. Else `#<room-id-prefix>` (first 8 chars of the localpart). The full client-side "heroes" display-name algorithm is not provided by mautrix-go; this approximation is fine for v1.

Room name/alias changes after buffer creation are ignored (Lurker buffer names are stable); the room ID → buffer ID mapping is persisted in the log DB `network_meta` table so renames never fork buffers.

## Phase 1 — read-only ingest (plaintext rooms)

Same shape as Bluesky: a `datasource.Source` implementation, **zero WebSocket protocol changes** — matrix networks are gated by the existing `commandsAllowedForNonIRC` allowlist (`input`/`history`/`mark_read` only) and the frontend's existing datasource read-only handling.

- `datasource/matrix/source.go` — implements `datasource.Source`. `Start`: login or token check (`/whoami`), upsert network row, initial `/sync` (no `since`) to enumerate joined rooms, `EnsureBuffer` per room, then launch the sync goroutine.
- Sync loop: `mautrix.Client.SyncWithContext` with a `DefaultSyncer`; `OnEventType(event.EventMessage, …)` → map → `IngestPost`. Implement `mautrix.SyncStore` over the log-DB `network_meta` table so `next_batch` survives restarts with no duplicate ingest (the `(buffer_id, msgid)` unique key is the backstop).
- Encrypted rooms: skipped with a one-time status-buffer notice per room ("room X is encrypted; E2EE not enabled").
- New rooms joined (from another client) mid-session appear via sync → `EnsureBuffer` on first event.
- On 401: status-buffer message + `network_state disconnected`, stop (token revoked needs operator action). Other HTTP errors: backoff and resume — `/sync` is naturally resumable.
- Media: homeservers now require authenticated media (MSC3916; mautrix-go handles it automatically since v0.21.0) — so a bare HTTPS `/media/download` URL in message content will **not** be fetchable by the preview pipeline or the browser without auth. v1: render the filename/URL as text; proper inline media needs a small authenticated proxy endpoint (`/api/matrix/media/{network}/{server}/{id}`) that streams via the client's token — Phase 4.

Config (`config.go`, mirroring the Bluesky block):

```yaml
data_sources:
  matrix:
    - network: matrix-home          # networks.name, stable
      homeserver: https://matrix.org
      user_id: "@alice:matrix.org"
      password: ${MATRIX_PASSWORD}      # or:
      # access_token: ${MATRIX_TOKEN}
      # device_id: ABCDEFGH
      rooms: []                     # optional allowlist (IDs or aliases); empty = all joined rooms
```

## Phase 2 — sending (first interactive datasource)

This is where Matrix diverges from Bluesky and the shared abstraction grows a capability:

- `datasource.Sender` optional interface: `Send(ctx, bufferID, kind, content) error` (kind = privmsg/action/notice → `m.text`/`m.emote`/`m.notice`). `api.Server` gains a way to resolve network ID → registered `Sender`.
- **WS protocol change (server-side routing only, no new wire types):** replace the boolean `commandsAllowedForNonIRC` gate with a per-kind capability set. Matrix allows `send`, `me`, `input`, `history`, `mark_read`; later `join` (accept room alias/ID in `channel`), `part` (leave room), `query`, `topic`. Everything else still errors with "command not supported".
- **`/api/state` change:** network DTO gains a `caps` array (e.g. `["send","join","mark_read_upstream"]`). The frontend switches its compose-box gating from "kind != irc → read-only" to capability-driven. This is the only client-visible protocol change Phase 2 needs.
- **Echo semantics — opposite of IRC.** Matrix *does* echo your own sends back via `/sync`. Do not blind-log outbound like `Manager.LogOutbound`: `POST /send` returns the `event_id`; persist the outbound message immediately with `MsgID = event_id` (instant local echo in the UI), and the sync copy is rejected by the existing `(buffer_id, msgid)` dedupe. Failed send = error to the client, nothing persisted.
- `mark_read` upstream: when the command targets a matrix buffer and the newest message has a Matrix event ID, also call `SendReceipt` (`m.read`) so notifications clear on the user's other devices. Lurker-side last-seen behavior is unchanged.

## Phase 3 — E2EE

- Build with `-tags goolm` (pure-Go Olm; keeps `CGO_ENABLED=0` builds and the container image unchanged).
- `cryptohelper.NewCryptoHelper(client, pickleKey, store)` with a SQLite crypto store at `data/<network>-matrix-crypto.db` (disposable-ish but losing it means re-verifying the device; document under operations). Pickle key: random, stored alongside the session row.
- Sends to encrypted rooms are encrypted transparently by the helper once `client.Crypto` is set; inbound `m.room.encrypted` decrypts in the sync loop and flows through the same handler.
- Verification, so other clients trust this device and history decrypts:
  - v1: **recovery key in config** → restore cross-signing + Megolm key backup via the SSSS helpers; self-verifies the device and unlocks old room keys.
  - v2 (optional): interactive emoji-SAS via `crypto/verificationhelper`, surfaced as status-buffer prompts.
- Undecryptable events (key not yet shared) ingest as a placeholder message; no retro-edit of stored rows until Phase 4's edit machinery exists.

## Phase 4 — fidelity (each item independent)

- **Edits** (`m.replace`): update the stored message row content (suffix ` (edited)`), publish a new `message_edited` stream event (`buffer_id`, `id`, `content`) — the first genuinely new WS event type; frontend patches in place, falls back to ignoring it.
- **Redactions**: blank stored content → `message_redacted` event.
- **Reactions** (`m.annotation`): aggregate counts per message; new `reaction` event or fold into `message_edited`. Lowest priority.
- **Presence/membership**: map `m.room.member` join/leave to existing `presence` events; publish `member_list` from `Client.JoinedMembers`.
- **Typing/receipts inbound**: probably never — Lurker doesn't surface these for IRC either.
- **Media proxy + upload**: authenticated download proxy endpoint (see Phase 1 media note); wire the existing `/api/upload` flow to `Client.UploadMedia` for outbound images.
- **Backfill**: on first buffer creation, one `Client.Messages` page (e.g. 50 events) backwards so new rooms aren't empty.

## Files added / modified (Phase 1–2)

Added:
- `datasource/matrix/source.go` — Source impl, sync loop, event mapping
- `datasource/matrix/session.go` — login/token persistence, SyncStore impl
- `datasource/matrix/source_test.go` — fixture-driven event → Post mapping tests per msgtype

Modified:
- `config.go` — `data_sources.matrix[]` block
- `main.go` — register matrix sources in `datasource.Manager`
- `db/network_types.go` — `NetworkKindMatrix`
- `db/control_migrations/` — `matrix_sessions` table; `db/log_migrations/` — `network_meta` table
- Phase 2: `datasource/types.go` (Sender), `api/ws.go` (capability gating), `api/state.go` (network `caps`), `web/src` compose gating

## Verification

1. **Connect**: valid config → log line `data source connected source=matrix`, `/api/state` shows `kind="matrix"` network with one buffer per joined room.
2. **Live**: message sent from Element arrives via `/api/stream` within ~1 s.
3. **Restart**: `since_token` persisted; no duplicates (dedupe key = event ID).
4. **Read-only gate (Phase 1)**: `send` to a matrix buffer → error envelope.
5. **Send (Phase 2)**: WS `send` → message appears in Element attributed to the account; no duplicate row from the sync echo.
6. **mark_read (Phase 2)**: marking a buffer read clears the room's unread badge in Element.
7. **E2EE (Phase 3)**: encrypted room round-trips; device shows verified (green shield) in Element after recovery-key setup.
8. `task test`, `task lint` pass; container image still builds with `CGO_ENABLED=0`.
