# Native macOS client

The native client is a macOS 26 SwiftUI application in `macos/`. It is a first-party Lurker frontend: IRC connections and durable history remain owned by the Go backend.

## Product scope

The app is intended to be useful as the everyday desktop client:

- one native main window using `NavigationSplitView`
- networks and buffers in the source sidebar
- message history and live updates in the content area
- optional channel-member inspector
- unread and mention counts, persisted read state, pinned buffers, and archived channels
- common slash commands: `/me`, `/join`, `/part`, `/query`, `/msg`, `/nick`, `/whois`, `/away`, `/back`, and `/topic`
- link previews and presence-event display settings
- mention notifications and Dock badge count
- native menus and keyboard shortcuts, including the Command-K channel switcher

Network administration, global history search, uploads, and theme selection remain web-only for now.

## Project and identity

- Xcode project: `macos/Lurker.xcodeproj`
- application sources: `macos/Lurker/`
- unit tests: `macos/LurkerTests/`
- UI smoke tests: `macos/LurkerUITests/`
- bundle identifier: `xyz.endymion.lurker`
- deployment target: macOS 26
- language mode: Swift 6 with complete concurrency checking

The UI follows the web client's information hierarchy, not its CSS. It uses system appearance, materials, controls, typography, split views, settings, menus, notifications, and accessibility behavior so it remains a native macOS application.

## Connection and trust model

The client accepts one base endpoint and uses the backend's existing REST and WebSocket APIs without server changes. It validates `/whoami` before saving a server and also checks `/api/tailscale-status`.

Only plain HTTP endpoints in the following locations are accepted:

- `localhost`, loopback IPv4, or IPv6 loopback
- Tailscale's `100.64.0.0/10` address range
- a single-label MagicDNS hostname
- a hostname ending in `.ts.net`

Credentials, paths, queries, and fragments are rejected. App Transport Security permits HTTP because the deployment model is a trusted private network; `EndpointPolicy` applies the narrower application-level destination restriction. The app is sandboxed with outbound network access only. It does not add authentication and must not be pointed at a publicly exposed Lurker service.

## Hydration and live events

`AppModel` opens the WebSocket stream before fetching `/api/state`. Events received during hydration are queued, the snapshot is installed, and queued events are then applied in order. This avoids losing messages in the state-fetch window.

The model:

- treats server UUIDs as stable identifiers
- deduplicates messages by ID
- maintains network, buffer, message, and member dictionaries
- reconnects the stream with backoff
- requests older history when the user reaches the top
- sends read markers through the existing WebSocket command
- stores only client-local preferences and the selected endpoint in `UserDefaults`

## Development

Build and open the app:

```bash
task build-macos
open build/DerivedData/Build/Products/Debug/Lurker.app
```

The app icon PNGs in `macos/Lurker/Assets.xcassets/AppIcon.appiconset/` are committed and used as-is by the build. They are generated from `web/public/favicon.svg`; regenerate them with `task macos-icon` only when that source changes (requires `rsvg-convert` — `brew install librsvg`).

Run checks:

```bash
task lint-macos
task test-macos
task test-macos-ui
```

The UI test launches with `-ui-testing`, which replaces network access with deterministic in-process fixture data and suppresses notification authorization prompts.
It signs the local test runner ad hoc and requires Xcode to have UI automation permission in System Settings.

## Signed distribution

The manual update format is a Developer ID-signed, notarized Apple silicon DMG. First store notary credentials in the keychain:

```bash
xcrun notarytool store-credentials lurker-notary
```

Then package:

```bash
export LURKER_DEVELOPER_ID='Developer ID Application: Example (TEAMID)'
export LURKER_NOTARY_PROFILE='lurker-notary'
task package-macos
```

The task performs a clean Release build, signs the app with hardened runtime and its sandbox entitlements, creates and signs a DMG, submits it to Apple's notary service, and staples the ticket. Output is written to `build/Lurker-<version>.dmg`. Updates are installed manually by replacing the application.
