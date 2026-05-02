# Storage model

This document describes Lurker's on-disk storage layout, the network and buffer models, and the rationale for the three-database split. See [ARCHITECTURE.md](ARCHITECTURE.md) for the overall system context.

## Databases

### Control DB

Path:

- `data/control.db`

Purpose:

- canonical network definitions
- global buffer registry
- schema migration bookkeeping

Important tables:

- `networks`
- `buffer_registry`
- `network_connect_commands`
- `schema_migrations`

The `networks` table stores:

- stable UUIDv7 network ID, stored on disk as a 16-byte SQLite `BLOB`
- human-facing network name
- connection settings like host/port/tls/nick/realname/SASL
- `sort_order` for persistent sidebar ordering

`network_connect_commands` stores an ordered raw IRC command list per network, keyed by `(network_id, position)` with `ON DELETE CASCADE`. Commands may contain secrets and are only exposed through the explicit connect-command API, not `/api/state`.

The `buffer_registry` table stores the global API-facing buffer ID namespace. Buffer IDs are UUIDv7 values stored as 16-byte SQLite `BLOB`s and serialized over JSON as strings.

### Per-network log DBs

Path pattern:

- `data/<normalized-network-name>.db`

Purpose:

- messages for that network
- per-buffer mutable state such as topic/joined/last seen

Important properties:

- message history is sharded by network DB
- backend message logs are retained indefinitely
- there is no automatic retention policy, max-age cleanup, or max-row cleanup for stored messages
- buffer IDs are the same UUIDv7 values used by `control.db.buffer_registry`; there is no separate local buffer ID namespace
- message IDs are UUIDv7 values stored as 16-byte SQLite `BLOB`s and exposed over the API as strings

### Preview cache DB

Path:

- `data/previews.db`

Purpose:

- URL metadata cache used by the inline preview feature (images + OpenGraph)

Why it is a separate database:

- the cache is global, not per-network — a link reposted across networks or channels must resolve to one fetch, not N
- it has no foreign-key relationship to any per-network log, so keeping it out of those files lets each log DB stay self-contained
- the cache is disposable: wiping `previews.db` only forces re-fetching, it does not lose chat history

Important tables:

- `url_previews` — one row per URL, keyed by URL, holds `kind`, `title`, `description`, `image_url`, `site_name`, `width`, `height`, `mime`, `fetched_at`, `error`
- `schema_migrations`

The `kind` column is one of `image`, `opengraph`, `none`, `error`. The last two are negative-result rows that prevent retry storms on URLs that don't preview usefully.

Per-message associations live in the per-network log DB (not here), in a `message_previews(message_id, url, position)` table added by log migration `0002_message_previews.sql`. The API joins the two halves in Go rather than in SQL: group messages by network → read `message_previews` from each log DB → batch-load URL rows from `previews.db`.

Migrations for this DB live in `db/preview_migrations/*.sql` and are applied by `db.OpenPreviews`. The store is owned by `MultiStore.Previews` alongside `Control` and the per-network `logs` map; closing `MultiStore` closes all three.

### Why three storage layers exist

The control DB handles global coordination:

- network metadata
- network ordering
- global buffer registry

The per-network DBs handle network-local data:

- message logs
- channel state
- read state
- per-message preview URL associations

The preview DB handles cross-network cache data:

- one URL metadata row shared across every network and channel

This avoids putting all message history for all networks into one DB while still allowing one global API surface, and keeps the expensive/disposable URL cache separate from both.

This project is still greenfield. On-disk databases from before the UUIDv7 storage model are intentionally not migrated; delete old `data/` directories when crossing that boundary.

## Network model

A network is:

- one logical IRC network row in the control DB
- zero or one active IRC client runtime at a time
- one per-network log database on disk

Important assumptions:

- network names are expected to be stable after creation
- rename behavior is conservative because log DB filenames derive from network names
- deleting a network removes it from the control DB and closes its log DB handle, but the log DB file is intentionally retained

## Buffer model

Buffer kinds:

- `status`
- `channel`
- `query`

Global/API view:

- each buffer has a stable UUIDv7 ID from `buffer_registry`
- clients use this buffer ID for history, active buffer selection, and mark-read operations

Per-network/log DB view:

- the per-network `buffers.id` value is the same UUIDv7 as `control.db.buffer_registry.id`
- `MultiStore.EnsureBuffer` is the authoritative creation path and keeps those rows in lockstep
- history, search, and mark-read operations resolve `buffer_id -> network_id -> log DB`, then query by the same buffer UUID

Important invariant:

- clients should treat buffer IDs as globally unique stable string identifiers
