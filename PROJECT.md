# The purpose for this project is to create a private IRC bouncer with first-party clients

- Backend runs 24/7 in a Docker container and logs messages to sqlite databases, one for each network
- Web, terminal, and native macOS frontends act as normal IRC clients, but connect to the backend to retrieve stored messages and send commands rather than connecting to IRC networks directly
- The application MUST always run behind a trusted private-network layer such as Tailscale or another VPN. It is not intended to be exposed directly to the public internet.
- It's assumed that the front-end connects to the backend securely via Tailscale, so no authentication is needed for the backend. It should run in a Docker container with no ports open to the outside except for the Tailnet.
- This is a single user application. One backend instance serves one user, and the frontend is just a client for that user. No multi-user support is needed. Even if multiple clients are connected, only one human will be operating them, race conditions are not a concern.
- This is a greenfield application, no need to worry about "existing users" or "backwards compatibility". It's OK to make large architectural changes instead of trying to kludge things to work in the old way.

Current clients:

- Web UI
- TUI client
- SwiftUI macOS client

Future client work should improve practical parity while keeping each interface idiomatic to its platform.

## Toolchain policy

Single contributor, single deployment target. Track the latest stable Go release aggressively rather than staying on an old language version for compatibility: bump `go.mod`, the `Dockerfile` builder image, and the version notes in `CLAUDE.md` / `AGENTS.md` together, then run `go fix ./...` (excluding `llm-shared/`) to pick up any modernizations the new language version unlocks. CI reads the version from `go.mod` via `go-version-file`, so it follows automatically.
