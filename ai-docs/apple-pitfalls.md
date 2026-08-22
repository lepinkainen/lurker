# Apple client SwiftUI/macOS pitfalls

Hard-won, reproduced-in-this-repo landmines for the native client
(`apple/Lurker/`). Each entry: symptom, why it happens, the fix as applied
here, how to verify. For general SwiftUI guidance that isn't Lurker-specific,
see `llm-shared/languages/swiftui.md`. See `apple.md` for the client's
architecture and product scope.

## Sidebar `List` crashes Xcode Previews

**Symptom.** A `List(selection:)` with `.listStyle(.sidebar)` and `Section`s
crashes the Xcode Previews host on Xcode 26 / macOS 26.5 — fatal error in
`TableViewListCore_Mac2.swift` / `ViewListTree.visitItem(force:)` during the
outline `expandItem` / key-view-loop path. Reproduced with a minimal
`.sidebar` List + Sections + a selection binding and zero app data, so this is
a Previews framework bug, not app code. The running app is unaffected.

**Why.** The Previews window's `recalculateKeyViewLoop` force-expands the
`NSOutlineView` backing the sidebar, and the selection-update diff asserts.
This fires even when the bound selection value is `nil` — the mere presence
of a selection binding triggers it. A `List` with no selection binding
renders fine in Previews.

**Fix.** `SidebarView.body` renders `sidebarList`, which branches on
`ProcessInfo.isPreviewOrUITest`: Previews get `List { sidebarContent }` (no
selection binding), the real app gets `List(selection:) { sidebarContent }`.
Content is factored into `@ViewBuilder sidebarContent` so both paths share
one body. `SettingsLink` is also guarded out of Previews — it needs a
`Settings` scene the preview host doesn't provide. `AppModel.preview()`
hydrates synchronously via `FixtureTransport.snapshot()` instead of calling
`start()`, because the async connect loop makes the outline diff an
empty-to-populated transition mid-animation — a second, independent crash
path through the same outline machinery.

**Verify.** Open `SidebarView.swift` in Xcode Canvas; it must render without
the Previews process crashing. If it crashes again, check
`ProcessInfo.isPreviewOrUITest` is still gating the `List` initializer choice
and that no new `Section`-bearing sidebar List was added without the same
branch.

**Debugging tip.** Read the newest
`~/Library/Logs/DiagnosticReports/Lurker-*.ips`, parse with Python (skip the
header line before the JSON body), and dump the crashed thread's
`frames[].symbol`. This is what identified the exact crash path each time —
guessing from the visible SwiftUI code alone does not get you there, since
the crash is inside private AppKit/Previews internals.

## Overlay float-up (nick-autocomplete popup)

**Symptom.** `.overlay(alignment: .topLeading) { panel.alignmentGuide(.top) {
$0[.bottom] + 4 } }`, used to float the composer's nick-autocomplete popup
above the input bar, instead rendered the panel directly on top of the bar
and grew downward off the window (measured: popup row `y≈895` vs composer
`y≈900` — the popup landed *below* its anchor, not above it).

**Why.** `alignmentGuide(.top)` inside a `.topLeading` overlay repositions the
panel's *top* edge relative to its own frame, not relative to the anchor's
top edge in the direction you want — it does not give you "grow upward from
here" for free. There is no cheap alignment-guide trick that reliably floats
a view above an anchor of unknown height.

**Fix.** Measure the panel's height with a `GeometryReader` in `.background`
feeding a `PreferenceKey`, then position with `.offset(y: -(height + gap))`,
gated by `.opacity(height > 0 ? 1 : 0)` to hide the pre-measurement frame
(otherwise there's a one-frame flash at the wrong position before the height
is known). See `ComposerPopupHeightKey` and its consumer in
`apple/Lurker/ConversationView.swift`.

**Verify.** XCUITest: log the popup element's `.frame` against the composer's
and the window's frames and assert the popup's `maxY` is at or above the
composer's `minY`. Coordinates are objective; a screenshot alone can miss a
panel that rendered off-window.

## Sandboxed `.fileImporter` needs the read-only entitlement

