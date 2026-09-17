// macOS-only message timeline rendered as a single AppKit NSTextView
// (TextKit 2). SwiftUI `Text` link hit-testing is unreliable in wrapped
// multi-link messages and per-row `.textSelection` cannot select across rows;
// a real text view gives exact link targets, the hand cursor, and cross-row
// copy natively. iOS keeps the SwiftUI TimelineView. See ai-docs/apple.md.
//
// TextKit 2 rule: never touch `textView.layoutManager` — merely reading it
// silently downgrades the view to TextKit 1. Use `textLayoutManager` only.

#if os(macOS)

import AppKit
import SwiftUI

extension NSAttributedString.Key {
  /// Message a run belongs to; drives the context menu.
  static let lurkerMessageID = NSAttributedString.Key("lurkerMessageID")
  /// Runs excluded from Copy (unread separator, preview cards).
  static let lurkerCopyExclude = NSAttributedString.Key("lurkerCopyExclude")
  /// NSColor painted across the full row width by `LurkerLayoutFragment`
  /// (mention highlight). Glyph-scoped `.backgroundColor` can't reach the
  /// container edges.
  static let lurkerRowHighlight = NSAttributedString.Key("lurkerRowHighlight")
  /// NSColor of the hairline rules drawn beside a separator title (day /
  /// unread separators) by `LurkerLayoutFragment`.
  static let lurkerSeparatorRule = NSAttributedString.Key("lurkerSeparatorRule")
}

/// Everything the block builder needs besides the item itself.
struct TimelineRenderContext {
  let buffer: Buffer?
  let model: AppModel
  var expandedGroups = Set<UUID>()
}

/// SwiftUI shell around the text view: pins the UnreadBar above it and
/// floats the history-loading spinner, mirroring the SwiftUI timeline.
struct MacTimelineContainer: View {

  // MARK: Internal

  let buffer: Buffer

  var body: some View {
    TimelineTextView(buffer: buffer)
      .overlay(alignment: .top) {
        if model.historyLoading.contains(buffer.id) {
          ProgressView()
            .controlSize(.small)
            .padding(10)
        }
      }
      .safeAreaInset(edge: .top, spacing: 0) {
        // `unread > 0` fallback: keeps the ack affordance available when
        // the server predates `marker_id` (version skew).
        if buffer.markerID != nil || buffer.unread > 0 {
          UnreadBar(buffer: buffer)
        }
      }
      .background(Color.lurkerTimelineBackground)
  }

  // MARK: Private

  @Environment(AppModel.self) private var model

}

struct TimelineTextView: NSViewRepresentable {

  // MARK: Internal

  let buffer: Buffer

  func makeCoordinator() -> TimelineCoordinator {
    TimelineCoordinator()
  }

  func makeNSView(context: Context) -> NSView {
    let textView = TimelineNSTextView(usingTextLayoutManager: true)
    textView.isEditable = false
    textView.isSelectable = true
    textView.isRichText = true
    textView.allowsUndo = false
    textView.usesFontPanel = false
    textView.usesFindBar = true
    textView.importsGraphics = false
    textView.drawsBackground = true
    textView.backgroundColor = .textBackgroundColor
    textView.textContainerInset = NSSize(width: 0, height: 5)
    textView.textContainer?.widthTracksTextView = true
    textView.textContainer?.lineFragmentPadding = 0
    textView.isVerticallyResizable = true
    textView.isHorizontallyResizable = false
    textView.autoresizingMask = [.width]
    textView.minSize = .zero
    textView.maxSize = NSSize(
      width: CGFloat.greatestFiniteMagnitude,
      height: CGFloat.greatestFiniteMagnitude,
    )
    // Cursor only: link *styling* stays per-run (message links are blue and
    // underlined, the presence-summary toggle is secondary text) — a color
    // here would repaint every .link range uniformly.
    textView.linkTextAttributes = [
      .cursor: NSCursor.pointingHand
    ]

    let scrollView = TimelineScrollView()
    scrollView.documentView = textView
    scrollView.hasVerticalScroller = true
    scrollView.drawsBackground = true
    scrollView.backgroundColor = .textBackgroundColor
    scrollView.contentView.postsBoundsChangedNotifications = true

    // NSTextView's legacy accessibility machinery ignores a subclass's
    // modern accessibilityChildren() override, so per-message AX rows live
    // on a transparent sibling host view instead.
    let container = NSView()
    let axHost = TimelineAXHostView()
    scrollView.translatesAutoresizingMaskIntoConstraints = false
    axHost.translatesAutoresizingMaskIntoConstraints = false
    container.addSubview(scrollView)
    container.addSubview(axHost)
    NSLayoutConstraint.activate([
      scrollView.leadingAnchor.constraint(equalTo: container.leadingAnchor),
      scrollView.trailingAnchor.constraint(equalTo: container.trailingAnchor),
      scrollView.topAnchor.constraint(equalTo: container.topAnchor),
      scrollView.bottomAnchor.constraint(equalTo: container.bottomAnchor),
      axHost.leadingAnchor.constraint(equalTo: container.leadingAnchor),
      axHost.trailingAnchor.constraint(equalTo: container.trailingAnchor),
      axHost.topAnchor.constraint(equalTo: container.topAnchor),
      axHost.bottomAnchor.constraint(equalTo: container.bottomAnchor),
    ])

    context.coordinator.install(textView: textView, scrollView: scrollView, axHost: axHost)
    return container
  }

