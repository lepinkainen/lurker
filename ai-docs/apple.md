# Native Apple client (macOS + iOS)

The native client is a SwiftUI application in `apple/` whose single app target builds for both macOS 26 and iOS 26 from the same sources. It is a first-party Lurker frontend: IRC connections and durable history remain owned by the Go backend.

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
- image attachment: composer paperclip (macOS `.fileImporter` / iOS `PhotosPicker`) plus cross-platform drag&drop, gated on `canSend`. The image uploads to `POST /api/upload` and the returned URL is inserted into the composer. HEIC and other non-web formats are converted to JPEG client-side (ImageIO, EXIF orientation baked into pixels); JPEG/PNG/GIF pass through untouched. Requires the `files.user-selected.read-only` sandbox entitlement for the macOS picker (drag&drop does not).

Network administration, global history search, and theme selection remain web-only for now.

## Project and identity

- Xcode project: `apple/Lurker.xcodeproj`
- application sources: `apple/Lurker/`
- unit tests: `apple/LurkerTests/`
- UI smoke tests: `apple/LurkerUITests/`
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
task build-apple
open build/DerivedData/Build/Products/Debug/Lurker.app
```

For iOS, open `apple/Lurker.xcodeproj` in Xcode, pick the **Lurker** scheme with an iPhone/iPad Simulator destination, and build (a free Apple ID team is fine for the simulator). Point Settings at your bouncer URL — localhost for the simulator, your Tailnet MagicDNS name on a real device.

The app icon PNGs in `apple/Lurker/Assets.xcassets/AppIcon.appiconset/` are committed and used as-is by the build. They are generated from `web/public/favicon.svg`; regenerate them with `task apple-icon` only when that source changes (requires `rsvg-convert` — `brew install librsvg`).

Run checks:

```bash
task lint-apple
task test-apple
task test-apple-ui
```

The UI test launches with `-ui-testing`, which replaces network access with deterministic in-process fixture data and suppresses notification authorization prompts.
It signs the local test runner ad hoc and requires Xcode to have UI automation permission in System Settings.

## Deferred: hand cursor over inline links (macOS)

Preview cards show the pointing-hand cursor via a plain `.pointerStyle(.link)` on the card button (`ConversationView.swift`, `PreviewCard`). Doing the same for inline URLs *inside* message text was implemented and then dropped as too complex for the payoff. Recorded here in case it becomes worth it later.

Why it is hard:

- SwiftUI has no per-character-range pointer API; `.pointerStyle` applies to a whole view.
- `.textSelection(.enabled)` re-asserts the I-beam cursor on every pointer move, so a static pointer style is overridden anyway.
- Selectable `Text` bypasses custom `TextRenderer`s, so you cannot observe link-run geometry on the visible text directly.

The working approach (all `#if os(macOS)`, in `MessageRow`):

1. Mark link runs. Rebuild the `AttributedString` as concatenated `Text` pieces; pieces whose run has `.link` get `.customAttribute(LinkRunAttribute())` (an empty `TextAttribute` struct).
2. Record their rects. Overlay that rebuilt text on the visible selectable `Text` with identical font, plus `.allowsHitTesting(false)` and `.accessibilityHidden(true)`. Give the overlay a `TextRenderer` whose `draw` walks `layout` lines/runs, collects `run.typographicBounds.rect` for runs carrying `LinkRunAttribute`, and stores them in an `NSLock`-guarded box object (renderer draws off the main path; the box makes it `Sendable`). The renderer draws nothing — the visible twin underneath renders.
3. Flip the cursor manually. `.onContinuousHover(coordinateSpace: .local)` on the visible text: on `.active`, hit-test the point against the recorded rects and call `NSCursor.pointingHand.set()` on hit / `NSCursor.iBeam.set()` when leaving a hit (must re-set on *every* move because the selectable text keeps re-asserting I-beam); on `.ended`, restore `NSCursor.arrow` if a link was hovered. Track the previous hit in a `@State private var hoveringLink`.

State lived as `@State private var linkRects = LinkRunRects()` on `MessageRow`. Fragile points: the twin must match layout exactly (same font modifiers, same wrapping width), and cursor churn during scroll needs care.

UI-test technique (also removed, `LurkerUITests.swift` history): link runs are not separate accessibility elements, so the test swept `coordinate(withNormalizedOffset:)` hover stops across the message row and compared `NSCursor.currentSystem?.image.tiffRepresentation` against `NSCursor.pointingHand` at each stop, asserting a hit somewhere over the link text and no hit over a plain message. The preview-card variant of that sweep test is still in the suite.

Full implementation: see the pre-removal diff of `ConversationView.swift` (git history of this branch, removed together with this section's addition).

## Signed distribution

The manual update format is a Developer ID-signed, notarized Apple silicon DMG. First store notary credentials in the keychain:

```bash
xcrun notarytool store-credentials lurker-notary
```

Then package:

```bash
export LURKER_DEVELOPER_ID='Developer ID Application: Example (TEAMID)'
export LURKER_NOTARY_PROFILE='lurker-notary'
task package-apple
```

The task performs a clean Release build, signs the app with hardened runtime and its sandbox entitlements, creates and signs a DMG, submits it to Apple's notary service, and staples the ticket. Output is written to `build/Lurker-<version>.dmg`. Updates are installed manually by replacing the application.