**Symptom.** The macOS Lurker app runs sandboxed
(`ENABLE_APP_SANDBOX=YES` in `apple/Lurker.xcodeproj/project.pbxproj`,
entitlements at `apple/Lurker/Lurker.entitlements`). A SwiftUI
`.fileImporter` (backed by `NSOpenPanel`) hard-crashes with `EXC_BREAKPOINT`
and a console message about "missing the User Selected File Read app sandbox
entitlement" unless `com.apple.security.files.user-selected.read-only` is
present in the entitlements file.

**Why.** The sandbox denies the file-read grant `NSOpenPanel` needs to hand
back a security-scoped URL, and SwiftUI does not degrade gracefully — it
traps. Drag & drop (`onDrop`) does **not** need this entitlement: dropped
data arrives over the drag pasteboard, which is a different sandbox gate that
Lurker's entitlements already satisfy.

**Fix.** `com.apple.security.files.user-selected.read-only` is present in
`apple/Lurker/Lurker.entitlements`, gating the composer's paperclip
`.fileImporter` image-attachment picker (`RootView`/composer image-attach
flow; iOS uses `PhotosPicker` instead, which has its own grant).

**Verify.** After any entitlements edit, do a **full rebuild and re-sign** —
an already-running process keeps its old sandbox container and won't pick up
the new entitlement. Click the paperclip, pick a file, confirm it attaches
without a crash.

## `ScrollViewReader.scrollTo` no-ops on an id that isn't actually rendered

**Symptom.** Loading older history (pagination) is supposed to pin the
viewport at the previously-oldest message after the page is prepended.
Anchoring on the raw store head (`messages[id].first?.id`) intermittently
failed to scroll at all, leaving the viewport at the top of the grown content
— which re-triggered the load-older `onAppear` in a runaway loop.

**Why.** The timeline renders from a derived, filtered list
(`selectedMessages`), not the raw message store: when presence-event display
is off, presence-kind messages are dropped or collapsed into a single
`PresenceSummary` row keyed by the *first* member's id. If the raw head
happened to be one of those hidden/absorbed messages, `proxy.scrollTo` was
asked to jump to an id with no corresponding view in the tree — a silent
no-op, not an error.

**Fix.** Two ids are tracked separately: the fetch cursor (still the raw
store head — required, or hidden rows would refetch forever) and the scroll
anchor (`AppModel.historyAnchor`, always the first *rendered* item —
message, day separator, or presence-summary id — for whichever timeline
shape is currently in effect). Only the anchor is passed to
`proxy.scrollTo`. See `AppModel.historyAnchor` / `loadOlderHistory()` in
`apple/Lurker/AppModel.swift` and the `onChange(of: model.historyAnchor)`
handler in `TimelineView` (`apple/Lurker/ConversationView.swift`).

The anchor also has to survive a buffer switch mid-fetch: switching away and
back before a `loadOlderHistory()` fetch resolves must not let that fetch set
an anchor for the now-rebuilt, bottom-anchored timeline. `applySelection`
clears any pending anchor and bumps a generation counter that in-flight
fetches check before writing one.

**Verify.** No unit test reproduces the scroll-timing race (it needs real
layout passes); the guard is `loadOlderHistory` requiring `historyAnchor ==
nil` before firing, plus two safety valves — the `onChange` handler drops an
anchor addressed to a different buffer instead of returning early, and
`onDisappear` clears a pending anchor for its own buffer — so a stuck anchor
can't wedge pagination shut. To verify by hand: scroll to the top of a buffer
with >1 page of history repeatedly, including a rapid switch away/back mid-load, and confirm the loading spinner does not spin forever and the
viewport doesn't jump.

## `AsyncImage` cancels its fetch when a `LazyVStack` row scrolls away

**Symptom.** Inline image previews and OpenGraph thumbnails in the timeline
loaded slowly, never retried after a failure, and sometimes only appeared
after a manual scroll nudged the row back into view.

**Why.** SwiftUI's built-in `AsyncImage` ties its fetch to the hosting view's
lifetime via `.task`. In a `LazyVStack` timeline, a row scrolling off-screen
(or the `ScrollView` reflowing during the auto-scroll-to-bottom animation)
tears down and recreates views constantly, cancelling in-flight downloads
before they complete.