  func updateNSView(_: NSView, context: Context) {
    // Reading `selectedMessages` (and, inside sync, `historyAnchor`) here
    // registers SwiftUI observation, so model mutations re-invoke this.
    let items = timelineItems(
      model.selectedMessages,
      buffer: buffer,
      expandedGroups: context.coordinator.expandedPresenceGroups,
    )
    context.coordinator.sync(items: items, buffer: buffer, model: model)
  }

  // MARK: Private

  @Environment(AppModel.self) private var model

}

/// Owns the rendered-block table and applies minimal text-storage edits when
/// the model's timeline changes. Blocks map 1:1 to `TimelineItem`s; the
/// character offset of block `i` is the sum of lengths before it.
@MainActor
final class TimelineCoordinator: NSObject {

  // MARK: Internal

  struct RenderedBlock {
    let item: TimelineItem
    var length: Int
    /// The builder's attributed newline, omitted while this is the last
    /// block. Restore it on append without replacing any existing content.
    let separator: NSAttributedString
    /// What the row's avatar slot rendered as (bot glyph / cached image /
    /// identicon). Depends on AppModel state outside the item, so a diff of
    /// items alone can't see it change; compared on every sync instead.
    var avatarKey: String?
  }

  private(set) var buffer: Buffer?
  private(set) var model: AppModel?
  /// Collapsed presence runs the user expanded in place (keyed by the run's
  /// first member id). Coordinator-owned so it survives rebuilds, unlike the
  /// iOS DisclosureGroup's @State. Cleared on buffer switch.
  private(set) var expandedPresenceGroups = Set<UUID>()

  /// Internal faces of the pinning logic for PreviewAttachmentResizeRelay,
  /// which lives outside the coordinator but must preserve follow-at-bottom
  /// when an inline image grows after insertion.
  var viewportPinnedToBottom: Bool {
    isNearBottom
  }

  func install(
    textView: TimelineNSTextView,
    scrollView: NSScrollView,
    axHost: TimelineAXHostView,
  ) {
    self.textView = textView
    self.scrollView = scrollView
    self.axHost = axHost
    textView.coordinator = self
    textView.delegate = self
    textView.textLayoutManager?.delegate = self
    axHost.coordinator = self
    NotificationCenter.default.addObserver(
      self,
      selector: #selector(clipViewBoundsChanged),
      name: NSView.boundsDidChangeNotification,
      object: scrollView.contentView,
    )
  }

