# iOS port (prototype)

This directory started as the macOS SwiftUI client. It has been restructured so
the **same target and the same sources build for iOS as well** — SwiftUI is
cross-platform, so there is no WebView and no second codebase. All platform
differences are isolated behind `#if os(macOS)` / `#if os(iOS)`.

> Status: prototype. The source changes were written and reviewed but **not
> compiled** on an Apple toolchain (the branch was prepared in a Linux CI
> environment). Open in Xcode, pick an iOS Simulator destination, and build.
> Anything that doesn't compile should be a small, localized fix.

## What changed

**New file**
- `Lurker/Platform.swift` — cross-platform shims. Semantic `Color` helpers
  (`.lurkerTimelineBackground`, `.lurkerSeparator`, `.lurkerControlBackground`,
  `.lurkerLink`) that resolve to `NSColor` on macOS / `UIColor` on iOS, plus a
  `Clipboard.copy(_:)` wrapping `NSPasteboard` / `UIPasteboard`.

**Sources made cross-platform**
- `LurkerApp.swift` — scene split by platform. macOS keeps `Window` +
  `Settings` scene + the menu-bar `LurkerCommands`. iOS uses a `WindowGroup`.
- `ConversationView.swift` — dropped `import AppKit`; colors/clipboard now go
  through `Platform.swift`.
- `MembersInspector.swift` — clipboard via `Clipboard.copy`.
- `NotificationManager.swift` — AppKit import gated; `setDockBadge` →
  `setBadge` (dock tile on macOS, `UNUserNotificationCenter.setBadgeCount` on
  iOS); `NSApp.activate()` gated to macOS.
- `AppModel.swift` — removed the unused `import AppKit`; added `showingSettings`
  (iOS has no `Settings` scene, so settings is an in-app sheet).
- `SidebarView.swift` — macOS `SettingsLink` vs. an iOS gear button that sets
  `showingSettings`; macOS-only `.menuStyle(.borderlessButton)` gated.
- `RootView.swift` — iOS-only settings sheet (`SettingsView` wrapped in a
  `NavigationStack` with a Done button).
- `SettingsView.swift` / `ConnectionEditor` — iOS URL-keyboard niceties;
  macOS-only fixed widths gated.
- `ChannelSwitcher.swift` — macOS double-click vs. iOS single-tap to open;
  macOS-only fixed frame gated.

**Project / bundle**
- `Lurker.xcodeproj` app target: `SDKROOT = auto`,
  `SUPPORTED_PLATFORMS = "iphoneos iphonesimulator macosx"`,
  `IPHONEOS_DEPLOYMENT_TARGET = 26.0`, `TARGETED_DEVICE_FAMILY = "1,2"`.
  macOS-only signing settings (`CODE_SIGN_ENTITLEMENTS`, `ENABLE_APP_SANDBOX`,
  `ENABLE_HARDENED_RUNTIME`) are scoped with `[sdk=macosx*]` so iOS doesn't
  inherit the macOS sandbox entitlements.
- `Info.plist` — added `UILaunchScreen` and supported orientations (ignored on
  macOS). `NSLocalNetworkUsageDescription` was already present and is what iOS
  needs to reach the bouncer over the local network / Tailnet.

## Pick up in Xcode

1. Open `macos/Lurker.xcodeproj`.
2. Select the **Lurker** scheme and an **iPhone Simulator** destination. If the
   destination list only shows "My Mac", confirm the target's *Supported
   Destinations* (General tab) lists iPhone/iPad — the build settings above
   should populate it; add it in the GUI if not.
3. Set your Team under Signing & Capabilities for the iOS destination (a free
   Apple ID is fine for the simulator).
4. Build & run. Point Settings at your bouncer URL (localhost for the
   simulator; your Tailnet MagicDNS name on a real device).

## Known follow-ups (not done here)

- **iPhone layout polish.** Layout is driven by `NavigationSplitView`, which
  collapses to a stack on iPhone, and the inspector becomes a sheet. It should
  be usable but wasn't tuned for small screens (spacing, the members inspector,
  and replacing menu-bar shortcuts with touch gestures like swipe-between-
  buffers).
- **Test targets stay macOS-only.** `LurkerTests` / `LurkerUITests` still build
  for macOS (mac `TEST_HOST`). Running the app on iOS is unaffected; only the
  Test action against an iOS destination would need work.
- **Push.** iOS background delivery still relies on `UNUserNotificationCenter`
  (local notifications only, foreground). Real background push needs APNs +
  server support — separate task.
- **Directory name.** Still `macos/` though it now builds both. Rename to
  `apple/` later if desired (updates only the folder, not project internals).
