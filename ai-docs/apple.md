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
- channel moderation: `/mode`, `/op`, `/deop`, `/voice`, `/devoice`, `/kick`, `/kickban`, `/ban`, `/unban`, and `/banlist` slash commands, plus Op/Voice/Kick actions in the member-list context menu (Op/Voice toggle to Deop/Devoice based on the member's current prefix)
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

- `Platform.swift` holds the shim layer: semantic `Color` helpers (`.lurkerTimelineBackground`, `.lurkerSeparator`, `.lurkerControlBackground`, `.lurkerLink`) resolving to `NSColor` on macOS / `UIColor` on iOS, and `Clipboard.copy(_:)` wrapping `NSPasteboard` / `UIPasteboard`. Keep color/clipboard `#if os(...)` branching confined to this file so views stay platform-agnostic; do not reintroduce unguarded AppKit or UIKit usage in shared views. The one sanctioned exception is `TimelineTextView.swift`: the macOS message timeline is an AppKit `NSTextView` by design (see "macOS timeline architecture" below), and that file is wholly `#if os(macOS)`.
- `LurkerApp.swift` splits scenes: macOS keeps `Window` + `Settings` scene + menu-bar `LurkerCommands`; iOS uses a `WindowGroup`, and settings is an in-app sheet (`AppModel.showingSettings`) because iOS has no `Settings` scene.
- `RootView.swift` picks the layout: `NavigationSplitView` on macOS and iPad regular width; on iPhone (compact width) a `NavigationStack` where selecting a buffer pushes the conversation (`AppModel.compactConversationVisible`, set in `selectBuffer` so the channel switcher and notification taps also push). Compact width puts the connection-status and channel-switcher buttons in the sidebar navigation bar and the Members toggle on the conversation screen; the members list presents as a sheet there, and the inspector default is hidden on iOS.
- The members-inspector visibility binding routes through `setInspectorVisible` so interactive dismissal persists to `UserDefaults`.
- macOS-only signing settings (`CODE_SIGN_ENTITLEMENTS`, `ENABLE_APP_SANDBOX`, `ENABLE_HARDENED_RUNTIME`) are scoped `[sdk=macosx*]` in the project so iOS builds do not inherit the macOS sandbox entitlements.
- `Info.plist` carries the iOS keys (`UILaunchScreen`, all four `UISupportedInterfaceOrientations` — required for iPad multitasking validation) alongside the macOS ones; `NSLocalNetworkUsageDescription` is what iOS needs to reach the bouncer over the local network / Tailnet.
- Test targets (`LurkerTests`, `LurkerUITests`) build for macOS only (mac `TEST_HOST`).

Known iOS follow-ups: real background push needs APNs plus server support (today notifications are local, foreground-only, because iOS suspends the WebSocket in the background), and small-screen polish such as swipe-between-buffers.

## Theme

`Theme.swift` centralizes the app's type scale and a handful of recurring layout metrics (row insets, sidebar child indent, badge padding). `Theme.Fonts` tokens are named for role, not appearance (`nick`, `message`, `timestamp`, `badge`, `sectionHeader`, `smallIcon`), since several resolve to the same underlying `Font` value but mark distinct call sites (e.g. `nick` and `message` are both `.body.monospaced()` today but are free to diverge later). Scale always comes from semantic text styles (`.caption`…`.title3`), never a hardcoded point size; emphasis is weight, de-emphasis is color (`.secondary`/`.tertiary`). New UI should reach for a `Theme` token instead of a literal font or padding value; add a new token only once a value is genuinely reused for the same role (roughly: the third occurrence), not for a one-off.

Hover-revealed affordances (macOS only) follow an opacity-fade rule, never insert-on-hover: the control stays in the view tree at a fixed size and only its opacity changes with `.onHover`, so the row never re-lays-out or jitters. `NetworkHeaderRow`'s overflow menu (`SidebarView.swift`) is the current example — visible only while the row is hovered on macOS, always visible on iOS (there is no hover there, so the `#if os(macOS)` opacity/`.onHover` pair is skipped entirely).

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

Swift formatting follows the [Airbnb Swift style guide](https://github.com/airbnb/swift), enforced deterministically by SwiftFormat (`brew install swiftformat`) with the config vendored at `apple/airbnb.swiftformat`. `task lint-apple` checks (CI runs this); `task format-apple` rewrites sources in place — run it instead of hand-fixing style complaints. Notable rules: 2-space indent, un-indented `#if` bodies, member ordering with `// MARK:` sections (`organizeDeclarations`), `@Test` display names derived from function names, `try #require(...)` instead of force unwraps in tests, 130-column hard wrap (upstream recommends 100 but does not enforce it). Formatter version is pinned: `--minversion` in the config and a pinned release download in the CI apple job (bump both together); CI prints `swiftformat --version` and `swift --version` so a rule-output mismatch is diagnosable at a glance.

The UI test launches with `-ui-testing`, which replaces network access with deterministic in-process fixture data and suppresses notification authorization prompts.
It signs the local test runner ad hoc and requires Xcode to have UI automation permission in System Settings.

## macOS timeline architecture (NSTextView)

On macOS the message timeline is a single AppKit `NSTextView` using TextKit 2 (`TimelineTextView.swift`, wholly `#if os(macOS)`); iOS keeps the SwiftUI `TimelineView` (ScrollView + LazyVStack) in `ConversationView.swift`. The rewrite exists because SwiftUI `Text` link hit-testing is unreliable in wrapped multi-link messages (clicks opened the wrong URL), a per-range hand cursor is impossible over selectable SwiftUI text, and `.textSelection(.enabled)` cannot select across rows. The text view gives exact link targets, the pointing-hand cursor (`linkTextAttributes`), and cross-row copy natively — the previously documented twin-`Text` cursor workaround is obsolete and was deleted with this section's predecessor.

Structure:

- `MacTimelineContainer` (SwiftUI) pins the `UnreadBar` via `safeAreaInset` and floats the history-loading spinner; `TimelineTextView` is the `NSViewRepresentable` (`NSScrollView` + `TimelineNSTextView(usingTextLayoutManager: true)`).
- **TextKit 2 rule: never touch `textView.layoutManager`** — reading it silently downgrades to TextKit 1. Use `textLayoutManager`/`textContentStorage` only.
- Timeline derivation (`timelineItems` in `ConversationView.swift` — day separators, unread separator, presence grouping) is shared between the iOS view and the macOS coordinator.
- `TimelineCoordinator` keeps a rendered-block table (one block per `TimelineItem`) and applies minimal storage edits from the pure `TimelineDiff`: identical id lists → in-place block replacement (preview arrival, netsplit tag, presence-run growth); strict-suffix id lists → append; anything else (buffer switch, history prepend, settings change) → full rebuild.
- Each message is one paragraph: `\t` + timestamp (right tab stop at the 42pt gutter) + `\t` + nick + body, `headIndent` aligning wrapped lines under the nick column. Custom attributes: `.lurkerMessageID` (context-menu lookup), `.lurkerCopyExclude` (rows dropped from Copy).
- Scrolling: rebuilds land at the bottom; a new-message append auto-scrolls **only when the viewport is already near the bottom** (deliberate change from the SwiftUI timeline's unconditional jump — scrolled-up reading position is preserved, the unread bar still shows). Every height-changing edit re-pins when the viewport was near the bottom, not just appends: replacement-only diffs (late preview on the last message) and async inline-image growth (`PreviewAttachmentResizeRelay` checks the coordinator's pin state before invalidating layout) — otherwise the grown document leaves the viewport past the near-bottom threshold and auto-follow silently dies. Older-history loads trigger from the clip-view bounds notification (viewport near the top edge), and the `historyAnchor` restore pins the previously-first visible message back to the top after a prepend, same contract as the SwiftUI path.
- Copy is sanitized (`TimelineNSTextView.copy`): `.lurkerCopyExclude` runs and attachment placeholders are dropped, the tab gutter flattens to spaces, so pasted lines read `HH:MM nick body` — cross-row selection includes timestamps and nicks.
- Context menu (Copy Message / Copy Nickname / Mute / Unmute) maps the click's character index to its message via the block table; nick tooltips use the `.toolTip` attribute.
- Esc-to-ack works when the text view is first responder via `cancelOperation`, mirroring `ConversationView`'s `.onKeyPress(.escape)`.
- Accessibility: each message is one AX row (`AXStaticText`, label "sender, time, content" — the SwiftUI timeline's combined-label contract) exposed through `TimelineAXHostView`, a transparent sibling overlay of the scroll view. Two constraints discovered the hard way: NSTextView's legacy accessibility machinery ignores a subclass's modern `accessibilityChildren()` override (hence the separate host view), and the coordinator must hold strong references to the row elements — AX clients resolve them after the children query returns, so unretained elements deallocate and get silently pruned.
- Rich content: nick avatars are 14×14 `NSTextAttachment` images (CoreGraphics identicon / 🤖 text for bots / rounded server avatar swapped in by `kickAvatarLoads` once `ImageCache` resolves it — rows re-render via block replacement). Bot/avatar state lives in `AppModel`, not in `TimelineItem`, so the item diff can't see it change: each rendered block stores an `avatarKey` (bot / cached image URL / identicon) and `refreshStaleAvatarRows` re-renders mismatched rows on every sync — including a `.none` diff — covering member-list/metadata events that land after the rows rendered. System rows get a tinted SF Symbol attachment; preview cards and inline images are the **shared SwiftUI `PreviewCard`/`InlineImageView`** hosted through `NSTextAttachmentViewProvider` + `NSHostingView` in their own copy-excluded paragraphs (a late `.preview` event is a plain block replacement; an inline image growing from placeholder invalidates layout through `PreviewAttachmentResizeRelay`). Full-row mention highlight and the separator hairline rules draw in `LurkerLayoutFragment` (delegate-supplied for paragraphs tagged `.lurkerRowHighlight`/`.lurkerSeparatorRule`).
- Presence groups expand in place: the summary line carries a `lurker-presence://<first-member-uuid>` link (hand cursor + activation for free); `clickedOnLink` toggles the id in the coordinator's `expandedPresenceGroups` (survives rebuilds, unlike the iOS DisclosureGroup's @State; cleared on buffer switch) and `timelineItems(_:buffer:expandedGroups:)` emits the member rows after the `▾` summary. Expanding a **terminal** group is a pure suffix append where the summary item compares equal, so `clickedOnLink` re-renders the toggled summary block explicitly — the diff alone would leave the arrow at `▸`. `linkTextAttributes` sets only the cursor so link styling stays per-run (blue message links, secondary presence toggle).

UI-test technique for cursor feedback (`testPointerBecomesHandOverInlineLink`): link runs are not separate accessibility elements, so the test sweeps `coordinate(withNormalizedOffset:)` hover stops across the timeline and compares `NSCursor.currentSystem?.image.tiffRepresentation` against `NSCursor.pointingHand`.

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