  func sync(items: [TimelineItem], buffer: Buffer, model: AppModel) {
    self.buffer = buffer
    self.model = model
    let fingerprint = Fingerprint(
      showEmbeds: buffer.showEmbeds,
      collapsePresence: buffer.collapsePresenceEvents,
    )
    defer {
      renderedBufferID = buffer.id
      renderedFingerprint = fingerprint
    }

    if buffer.id != renderedBufferID || fingerprint != renderedFingerprint {
      if buffer.id != renderedBufferID {
        expandedPresenceGroups = []
      }
      rebuild(items)
      scrollToBottom()
      // An anchor addressed to another buffer is stale: drop it without
      // scrolling rather than leaving it to block further load-older calls.
      consumeAnchor(model, ownedBy: nil)
      kickAvatarLoads(items)
      return
    }

    // A history page landed: rebuild (prepends shift every offset anyway)
    // and pin the previously-first visible message back to the top edge.
    if let anchor = model.historyAnchor, anchor.bufferID == buffer.id {
      rebuild(items)
      restoreAnchor(anchor.messageID)
      consumeAnchor(model, ownedBy: buffer.id)
      kickAvatarLoads(items)
      return
    }

    let pinned = isNearBottom
    switch TimelineDiff.compute(old: blocks.map(\.item), new: items) {
    case .none:
      break

    case .rebuild:
      rebuild(items)
      if pinned {
        scrollToBottom()
      }

    case .incremental(let replacements, let appendFrom):
      for index in replacements { replaceBlock(at: index, with: items[index]) }
      if let appendFrom {
        appendBlocks(items[appendFrom...])
      }
      // Replacements can change block heights too (a late preview growing
      // the last message), not just appends — re-pin for either.
      if pinned, appendFrom != nil || !replacements.isEmpty {
        scrollToBottom(animated: appendFrom != nil)
      }
    }
    // Bot/avatar state lives in AppModel, not in the items, so even a .none
    // diff can hide rows whose avatar slot is out of date.
    if refreshStaleAvatarRows(), pinned {
      scrollToBottom()
    }
    kickAvatarLoads(items)
  }

  func repinToBottom() {
    scrollToBottom()
  }

  /// One AX element per message block, restoring the SwiftUI timeline's
  /// contract: label "<sender>, <time>, <content>" with the row's on-screen
  /// frame. Elements are rebuilt on every call — accessibility queries are
  /// rare and the layout is the source of truth for frames.
  func accessibilityRows() -> [NSAccessibilityElement] {
    guard
      let textView, let axHost,
      let layout = textView.textLayoutManager,
      let content = textView.textContentStorage
    else { return [] }
    forceFullLayout()
    let inset = textView.textContainerInset
    var elements = [NSAccessibilityElement]()
    var location = 0
    for block in blocks {
      defer { location += block.length }
      guard case .message(let message) = block.item else { continue }
      guard
        let start = content.location(
          content.documentRange.location,
          offsetBy: location,
        ),
        let end = content.location(start, offsetBy: block.length),
        let range = NSTextRange(location: start, end: end)
      else { continue }
      var rect = CGRect.null
      layout.enumerateTextSegments(in: range, type: .standard, options: []) {
        _, frame, _, _ in
        rect = rect.union(frame)
        return true
      }
      guard !rect.isNull else { continue }
      // Fragment frames are container coordinates; the view adds the inset.
      // convert(_:to:) resolves scrolling and flippedness into the host's
      // space, which is what accessibilityFrameInParentSpace expects.
      let inView = rect.offsetBy(dx: inset.width, dy: inset.height)
      let parentSpace = textView.convert(inView, to: axHost)
      let label = "\(message.sender), \(displayTime(message.ts)), \(message.content)"
      let element =
        NSAccessibilityElement.element(
          withRole: .staticText,
          frame: .zero,
          label: label,
          parent: axHost,
        )
        as! NSAccessibilityElement
      element.setAccessibilityFrameInParentSpace(parentSpace)
      elements.append(element)
    }
    axRowElements = elements
    return elements
  }

  func message(atCharacterIndex index: Int) -> Message? {
    var location = 0
    for block in blocks {
      if index < location + block.length {
        if case .message(let message) = block.item {
          return message
        }
        return nil
      }
      location += block.length
    }
    return nil
  }

  /// Esc pressed while the text view is first responder: ack unread, the
  /// same behavior as `ConversationView`'s `.onKeyPress(.escape)`.
  func handleEscape() -> Bool {
    guard let buffer, let model, buffer.markerID != nil || buffer.unread > 0 else {
      return false
    }
    model.ackRead(buffer.id)
    return true
  }

  @objc
  func copyMessage(_ sender: NSMenuItem) {
    guard let message = sender.representedObject as? Message else { return }
    Clipboard.copy(message.content)
  }

  @objc
  func copyNickname(_ sender: NSMenuItem) {
    guard let message = sender.representedObject as? Message else { return }
    Clipboard.copy(message.sender)
  }

  @objc
  func muteSender(_ sender: NSMenuItem) {
    guard let message = sender.representedObject as? Message else { return }
    model?.mute(nick: message.sender, in: message.networkID)
  }

  @objc
  func unmuteSender(_ sender: NSMenuItem) {
    guard let message = sender.representedObject as? Message else { return }
    model?.unmute(nick: message.sender, in: message.networkID)
  }

