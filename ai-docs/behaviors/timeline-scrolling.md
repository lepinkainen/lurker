# Timeline Scrolling (Apple client)

Applies to both Apple timelines: the macOS AppKit `TimelineTextView` and the iOS SwiftUI `ConversationView`.

## Purpose

Match what every chat app does (Slack, Discord). When the user is at the bottom they see new messages as they arrive. When they have scrolled up to read backlog, nothing pulls them away from what they are reading, and the unread bar tells them there is more below.

## Model

The timeline has one piece of scroll state per open buffer: **following**, which is true or false.

- **Following** means that when content arrives or changes, the viewport stays pinned to the very bottom of the document, bottom margin included.
- **Reading backlog** means the viewport stays where the user put it.

Only the user changes following, never content changes:

- **User scroll** (wheel, trackpad, scroller drag, Page Up/Down, Home/End, keyboard or VoiceOver navigation). When the scroll settles, set following = **some part of the latest row is inside the viewport**. Even a single visible pixel counts. The latest row is the last rendered timeline row of any kind, together with its preview card or inline image.
- **Explicit "go to bottom" actions** (Esc, unread-bar activation, sending a message, switching buffers) set following = true and snap to the bottom.

Content changes never set or clear following. They include appends, replacements, a preview or image arriving or resizing, a presence group collapsing or expanding, message removal, window resize, and the unread bar appearing or disappearing. Each change reads the following value as it was **before** the change and acts on it. It never re-derives following from the post-change geometry, because a document that shrinks or grows under a still viewport can make a backlog reader look "at the bottom", or a follower look "scrolled away".

Programmatic scrolls do not count as user scrolls, so they never recompute following. These are pins to the bottom and anchor restores after a history prepend.

## Expected

| Event | Following | Reading backlog |
|---|---|---|
| New message from someone else | Snap to the bottom | No movement. The unread bar shows or its count grows |
| Own message sent | Snap to the bottom | Snap to the bottom, following = true |
| Height change to existing content (late preview, image growth, presence toggle, removal) | Stay pinned to the bottom | The top visible row keeps its on-screen position |
| Viewport resize (window resize, unread bar inset, keyboard on iOS) | Stay pinned to the bottom | The top visible row keeps its on-screen position |
| Esc / unread-bar activation | Ack the marker (already at the bottom) | Ack the marker, snap to the bottom, following = true |
| Buffer switch | Land at the bottom, following = true | Land at the bottom, following = true |
| Older history loaded (viewport near the top) | Stay pinned to the bottom | The previously first visible message stays at the top (`historyAnchor`) |

Snaps are immediate, with no animation. Animated scrolls pass through intermediate origins that look like user scrolls away from the bottom.

## Unread bar interaction

The unread bar is the "new messages below" notifier. Whether it is visible is decided only by the server marker (`behaviors/new-messages-marker.md`), not by scroll state. It can be visible while following (nothing auto-acks) and hidden while reading backlog (the marker was acked and only non-counting rows arrived since).

Esc means "I've read all the backlog I care about, show me the new stuff". It does two things at once: it acks the marker, and it jumps to the bottom with following = true. Activating the unread bar does the same. Esc still yields to the overlay priority in `keyboard-shortcuts.md`: while an overlay is open, Esc closes that overlay and does not scroll. With nothing to ack (no marker, no unread count), Esc still jumps to the bottom.

## Cases

### Case A — following at the bottom

1. The user is at the bottom of #chan. Following = true.
2. Messages arrive. Each one lands visible and the viewport stays pinned to the bottom.
3. A link preview arrives late on the last message and the row grows. The viewport stays pinned, so the whole card is visible.

### Case B — last row partly visible

1. The user scrolls up until only the top few pixels of the last message show, then stops.
2. The scroll settles. The latest row intersects the viewport, so following = true.
3. The next message snaps the viewport to the very bottom.

### Case C — reading backlog