**Fix.** `ImageCache` (`apple/Lurker/CachedAsyncImage.swift`) owns the fetch
`Task` itself rather than letting the view own it: concurrent requests for
the same URL share one in-flight task, and a cancelled *caller* await (the
view disappearing) does not cancel the underlying `URLSession` download — it
completes and populates an `NSCache` for whichever view asks next. Failures
are deliberately left uncached so a row that reappears gets to retry.
`CachedAsyncImage` is the drop-in `AsyncImage` replacement built on top of it;
both the inline image preview and the OpenGraph card thumbnail use it.

**Verify.** Scroll a channel with several link previews up and down rapidly
during initial load; every preview should eventually settle instead of
some going blank permanently. `ImageCache.image(for:)` is unit-testable in
isolation (concurrent callers for the same URL, one download).

## `ForEach` needs distinct identity per rendered occurrence, not per model object

**Symptom.** Pinned channels are meant to appear both in a dedicated Pinned
section and under their normal network section. Rendering the same `Buffer`
in both `ForEach` loops (keyed by the buffer's own stable id) produced
misbehaving lazy-stack diffing — SwiftUI's lazy stacks require every rendered
element to have a distinct identity, and the same id appearing twice in the
combined identity space breaks that.

**Why.** `Identifiable`/`ForEach` identity is a rendering concern, not a data
concern. A buffer that is legitimately supposed to show up twice needs two
different ids — one per place it's drawn — not one id reused twice.

**Fix.** `SidebarBufferOccurrence` (`apple/Lurker/SidebarView.swift`) wraps a
`Buffer` with a `Placement` (`.pinned` or `.network(UUID)`) and derives its
`id` from the composite of placement + buffer id. All four sidebar `ForEach`
loops (pinned channels, network channels, queries, archived buffers) map
their arrays through `SidebarBufferOccurrence` before rendering, instead of
iterating `Buffer` directly.

**Verify.** Pin a channel, confirm it renders in both the Pinned section and
under its network with no dropped/duplicated rows and independent hover/drag
state per occurrence. There's a unit test asserting id namespacing and a UI
test asserting the pinned channel renders in both places.

## `scenePhase` doesn't track macOS app activation

**Symptom.** The client shows a "sync banner" when the WebSocket connection
looks stale, prompted by re-probing the connection whenever the user returns
to the app. Using `@Environment(\.scenePhase)` alone, alt-tabbing back to
Lurker on macOS rarely re-triggered the probe.

**Why.** On macOS, `scenePhase` reflects the *window's* visibility/occlusion
state, not the application's foreground/background activation state.
Alt-tab, Mission Control, and Spaces switches routinely leave a window
"active" from `scenePhase`'s point of view while the app itself was
backgrounded and its socket may have died.

**Fix.** `RootView` (`apple/Lurker/RootView.swift`) observes
`NSApplication.didBecomeActiveNotification` /
`didResignActiveNotification` and `NSWorkspace.didWakeNotification` directly
via `.onReceive`, alongside (not instead of) the cross-platform `scenePhase`
handler, and calls `AppModel.setApplicationActive(_:)` from each. This is
`#if os(macOS)`-gated; iOS keeps relying on `scenePhase` alone, where it
correctly tracks foreground/background.

**Verify.** With the app connected, alt-tab away and back (or sleep/wake the
Mac) and confirm the out-of-sync banner appears promptly if the socket had
actually dropped, rather than only after switching buffers or some other
incidental state read.

## `onKeyPress` arrow keys always carry implicit modifiers

**Symptom.** Composer input-history recall (arrow-up/down to cycle previous
messages) never fired. The handler guarded on `press.modifiers.isEmpty`,
which arrow-key presses never satisfy.

**Why.** Arrow keys report non-empty `modifiers` even when the user pressed
no chord — `.function` and `.numericPad` flags are set implicitly by the
platform for arrow/function-row keys. Checking `isEmpty` rejects every arrow
press, not just chorded ones.

**Fix.** Guard on intersection with the actual chord modifiers instead of
emptiness: `press.modifiers.intersection([.command, .option, .control,
.shift]).isEmpty`. See the `.onKeyPress(keys: [.upArrow, .downArrow])`
handler in `apple/Lurker/ConversationView.swift` — modified arrows (⌥↑ etc.)
are reserved for menu-bar shortcuts and still correctly fall through when
this guard is non-empty.

**Verify.** Focus the composer, press bare arrow-up/down and confirm history
recall works; press ⌥↑ and confirm it's ignored by the composer (falls
through to the menu-bar shortcut instead).
