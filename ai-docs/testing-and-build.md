# Testing and build

## Testing strategy

For IRC package tests, prefer unit tests that inject synthetic `girc.Event` values or fake connection hooks over socket-level fake IRC servers.

Rationale:

- Lurker should test its own translation layer and state management
- the `girc` library is treated as trusted for protocol parsing and wire-level behavior
- fast deterministic tests are preferred over in-process network servers where possible

This means tests should primarily cover:

- IRC event -> SQLite persistence
- IRC event -> hub publication
- manager lifecycle/state transitions
- retry/failover selection logic via injected connector seams

Only add true transport-level integration tests when validating behavior that is specifically about Lurker's own network integration rather than `girc` internals.

## Build and developer workflow

Preferred commands come from `Taskfile.yml`:

- `task dev`
- `task dev-web`
- `task web-install`
- `task web-dev`
- `task web-build`
- `task test`
- `task lint`
- `task build`
- `task up`
- `task down`