  // MARK: Private

  private struct Fingerprint: Equatable {
    let showEmbeds: Bool
    let collapsePresence: Bool
  }

  private weak var textView: TimelineNSTextView?
  private weak var scrollView: NSScrollView?
  private weak var axHost: TimelineAXHostView?
  // AX clients hold opaque tokens into these objects and resolve them
  // later; without a strong reference here the elements deallocate between
  // the children query and the attribute fetch and get pruned.
  private var axRowElements = [NSAccessibilityElement]()
  private var blocks = [RenderedBlock]()
  private var renderedBufferID: UUID?
  private var renderedFingerprint: Fingerprint?
  private var avatarLoadsInFlight = Set<URL>()

  private var renderContext: TimelineRenderContext? {
    guard let model else { return nil }
    return TimelineRenderContext(
      buffer: buffer,
      model: model,
      expandedGroups: expandedPresenceGroups,
    )
  }

  private var isNearBottom: Bool {
    guard let scrollView, let textView else { return true }
    return scrollView.documentVisibleRect.maxY >= textView.frame.maxY - 40
  }

  /// Re-derives the item list from the current model state (used after a
  /// coordinator-owned state change like a presence-group toggle, where no
  /// model mutation will re-invoke updateNSView).
  private func resyncFromModel() {
    guard let model, let buffer else { return }
    let items = timelineItems(
      model.selectedMessages,
      buffer: buffer,
      expandedGroups: expandedPresenceGroups,
    )
    sync(items: items, buffer: buffer, model: model)
  }

  private func rebuild(_ items: [TimelineItem]) {
    guard let textView, let storage = textView.textStorage, let context = renderContext else {
      return
    }
    let document = NSMutableAttributedString()
    blocks = items.enumerated().map { index, item in
      let rendered = renderBlock(item, context: context, isLast: index == items.count - 1)
      document.append(rendered.text)
      return rendered.block
    }
    storage.setAttributedString(document)
  }

  private func replaceBlock(at index: Int, with item: TimelineItem) {
    guard let textView, let storage = textView.textStorage, let context = renderContext else {
      return
    }
    let rendered = renderBlock(
      item,
      context: context,
      isLast: index == blocks.count - 1,
    )
    let range = NSRange(location: offset(of: index), length: blocks[index].length)
    textView.textContentStorage?.performEditingTransaction {
      storage.replaceCharacters(in: range, with: rendered.text)
    }
    blocks[index] = rendered.block
  }

  private func appendBlocks(_ items: ArraySlice<TimelineItem>) {
    guard let textView, let storage = textView.textStorage, let context = renderContext else {
      return
    }
    // Load-bearing: an empty append would still restore the tail separator
    // and reintroduce the trailing empty line.
    guard !items.isEmpty else { return }
    let appended = NSMutableAttributedString()
    if let last = blocks.last {
      appended.append(last.separator)
    }
    let newBlocks = items.enumerated().map { index, item in
      let rendered = renderBlock(item, context: context, isLast: index == items.count - 1)
      appended.append(rendered.text)
      return rendered.block
    }
    // One suffix insertion preserves selection and hosted preview identity
    // in the old tail. Its omitted separator belongs to that block's range.
    textView.textContentStorage?.performEditingTransaction {
      if let last = blocks.last {
        blocks[blocks.count - 1].length += last.separator.length
      }
      blocks.append(contentsOf: newBlocks)
      storage.append(appended)
    }
  }

  /// Block builders include a paragraph separator for concatenation. At
  /// the document end it creates an empty TextKit line, so omit only that
  /// final separator (preserving any newlines in the message itself).
  private func renderBlock(
    _ item: TimelineItem,
    context: TimelineRenderContext,
    isLast: Bool,
  ) -> (text: NSAttributedString, block: RenderedBlock) {
    let text = timelineBlockText(item, context: context)
    let separator = text.attributedSubstring(from: NSRange(location: text.length - 1, length: 1))
    let length = text.length - (isLast ? 1 : 0)
    return (
      isLast ? text.attributedSubstring(from: NSRange(location: 0, length: length)) : text,
      RenderedBlock(item: item, length: length, separator: separator, avatarKey: avatarKey(for: item)),
    )
  }

