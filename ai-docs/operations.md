# Operations notes

## Update checker

Lurker can poll GHCR for newer published image metadata and expose cached status over HTTP. It does not pull images, restart containers, or require Docker socket access.

### Purpose

Use this to detect that deployed server is behind published container image without giving application power to replace itself.

### Configuration

Environment variables:

- `UPDATE_CHECK_ENABLED` default `true`
- `UPDATE_CHECK_IMAGE` default `ghcr.io/lepinkainen/lurker`
- `UPDATE_CHECK_TAG` default `latest`
- `UPDATE_CHECK_INTERVAL` default `6h`
- `GHCR_USERNAME` optional
- `GHCR_TOKEN` optional

### API

Endpoint:

- `GET /api/update-status`

Example response:

```json
{
  "enabled": true,
  "image": "ghcr.io/lepinkainen/lurker",
  "tag": "latest",
  "current_version": "sha-abc123",
  "current_commit": "abc123",
  "current_build_time": "2026-04-27T12:00:00Z",
  "remote_version": "sha-def456",
  "remote_commit": "def456",
  "remote_build_time": "2026-04-28T08:00:00Z",
  "remote_digest": "sha256:...",
  "checked_at": "2026-04-28T08:05:00Z",
  "update_available": true
}
```

### Implementation notes

- only `ghcr.io/...` images are supported currently
- checker uses OCI registry APIs, not GitHub HTML scraping
- checker resolves multi-arch manifest lists to current runtime OS/arch
- comparison prefers OCI label `org.opencontainers.image.revision`
- fallback comparison uses `org.opencontainers.image.version`
- failures are surfaced in status `error` field and logs, but do not fail app startup
