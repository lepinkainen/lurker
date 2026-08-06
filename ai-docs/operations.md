# Operations notes

## Update checker

Lurker polls the GitHub Actions API for the latest successful `release.yml` run and compares its head SHA with the commit baked into the running binary (`main.gitHash`, set from the same SHA by the Docker build). A mismatch means a newer image is published. One unauthenticated request per interval; it does not pull images, restart containers, or require Docker socket access.

### Configuration

Environment variables:

- `UPDATE_CHECK_ENABLED` default `true`
- `UPDATE_CHECK_INTERVAL` default `24h`, clamped to minimum `1h`

### API

Endpoint:

- `GET /api/update-status`

Example response:

```json
{
  "enabled": true,
  "remote_version": "def4567",
  "checked_at": "2026-04-28T08:05:00Z",
  "update_available": true
}
```

### Implementation notes

- source of truth is `GET https://api.github.com/repos/lepinkainen/lurker/actions/workflows/release.yml/runs?status=success&per_page=1` — a successful release run means the image was pushed, so failed builds never produce false positives
- `remote_version` is the short commit SHA of the latest published image
- dev builds (`gitHash` empty or `unknown`) never report an update
- failures are surfaced in status `error` field and logs, but do not fail app startup

## Media storage

Uploaded images are optimized in-process (`media/transcode.go`) and then handed to a **blob backend**
named explicitly in `config.yaml`. Provisioning walkthrough for the S3 path, including the OpenTofu
config: [`S3_SETUP.md`](../S3_SETUP.md).

### Configuration

```yaml
media:
  backend: s3              # "s3" | "disk" — required when the block is present
  max_bytes: 20971520      # optional, default 20 MiB
  s3:
    endpoint: s3.eu-north-1.amazonaws.com      # no scheme
    region: eu-north-1
    bucket: lurker-media
    access_key_id: ${AWS_ACCESS_KEY_ID_LURKER}
    secret_access_key: ${AWS_SECRET_ACCESS_KEY_LURKER}
    public_base_url: https://cdn.example.com   # CloudFront domain, not the S3 host
    prefix: ""
    path_style: false
  disk:
    dir: ./data/uploads
    base_url: ""           # optional; empty derives the URL from the request
```

`${VAR}` values are expanded from the environment, so credentials never live in the file.

There are no `UPLOAD_DIR` / `UPLOAD_MAX_BYTES` / `UPLOAD_BASE_URL` env vars — they were removed with
the implicit-disk model. `data/media.db` (metadata, SHA-256 dedup) is unaffected by the backend choice.

### Backend selection is explicit, and there is no fallback

| Situation | Behavior |
| --- | --- |
| No `media:` block | `WARN` at boot, uploads disabled, `POST /api/upload` → `404`. Bouncer runs normally. |
| Block present, backend unnamed/unknown/incomplete | `ERROR` + `exit(1)`. Same class as a broken `config.yaml`. |
| `backend: s3`, bucket unreachable at boot | `ERROR` at boot naming the bucket; **process keeps running** so IRC stays up. Uploads → `502` until fixed. |
| `backend: s3` healthy | `INFO media storage backend=s3 …`; variants PUT to the bucket, nothing on local disk. |
| `backend: disk` | Variants under `media.disk.dir`, served by `GET /uploads/{name}`. |

A failing S3 backend never falls back to local disk. That fallback is precisely how a misconfigured
deploy quietly fills a server's drive with media, so it does not exist.

### Object layout and caching

- Key is `[<prefix>/]<base62-id><ext>` — the same key recorded in `media.db`.
- Every PUT sets `Cache-Control: public, max-age=31536000, immutable`; keys embed a random id and are
  never rewritten, so the bytes behind a URL never change.
- Boot probes the bucket with `BucketExists` (needs `s3:ListBucket`) under a 10s timeout.

### Topology (S3 backend)

Bucket is **fully private** (Block Public Access on, ACLs disabled). Public reads go only through
CloudFront, which reads the bucket via OAC with a bucket policy scoped to that one distribution ARN.
`public_base_url` is the CloudFront hostname; with the S3 backend there is no local copy, so a
request-derived URL would be a dead link — hence `public_base_url` is a required key.

### Operational caveats

- **Anything uploaded is publicly fetchable** by anyone holding the URL — that is the point, other IRC
  members must load it. The 10-char base62 ids make URLs unguessable, not secret.
- **`DELETE /api/media/{id}` does not purge the CDN.** It removes the metadata row and the bucket
  object; an edge-cached copy can survive until its TTL. No automatic invalidation.
- **Orphan cleanup is manual.** A failed upload records nothing (no row, no object), but nothing
  reconciles bucket contents against `media.db` on a schedule. The `DELETE` endpoint and the web
  Settings → Media library browser are the tools.
- Backing up `data/` still covers all chat data; media bytes live in the bucket and are not part of it.