  /// Fires cache-filling fetches for avatars the block builder had to
  /// render as identicons, then re-renders those senders' rows when the
  /// image lands. Loads are deduplicated by URL across syncs.
  private func kickAvatarLoads(_ items: [TimelineItem]) {
    guard let model, let buffer else { return }
    var pending = [URL: String]()
    for item in items {
      guard
        case .message(let message) = item,
        message.displayKind != "sys",
        model.hasAvatar(message.sender),
        let url = model.avatarURL(networkID: message.networkID, nick: message.sender),
        ImageCache.shared.cached(for: url) == nil
      else { continue }
      pending[url] = message.sender
    }
    let bufferID = buffer.id
    for (url, nick) in pending where !avatarLoadsInFlight.contains(url) {
      avatarLoadsInFlight.insert(url)
      Task { @MainActor [weak self] in
        _ = await ImageCache.shared.image(for: url)
        guard let self else { return }
        self.avatarLoadsInFlight.remove(url)
        self.avatarLoaded(nick: nick, bufferID: bufferID)
      }
    }
  }

  private func avatarLoaded(nick _: String, bufferID: UUID) {
    guard buffer?.id == bufferID else { return }
    _ = refreshStaleAvatarRows()
  }

  /// Mirrors `avatarRun`'s branch order; a mismatch with a block's rendered
  /// key means the row must be re-rendered.
  private func avatarKey(for item: TimelineItem) -> String? {
    guard let model, case .message(let message) = item, message.displayKind != "sys" else {
      return nil
    }
    if model.isBot(message.sender) {
      return "bot"
    }
    if
      model.hasAvatar(message.sender),
      let url = model.avatarURL(networkID: message.networkID, nick: message.sender),
      ImageCache.shared.cached(for: url) != nil
    {
      return url.absoluteString
    }
    return "identicon"
  }

  /// Re-renders rows whose avatar slot no longer matches the model (bot
  /// flag or avatar metadata arriving after the row rendered, or a fetched
  /// image landing in the cache). Returns whether anything changed.
  private func refreshStaleAvatarRows() -> Bool {
    var changed = false
    for index in blocks.indices
      where blocks[index].avatarKey != avatarKey(for: blocks[index].item)
    {
      replaceBlock(at: index, with: blocks[index].item)
      changed = true
    }
    return changed
  }

  private func offset(of index: Int) -> Int {
    // ponytail: O(n) prefix sum per lookup; fine for per-event edits at
    // backlog sizes, revisit with a running index if buffers hit 10k+ blocks.
    blocks[..<index].reduce(0) { $0 + $1.length }
  }

  /// Forces exact (non-estimated) layout of the whole document and sizes
  /// the text view to match. `ensureLayout(for: documentRange)` is not
  /// enough: TextKit 2 leaves off-viewport fragment frames *estimated*, so
  /// scroll targets computed from them land pages away and get clamped
  /// against a stale document height. Rebuilds are rare (buffer switch,
  /// history page); full layout of a backlog page is cheap.
  private func forceFullLayout() {
    guard let textView, let layout = textView.textLayoutManager else { return }
    layout.enumerateTextLayoutFragments(from: nil, options: [.ensuresLayout]) { _ in true }
    let usage = layout.usageBoundsForTextContainer
    let height = usage.maxY + textView.textContainerInset.height * 2
    if abs(textView.frame.height - height) > 0.5 {
      textView.setFrameSize(NSSize(width: textView.frame.width, height: height))
    }
  }

  private func scrollToBottom(animated: Bool = false) {
    guard let textView else { return }
    forceFullLayout()
    if animated {
      textView.enclosingScrollView?.contentView.animator().setBoundsOrigin(bottomOrigin())
    } else {
      textView.scrollToEndOfDocument(nil)
    }
  }

  private func bottomOrigin() -> NSPoint {
    guard let scrollView, let textView else { return .zero }
    let y = max(0, textView.frame.height - scrollView.contentSize.height)
    return NSPoint(x: 0, y: y)
  }

  private func restoreAnchor(_ messageID: UUID) {
    guard
      let textView,
      let layout = textView.textLayoutManager,
      let contentStorage = textView.textContentStorage,
      let index = blocks.firstIndex(where: { $0.item.anchorMessageID == messageID })
    else { return }
    forceFullLayout()
    guard
      let location = contentStorage.location(
        contentStorage.documentRange.location,
        offsetBy: offset(of: index),
      ),
      let fragment = layout.textLayoutFragment(for: location)
    else { return }
    let y = fragment.layoutFragmentFrame.minY + textView.textContainerInset.height
    scrollView?.contentView.setBoundsOrigin(NSPoint(x: 0, y: max(0, y)))
    if let scrollView {
      scrollView.reflectScrolledClipView(scrollView.contentView)
    }
  }