1. The user scrolls up and the latest row leaves the viewport. Following = false.
2. Messages arrive. The viewport does not move, and the unread bar shows "N new messages".
3. A presence group above the viewport collapses and the document shrinks. The top visible row stays put, the viewport does not jump down, and following stays false.
4. The user presses Esc. The marker is acked, the view snaps to the bottom and following = true.

### Case D — sending while reading backlog

1. The user is scrolled up in backlog and types a reply.
2. On send, the view snaps to the bottom, following = true, and the local echo is visible.
3. The marker is **not** acked by sending, because acks are explicit only (`new-messages-marker.md`).

### Case E — scrolling back down by hand

1. The user is reading backlog and scrolls down until the latest row is visible again.
2. Following = true. No ack happens, so the unread bar stays until Esc or a tap.

## Edge cases

- **Document shorter than the viewport**: the latest row is always visible, so following is always true.
- **Unread bar appears on the first message after catching up**: its `safeAreaInset` shrinks the viewport. This is a content or viewport change, not a user scroll, so following keeps its prior value (see `apple-pitfalls.md`, "The unread bar can hide the newest IRC message at the bottom").
- **Late layout**: TextKit 2 can finish laying out an appended paragraph after the first snap. While following, the final geometry must still end pinned to the bottom.
- **Live window resize while following**: stays pinned. Repinning on each resize tick must not lay out the whole document (see Implementation constraints).
- **Following with a history prepend**: following wins, so the view stays at the bottom and does not jump to the anchor.
- **iOS keyboard show/hide**: this is a viewport resize, so the same rules apply.

## Implementation constraints

These come from the 2026-09-28 review of the frame-change repin:

- Snapshot following before any storage edit or frame change, and act on the snapshot. The removed `viewportPinnedToBottom` (`followsBottom || isNearBottom`), evaluated after the frame changed, violated the Model. Content changes now never write `followsBottom`, so the stored value is the snapshot.
- Do not force layout or move the clip view synchronously inside `NSView.frameDidChange`. TextKit 2 posts it from inside its own viewport layout. Defer the repin to the next run-loop turn and coalesce repeated requests.
- One repin path. The review found four (`sync()`, `scrollToBottomAfterLayout`, the frame observer and `PreviewAttachmentResizeRelay.repinToBottom`), so one event could lay out the full document several times. Now `scrollToBottom()` is the only repin. `sync()` calls it directly after its own storage edits. Layout callbacks (`TimelineNSTextView.setFrameSize` height changes, `TimelineScrollView.tile()` viewport changes, `viewDidEndLiveResize`) reach it only through the deferred, coalesced `scheduleRepin()`.
- Only height changes need a repin. Ignore width-only frame changes unless they changed the height through reflow.

## Known gaps

- **macOS, reading backlog:** the top row is re-anchored after storage edits the coordinator makes: rebuilds, block replacements and presence toggles. It is not re-anchored when TextKit changes a height from inside its own layout. For example, an inline image above the viewport that finishes loading pushes the rows below it down.
- **iOS, reading backlog:** `.defaultScrollAnchor(.top, for: .sizeChanges)` keeps the scroll offset from the top. Height changes above the viewport rely on SwiftUI's own lazy-stack position preservation.

## Non-goals

- No per-buffer scroll position memory. A buffer switch always lands at the bottom.
- No "jump to the unread divider" on open.
- No auto-ack from scrolling to the bottom. Acks stay explicit.
- No floating "jump to bottom" button separate from the unread bar.

## Related

- `ai-docs/behaviors/new-messages-marker.md` — the marker and unread bar lifecycle, and ack semantics
- `ai-docs/keyboard-shortcuts.md` — Esc overlay priority
- `ai-docs/apple.md` — timeline architecture (`TimelineCoordinator`, `MacTimelineContainer`)
- `ai-docs/apple-pitfalls.md` — unread bar inset versus follow intent
- `apple/Lurker/TimelineTextView.swift`, `apple/Lurker/ConversationView.swift`
