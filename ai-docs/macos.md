# Native Apple client (macOS + iOS)

The native client is a SwiftUI application in `macos/` whose single app target builds for both macOS 26 and iOS 26 from the same sources. It is a first-party Lurker frontend: IRC connections and durable history remain owned by the Go backend. (The directory is still named `macos/` for history; it builds both platforms.)

## Product scope

The app is intended to be useful as the everyday desktop client (with the same sources also serving iPhone and iPad):

- one native main window using `NavigationSplitView`
- networks and buffers in the source sidebar
- message history and live updates in the content area
- optional channel-member inspector
- unread and mention counts, persisted read state, pinned buffers, and archived channels
- common slash commands: `/me`, `/join`, `/part`, `/query`, `/msg`, `/nick`, `/whois`, `/away`, `/back`, and `/topic`
- link previews and presence-event display settings
- mention notifications and an app-icon badge count (Dock tile on macOS, notification badge on iOS)
- native menus and keyboard shortcuts on macOS, including the Command-K channel switcher

Network administration, global history search, uploads, and theme selection remain web-only for now.

## Project and identity

- Xcode project: `macos/Lurker.xcodeproj`
- application sources: `macos/Lurker/`
- unit tests: `macos/LurkerTests/`
- UI smoke tests: `macos/LurkerUITests/`
- bundle identifier: `xyz.endymion.lurker`
- deployment targets: macOS 26 and iOS 26 (`SDKROOT = auto`, `SUPPORTED_PLATFORMS = "iphoneos iphonesimulator macosx"`, `TARGETED_DEVICE_FAMILY = "1,2"`)
- language mode: Swift 6 with complete concurrency checking

The UI follows the web client's information hierarchy, not its CSS. It uses system appearance, materials, controls, typography, split views, settings, menus, notifications, and accessibility behavior so it remains a native application on each platform.

## Cross-platform structure

SwiftUI sources are shared; platform differences are isolated:

- `Platform.swift` holds the shim layer: semantic `Color` helpers (`.lurkerTimelineBackground`, `.lurkerSeparator`, `.lurkerControlBackground`, `.lurkerLink`) resolving to `NSColor` on macOS / `UIColor` on iOS, and `Clipboard.copy(_:)` wrapping `NSPasteboard` / `UIPasteboard`. Keep color/clipboard `#if os(...)` branching confined to this file so views stay platform-agnostic; do not reintroduce unguarded AppKit or UIKit usage in shared views.
- `LurkerApp.swift` splits scenes: macOS keeps `Window` + `Settings` scene + menu-bar `LurkerCommands`; iOS uses a `WindowGroup`, and settings is an in-app sheet (`AppModel.showingSettings`) because iOS has no `Settings` scene.
- `RootView.swift` picks the layout: `NavigationSplitView` on macOS and iPad regular width; on iPhone (compact width) a `NavigationStack` where selecting a buffer pushes the conversation (`AppModel.compactConversationVisible`, set in `selectBuffer` so the channel switcher and notification taps also push). Compact width puts the connection-status and channel-switcher buttons in the sidebar navigation bar and the Members toggle on the conversation screen; the members list presents as a sheet there, and the inspector default is hidden on iOS.
- The members-inspector visibility binding routes through `setInspectorVisible` so interactive dismissal persists to `UserDefaults`.
- macOS-only signing settings (`CODE_SIGN_ENTITLEMENTS`, `ENABLE_APP_SANDBOX`, `ENABLE_HARDENED_RUNTIME`) are scoped `[sdk=macosx*]` in the project so iOS builds do not inherit the macOS sandbox entitlements.
- `Info.plist` carries the iOS keys (`UILaunchScreen`, all four `UISupportedInterfaceOrientations` — required for iPad multitasking validation) alongside the macOS ones; `NSLocalNetworkUsageDescription` is what iOS needs to reach the bouncer over the local network / Tailnet.
- Test targets (`LurkerTests`, `LurkerUITests`) build for macOS only (mac `TEST_HOST`).

Known iOS follow-ups: real background push needs APNs plus server support (today notifications are local, foreground-only, because iOS suspends the WebSocket in the background), and small-screen polish such as swipe-between-buffers.

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

For iOS, open `macos/Lurker.xcodeproj` in Xcode, pick the **Lurker** scheme with an iPhone/iPad Simulator destination, and build (a free Apple ID team is fine for the simulator). Point Settings at your bouncer URL — localhost for the simulator, your Tailnet MagicDNS name on a real device.

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