  private func consumeAnchor(_ model: AppModel, ownedBy bufferID: UUID?) {
    guard let anchor = model.historyAnchor else { return }
    if let bufferID, anchor.bufferID != bufferID {
      return
    }
    // Never mutate observable state synchronously inside a SwiftUI view
    // update (updateNSView) — defer to the next main-actor turn.
    Task { @MainActor in
      if model.historyAnchor == anchor {
        model.historyAnchor = nil
      }
    }
  }

  @objc
  private func clipViewBoundsChanged(_: Notification) {
    guard let scrollView else { return }
    if scrollView.documentVisibleRect.minY < 200 {
      // Self-guarding: AppModel refuses while a load or anchor is pending.
      model?.loadOlderHistory()
    }
  }

}

extension TimelineCoordinator: NSTextViewDelegate {
  /// Internal lurker-presence:// links toggle a collapsed presence run's
  /// in-place expansion; real URLs fall through to the default opener.
  func textView(_: NSTextView, clickedOnLink link: Any, at _: Int) -> Bool {
    guard
      let url = link as? URL,
      url.scheme == "lurker-presence",
      let id = (url.host()).flatMap(UUID.init(uuidString:))
    else { return false }
    expandedPresenceGroups.formSymmetricDifference([id])
    resyncFromModel()
    // Expanding a terminal group is a pure suffix append: the summary item
    // itself compares equal, so the diff never redraws its arrow. Re-render
    // the toggled block explicitly (collapse rebuilds, where this is a
    // harmless no-op replacement).
    if
      let index = blocks.firstIndex(where: {
        if case .presence(let groupID, _) = $0.item {
          return groupID == id
        }
        return false
      })
    {
      replaceBlock(at: index, with: blocks[index].item)
    }
    return true
  }
}

extension TimelineCoordinator: @preconcurrency NSTextLayoutManagerDelegate {
  /// Paragraphs tagged with a full-row highlight or separator rules render
  /// through `LurkerLayoutFragment`, which draws behind/around the text.
  func textLayoutManager(
    _: NSTextLayoutManager,
    textLayoutFragmentFor _: NSTextLocation,
    in textElement: NSTextElement,
  ) -> NSTextLayoutFragment {
    if let paragraph = textElement as? NSTextParagraph, paragraph.attributedString.length > 0 {
      let attributes = paragraph.attributedString.attributes(at: 0, effectiveRange: nil)
      let highlight = attributes[.lurkerRowHighlight] as? NSColor
      let rule = attributes[.lurkerSeparatorRule] as? NSColor
      if highlight != nil || rule != nil {
        let fragment = LurkerLayoutFragment(
          textElement: textElement,
          range: textElement.elementRange,
        )
        fragment.rowHighlight = highlight
        fragment.separatorRule = rule
        return fragment
      }
    }
    return NSTextLayoutFragment(textElement: textElement, range: textElement.elementRange)
  }
}

/// Custom drawing behind/around a paragraph: full-container-width mention
/// highlight, and the hairline rules flanking a centered separator title.
final class LurkerLayoutFragment: NSTextLayoutFragment {
  var rowHighlight: NSColor?
  var separatorRule: NSColor?

  override func draw(at point: CGPoint, in context: CGContext) {
    context.saveGState()
    if let rowHighlight {
      context.setFillColor(rowHighlight.cgColor)
      context.fill(CGRect(origin: point, size: layoutFragmentFrame.size))
    }
    if let separatorRule, let line = textLineFragments.first {
      let bounds = line.typographicBounds
      let y = point.y + layoutFragmentFrame.height / 2
      let inset: CGFloat = 12
      let gap: CGFloat = 8
      context.setStrokeColor(separatorRule.cgColor)
      context.setLineWidth(1)
      context.move(to: CGPoint(x: point.x + inset, y: y))
      context.addLine(to: CGPoint(x: point.x + bounds.minX - gap, y: y))
      context.move(to: CGPoint(x: point.x + bounds.maxX + gap, y: y))
      context.addLine(to: CGPoint(x: point.x + layoutFragmentFrame.width - inset, y: y))
      context.strokePath()
    }
    context.restoreGState()
    super.draw(at: point, in: context)
  }
}

#endif
