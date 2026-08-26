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
    var expandedGroups: Set<UUID> = []
  }

  /// SwiftUI shell around the text view: pins the UnreadBar above it and
  /// floats the history-loading spinner, mirroring the SwiftUI timeline.
  struct MacTimelineContainer: View {
    @Environment(AppModel.self) private var model
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
  }

  struct TimelineTextView: NSViewRepresentable {
    @Environment(AppModel.self) private var model
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
        width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
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

    func updateNSView(_ container: NSView, context: Context) {
      // Reading `selectedMessages` (and, inside sync, `historyAnchor`) here
      // registers SwiftUI observation, so model mutations re-invoke this.
      let items = timelineItems(
        model.selectedMessages, buffer: buffer,
        expandedGroups: context.coordinator.expandedPresenceGroups)
      context.coordinator.sync(items: items, buffer: buffer, model: model)
    }
  }

  /// Owns the rendered-block table and applies minimal text-storage edits when
  /// the model's timeline changes. Blocks map 1:1 to `TimelineItem`s; the
  /// character offset of block `i` is the sum of lengths before it.
  @MainActor
  final class TimelineCoordinator: NSObject {
    struct RenderedBlock {
      let item: TimelineItem
      var length: Int
    }

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
    private var axRowElements: [NSAccessibilityElement] = []
    private var blocks: [RenderedBlock] = []
    private var renderedBufferID: UUID?
    private var renderedFingerprint: Fingerprint?
    private(set) var buffer: Buffer?
    private(set) var model: AppModel?
    /// Collapsed presence runs the user expanded in place (keyed by the run's
    /// first member id). Coordinator-owned so it survives rebuilds, unlike the
    /// iOS DisclosureGroup's @State. Cleared on buffer switch.
    private(set) var expandedPresenceGroups: Set<UUID> = []
    private var avatarLoadsInFlight: Set<URL> = []

    private var renderContext: TimelineRenderContext? {
      guard let model else { return nil }
      return TimelineRenderContext(
        buffer: buffer, model: model, expandedGroups: expandedPresenceGroups)
    }

    func install(
      textView: TimelineNSTextView, scrollView: NSScrollView, axHost: TimelineAXHostView
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
        object: scrollView.contentView)
    }

    func sync(items: [TimelineItem], buffer: Buffer, model: AppModel) {
      self.buffer = buffer
      self.model = model
      let fingerprint = Fingerprint(
        showEmbeds: buffer.showEmbeds, collapsePresence: buffer.collapsePresenceEvents)
      defer {
        renderedBufferID = buffer.id
        renderedFingerprint = fingerprint
      }

      if buffer.id != renderedBufferID || fingerprint != renderedFingerprint {
        if buffer.id != renderedBufferID { expandedPresenceGroups = [] }
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
        return
      }

      switch TimelineDiff.compute(old: blocks.map(\.item), new: items) {
      case .none:
        return
      case .rebuild:
        let pinned = isNearBottom
        rebuild(items)
        if pinned { scrollToBottom() }
      case .incremental(let replacements, let appendFrom):
        let pinned = isNearBottom
        for index in replacements { replaceBlock(at: index, with: items[index]) }
        if let appendFrom {
          appendBlocks(items[appendFrom...])
          if pinned { scrollToBottom(animated: true) }
        }
      }
      kickAvatarLoads(items)
    }

    /// Re-derives the item list from the current model state (used after a
    /// coordinator-owned state change like a presence-group toggle, where no
    /// model mutation will re-invoke updateNSView).
    private func resyncFromModel() {
      guard let model, let buffer else { return }
      let items = timelineItems(
        model.selectedMessages, buffer: buffer, expandedGroups: expandedPresenceGroups)
      sync(items: items, buffer: buffer, model: model)
    }

    // MARK: Storage edits

    private func rebuild(_ items: [TimelineItem]) {
      guard let textView, let storage = textView.textStorage, let context = renderContext else {
        return
      }
      let document = NSMutableAttributedString()
      blocks = items.map { item in
        let block = timelineBlockText(item, context: context)
        document.append(block)
        return RenderedBlock(item: item, length: block.length)
      }
      storage.setAttributedString(document)
    }

    private func replaceBlock(at index: Int, with item: TimelineItem) {
      guard let textView, let storage = textView.textStorage, let context = renderContext else {
        return
      }
      let block = timelineBlockText(item, context: context)
      let range = NSRange(location: offset(of: index), length: blocks[index].length)
      textView.textContentStorage?.performEditingTransaction {
        storage.replaceCharacters(in: range, with: block)
      }
      blocks[index] = RenderedBlock(item: item, length: block.length)
    }

    private func appendBlocks(_ items: ArraySlice<TimelineItem>) {
      guard let textView, let storage = textView.textStorage, let context = renderContext else {
        return
      }
      let appended = NSMutableAttributedString()
      for item in items {
        let block = timelineBlockText(item, context: context)
        appended.append(block)
        blocks.append(RenderedBlock(item: item, length: block.length))
      }
      textView.textContentStorage?.performEditingTransaction {
        storage.append(appended)
      }
    }

    // MARK: Avatars

    /// Fires cache-filling fetches for avatars the block builder had to
    /// render as identicons, then re-renders those senders' rows when the
    /// image lands. Loads are deduplicated by URL across syncs.
    private func kickAvatarLoads(_ items: [TimelineItem]) {
      guard let model, let buffer else { return }
      var pending: [URL: String] = [:]
      for item in items {
        guard case .message(let message) = item,
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

    private func avatarLoaded(nick: String, bufferID: UUID) {
      guard buffer?.id == bufferID else { return }
      for (index, block) in blocks.enumerated() {
        if case .message(let message) = block.item, message.sender == nick {
          replaceBlock(at: index, with: block.item)
        }
      }
    }

    private func offset(of index: Int) -> Int {
      // ponytail: O(n) prefix sum per lookup; fine for per-event edits at
      // backlog sizes, revisit with a running index if buffers hit 10k+ blocks.
      blocks[..<index].reduce(0) { $0 + $1.length }
    }

    // MARK: Scrolling

    private var isNearBottom: Bool {
      guard let scrollView, let textView else { return true }
      return scrollView.documentVisibleRect.maxY >= textView.frame.maxY - 40
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
      guard let textView,
        let layout = textView.textLayoutManager,
        let contentStorage = textView.textContentStorage,
        let index = blocks.firstIndex(where: { $0.item.anchorMessageID == messageID })
      else { return }
      forceFullLayout()
      guard
        let location = contentStorage.location(
          contentStorage.documentRange.location, offsetBy: offset(of: index)),
        let fragment = layout.textLayoutFragment(for: location)
      else { return }
      let y = fragment.layoutFragmentFrame.minY + textView.textContainerInset.height
      scrollView?.contentView.setBoundsOrigin(NSPoint(x: 0, y: max(0, y)))
      if let scrollView { scrollView.reflectScrolledClipView(scrollView.contentView) }
    }

    private func consumeAnchor(_ model: AppModel, ownedBy bufferID: UUID?) {
      guard let anchor = model.historyAnchor else { return }
      if let bufferID, anchor.bufferID != bufferID { return }
      // Never mutate observable state synchronously inside a SwiftUI view
      // update (updateNSView) — defer to the next main-actor turn.
      Task { @MainActor in
        if model.historyAnchor == anchor { model.historyAnchor = nil }
      }
    }

    @objc private func clipViewBoundsChanged(_ notification: Notification) {
      guard let scrollView else { return }
      if scrollView.documentVisibleRect.minY < 200 {
        // Self-guarding: AppModel refuses while a load or anchor is pending.
        model?.loadOlderHistory()
      }
    }

    // MARK: Accessibility

    /// One AX element per message block, restoring the SwiftUI timeline's
    /// contract: label "<sender>, <time>, <content>" with the row's on-screen
    /// frame. Elements are rebuilt on every call — accessibility queries are
    /// rare and the layout is the source of truth for frames.
    func accessibilityRows() -> [NSAccessibilityElement] {
      guard let textView, let axHost,
        let layout = textView.textLayoutManager,
        let content = textView.textContentStorage
      else { return [] }
      forceFullLayout()
      let inset = textView.textContainerInset
      var elements: [NSAccessibilityElement] = []
      var location = 0
      for block in blocks {
        defer { location += block.length }
        guard case .message(let message) = block.item else { continue }
        guard
          let start = content.location(
            content.documentRange.location, offsetBy: location),
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
            withRole: .staticText, frame: .zero, label: label, parent: axHost)
          as! NSAccessibilityElement
        element.setAccessibilityFrameInParentSpace(parentSpace)
        elements.append(element)
      }
      axRowElements = elements
      return elements
    }

    // MARK: Interactions

    func message(atCharacterIndex index: Int) -> Message? {
      var location = 0
      for block in blocks {
        if index < location + block.length {
          if case .message(let message) = block.item { return message }
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

    @objc func copyMessage(_ sender: NSMenuItem) {
      guard let message = sender.representedObject as? Message else { return }
      Clipboard.copy(message.content)
    }

    @objc func copyNickname(_ sender: NSMenuItem) {
      guard let message = sender.representedObject as? Message else { return }
      Clipboard.copy(message.sender)
    }

    @objc func muteSender(_ sender: NSMenuItem) {
      guard let message = sender.representedObject as? Message else { return }
      model?.mute(nick: message.sender, in: message.networkID)
    }

    @objc func unmuteSender(_ sender: NSMenuItem) {
      guard let message = sender.representedObject as? Message else { return }
      model?.unmute(nick: message.sender, in: message.networkID)
    }
  }

  extension TimelineCoordinator: NSTextViewDelegate {
    /// Internal lurker-presence:// links toggle a collapsed presence run's
    /// in-place expansion; real URLs fall through to the default opener.
    func textView(_ textView: NSTextView, clickedOnLink link: Any, at charIndex: Int) -> Bool {
      guard let url = link as? URL,
        url.scheme == "lurker-presence",
        let id = (url.host()).flatMap(UUID.init(uuidString:))
      else { return false }
      expandedPresenceGroups.formSymmetricDifference([id])
      resyncFromModel()
      return true
    }
  }

  extension TimelineCoordinator: @preconcurrency NSTextLayoutManagerDelegate {
    /// Paragraphs tagged with a full-row highlight or separator rules render
    /// through `LurkerLayoutFragment`, which draws behind/around the text.
    func textLayoutManager(
      _ textLayoutManager: NSTextLayoutManager,
      textLayoutFragmentFor location: NSTextLocation,
      in textElement: NSTextElement
    ) -> NSTextLayoutFragment {
      if let paragraph = textElement as? NSTextParagraph, paragraph.attributedString.length > 0 {
        let attributes = paragraph.attributedString.attributes(at: 0, effectiveRange: nil)
        let highlight = attributes[.lurkerRowHighlight] as? NSColor
        let rule = attributes[.lurkerSeparatorRule] as? NSColor
        if highlight != nil || rule != nil {
          let fragment = LurkerLayoutFragment(
            textElement: textElement, range: textElement.elementRange)
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

  /// Read-only text view adding the per-message context menu, Esc-to-ack, and
  /// copy sanitization on top of stock NSTextView behavior.
  final class TimelineNSTextView: NSTextView {
    weak var coordinator: TimelineCoordinator?

    override func menu(for event: NSEvent) -> NSMenu? {
      let point = convert(event.locationInWindow, from: nil)
      let index = characterIndexForInsertion(at: point)
      guard let message = coordinator?.message(atCharacterIndex: index) else {
        return super.menu(for: event)
      }
      let menu = NSMenu()
      menu.addItem(item("Copy Message", #selector(TimelineCoordinator.copyMessage), message))
      if !message.sender.isEmpty {
        menu.addItem(item("Copy Nickname", #selector(TimelineCoordinator.copyNickname), message))
        menu.addItem(.separator())
        menu.addItem(
          item("Mute \(message.sender)", #selector(TimelineCoordinator.muteSender), message))
        menu.addItem(
          item("Unmute \(message.sender)", #selector(TimelineCoordinator.unmuteSender), message))
      }
      if selectedRange().length > 0 {
        menu.addItem(.separator())
        menu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "")
      }
      return menu
    }

    private func item(_ title: String, _ action: Selector, _ message: Message) -> NSMenuItem {
      let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
      item.target = coordinator
      item.representedObject = message
      return item
    }

    override func cancelOperation(_ sender: Any?) {
      if coordinator?.handleEscape() != true {
        super.cancelOperation(sender)
      }
    }

    /// Copies the visible text of the selection, minus rows that only make
    /// sense on screen (unread separator, preview placeholders). The tab-based
    /// gutter layout is flattened to single spaces so pasted lines read
    /// `HH:MM nick body`.
    override func copy(_ sender: Any?) {
      guard let storage = textStorage else { return super.copy(sender) }
      let ranges = selectedRanges.map(\.rangeValue).filter { $0.length > 0 }
      guard !ranges.isEmpty else { return super.copy(sender) }
      var pieces: [String] = []
      for range in ranges {
        let sub = storage.attributedSubstring(from: range)
        var out = ""
        sub.enumerateAttributes(in: NSRange(location: 0, length: sub.length)) { attrs, r, _ in
          if attrs[.lurkerCopyExclude] != nil { return }
          if attrs[.attachment] != nil { return }
          out += (sub.string as NSString).substring(with: r)
        }
        let lines = out.components(separatedBy: "\n")
          .map { $0.replacingOccurrences(of: "\t", with: " ").trimmingCharacters(in: .whitespaces) }
          .filter { !$0.isEmpty }
        if !lines.isEmpty { pieces.append(lines.joined(separator: "\n")) }
      }
      guard !pieces.isEmpty else { return }
      Clipboard.copy(pieces.joined(separator: "\n"))
    }
  }

  /// Overriding scrollWheel(with:) opts this scroll view out of AppKit's
  /// asynchronous "responsive scrolling" (per the AppKit release notes).
  /// Responsive scrolling applies wheel/momentum deltas on a concurrent pass
  /// that overrides programmatic origin changes — the history-prepend anchor
  /// restore must win over an in-flight gesture, or the viewport lands one
  /// page off and can cascade extra history loads.
  final class TimelineScrollView: NSScrollView {
    override func scrollWheel(with event: NSEvent) {
      super.scrollWheel(with: event)
    }
  }

  /// Transparent overlay whose only job is exposing per-message AX rows —
  /// NSTextView's legacy accessibility path ignores subclass overrides of the
  /// modern accessibilityChildren(), a plain NSView honors them. Never
  /// intercepts events.
  final class TimelineAXHostView: NSView {
    weak var coordinator: TimelineCoordinator?

    override func hitTest(_ point: NSPoint) -> NSView? {
      nil
    }

    override func isAccessibilityElement() -> Bool {
      // A real element (not an ignored pass-through view): ignored views'
      // custom accessibilityChildren are dropped from the AX tree entirely.
      true
    }

    override func accessibilityRole() -> NSAccessibility.Role? {
      .group
    }

    override func accessibilityChildren() -> [Any]? {
      let rows = coordinator?.accessibilityRows() ?? []
      return rows.isEmpty ? super.accessibilityChildren() : rows
    }
  }

  /// Pure block-level diff between the rendered timeline and the model's new
  /// item list. AppKit-free so it unit-tests without a view.
  enum TimelineDiff: Equatable {
    case none
    case rebuild
    /// `replacements` are indices whose id survived but whose content changed
    /// (preview arrival, netsplit tag, presence run growth); `appendFrom` is
    /// the first index of a strict-suffix append, nil when lengths match.
    case incremental(replacements: [Int], appendFrom: Int?)

    static func compute(old: [TimelineItem], new: [TimelineItem]) -> TimelineDiff {
      if old.isEmpty && new.isEmpty { return .none }
      if old.isEmpty || new.isEmpty { return .rebuild }
      let oldIDs = old.map(\.id)
      let newIDs = new.map(\.id)
      if oldIDs == newIDs {
        let replacements = old.indices.filter { old[$0] != new[$0] }
        return replacements.isEmpty
          ? .none : .incremental(replacements: replacements, appendFrom: nil)
      }
      if newIDs.count > oldIDs.count, Array(newIDs.prefix(oldIDs.count)) == oldIDs {
        let replacements = old.indices.filter { old[$0] != new[$0] }
        return .incremental(replacements: replacements, appendFrom: oldIDs.count)
      }
      return .rebuild
    }
  }

  extension TimelineItem {
    /// The message id a scroll anchor resolves to: a message row's own id, or
    /// a collapsed presence run's first member (its rendered identity).
    var anchorMessageID: UUID? {
      switch self {
      case .message(let message): message.id
      case .presence(let id, _): id
      case .day, .unread: nil
      }
    }
  }

  // MARK: - Block rendering

  /// AppKit mirrors of `Theme.Fonts`, derived from the semantic text styles so
  /// OS metric changes keep working (Theme rule: no hardcoded point sizes).
  enum TimelineNSFonts {
    static var message: NSFont {
      .monospacedSystemFont(
        ofSize: NSFont.preferredFont(forTextStyle: .body).pointSize,
        weight: .regular)
    }

    static func nick(isSelf: Bool) -> NSFont {
      .monospacedSystemFont(
        ofSize: NSFont.preferredFont(forTextStyle: .body).pointSize,
        weight: isSelf ? .bold : .medium)
    }

    static var timestamp: NSFont {
      .monospacedDigitSystemFont(
        ofSize: NSFont.preferredFont(forTextStyle: .caption1).pointSize,
        weight: .regular)
    }

    static func footnote(_ weight: NSFont.Weight) -> NSFont {
      .systemFont(ofSize: NSFont.preferredFont(forTextStyle: .footnote).pointSize, weight: weight)
    }

    static var presenceSummary: NSFont {
      .monospacedSystemFont(
        ofSize: NSFont.preferredFont(forTextStyle: .footnote).pointSize,
        weight: .regular)
    }
  }

  /// Row geometry shared by every block: an 11pt leading inset, a 42pt
  /// right-aligned timestamp gutter, then the nick/body column — the same
  /// metrics as the SwiftUI `MessageRow`.
  private enum RowMetrics {
    static let inset: CGFloat = 11
    static let gutterRight: CGFloat = inset + 42  // 53
    static let contentLeft: CGFloat = gutterRight + 8  // 61
  }

  @MainActor
  private enum RowStyles {
    static let message: NSParagraphStyle = {
      let style = NSMutableParagraphStyle()
      style.tabStops = [
        NSTextTab(textAlignment: .right, location: RowMetrics.gutterRight, options: [:]),
        NSTextTab(textAlignment: .left, location: RowMetrics.contentLeft, options: [:]),
      ]
      style.headIndent = RowMetrics.contentLeft
      style.tailIndent = -RowMetrics.inset
      style.paragraphSpacing = 4
      return style
    }()

    static let centered: NSParagraphStyle = separator(spacing: 9)
    static let unread: NSParagraphStyle = separator(spacing: 5)

    static let indented: NSParagraphStyle = {
      let style = NSMutableParagraphStyle()
      style.firstLineHeadIndent = RowMetrics.contentLeft
      style.headIndent = RowMetrics.contentLeft
      style.tailIndent = -RowMetrics.inset
      style.paragraphSpacing = 4
      return style
    }()

    private static func separator(spacing: CGFloat) -> NSParagraphStyle {
      let style = NSMutableParagraphStyle()
      style.alignment = .center
      style.paragraphSpacingBefore = spacing
      style.paragraphSpacing = spacing
      return style
    }
  }

  /// Renders one `TimelineItem` as an attributed paragraph (or several, for a
  /// message with previews). Every block ends in exactly one "\n".
  @MainActor
  func timelineBlockText(
    _ item: TimelineItem, context: TimelineRenderContext
  ) -> NSAttributedString {
    switch item {
    case .day(_, let title):
      return separatorLine(
        title, color: .secondaryLabelColor, rule: .separatorColor,
        style: RowStyles.centered, weight: .medium)
    case .unread:
      let line = separatorLine(
        "New Messages", color: .systemOrange,
        rule: NSColor.systemOrange.withAlphaComponent(0.35),
        style: RowStyles.unread, weight: .semibold)
      let excluded = NSMutableAttributedString(attributedString: line)
      excluded.addAttribute(
        .lurkerCopyExclude, value: true,
        range: NSRange(location: 0, length: excluded.length))
      return excluded
    case .presence(let id, let messages):
      let expanded = context.expandedGroups.contains(id)
      let arrow = expanded ? "▾" : "▸"
      let text = NSMutableAttributedString(
        string: "\(arrow) \(presenceSummaryText(messages))",
        attributes: [
          .font: TimelineNSFonts.presenceSummary,
          .foregroundColor: NSColor.secondaryLabelColor,
          .paragraphStyle: RowStyles.indented,
          // Internal toggle link: activation + hand cursor for free; styling
          // stays secondary because linkTextAttributes only sets the cursor.
          .link: URL(string: "lurker-presence://\(id.uuidString)")!,
        ])
      text.append(
        NSAttributedString(
          string: "\n",
          attributes: [
            .font: TimelineNSFonts.presenceSummary,
            .paragraphStyle: RowStyles.indented,
          ]))
      return text
    case .message(let message):
      return messageBlock(message, context: context)
    }
  }

  @MainActor
  private func separatorLine(
    _ title: String, color: NSColor, rule: NSColor, style: NSParagraphStyle,
    weight: NSFont.Weight
  ) -> NSAttributedString {
    NSAttributedString(
      string: title + "\n",
      attributes: [
        .font: TimelineNSFonts.footnote(weight),
        .foregroundColor: color,
        .paragraphStyle: style,
        .lurkerSeparatorRule: rule,
      ])
  }

  @MainActor
  private func messageBlock(
    _ message: Message, context: TimelineRenderContext
  ) -> NSAttributedString {
    let block = NSMutableAttributedString()
    let time = displayTime(message.ts)

    block.append(
      NSAttributedString(
        string: "\t\(time)\t",
        attributes: [
          .font: TimelineNSFonts.timestamp,
          .foregroundColor: NSColor.tertiaryLabelColor,
          .paragraphStyle: RowStyles.message,
        ]))

    if message.displayKind == "sys" {
      block.append(
        systemIconAttachment(for: message, font: TimelineNSFonts.message))
      block.append(
        NSAttributedString(
          string: " " + systemMessageText(message),
          attributes: [
            .font: TimelineNSFonts.message,
            .foregroundColor: NSColor.secondaryLabelColor,
            .paragraphStyle: RowStyles.message,
          ]))
    } else {
      let nickFont = TimelineNSFonts.nick(isSelf: message.isSelf == true)
      block.append(avatarRun(for: message, model: context.model, font: nickFont))
      var nickAttributes: [NSAttributedString.Key: Any] = [
        .font: nickFont,
        .foregroundColor: NSColor(nickPaletteColor(message.senderColor)),
        .paragraphStyle: RowStyles.message,
        .toolTip: message.userhost ?? message.sender,
      ]
      if message.userhost == nil && message.sender.isEmpty {
        nickAttributes.removeValue(forKey: .toolTip)
      }
      block.append(NSAttributedString(string: message.sender, attributes: nickAttributes))
      block.append(
        NSAttributedString(
          string: " ",
          attributes: [.font: TimelineNSFonts.message, .paragraphStyle: RowStyles.message]))

      let baseColor: NSColor =
        message.displayKind == "action" ? .systemPurple : .labelColor
      let body = NSMutableAttributedString(
        attributedString: nsMessageBody(
          message,
          baseColor: baseColor))
      body.addAttribute(
        .paragraphStyle, value: RowStyles.message,
        range: NSRange(location: 0, length: body.length))
      block.append(body)
    }

    block.append(
      NSAttributedString(
        string: "\n",
        attributes: [.font: TimelineNSFonts.message, .paragraphStyle: RowStyles.message]))

    // Preview cards render as their own attachment paragraphs inside the same
    // block, so a `.preview` event is a plain block replacement. Excluded from
    // Copy — the raw URL is already in the message text.
    if context.buffer?.showEmbeds != false {
      for preview in message.previews ?? []
      where preview.kind == "image" || preview.kind == "opengraph" {
        block.append(previewParagraph(preview, model: context.model))
      }
    }

    if message.mentionsMe == true || message.highlight == true {
      // Painted at full container width by LurkerLayoutFragment; covers the
      // preview paragraphs too, like the SwiftUI row background did.
      block.addAttribute(
        .lurkerRowHighlight, value: NSColor.systemOrange.withAlphaComponent(0.10),
        range: NSRange(location: 0, length: block.length))
    }

    block.addAttribute(
      .lurkerMessageID, value: message.id.uuidString,
      range: NSRange(location: 0, length: block.length))
    return block
  }

  /// 14×14 avatar square before the nick (bot glyph / server avatar /
  /// identicon), aligned like the SwiftUI row's firstTextBaseline guide.
  @MainActor
  private func avatarRun(for message: Message, model: AppModel, font: NSFont) -> NSAttributedString
  {
    if model.isBot(message.sender) {
      return NSAttributedString(
        string: "🤖 ",
        attributes: [.font: font, .paragraphStyle: RowStyles.message])
    }
    let image: NSImage
    if model.hasAvatar(message.sender),
      let url = model.avatarURL(networkID: message.networkID, nick: message.sender),
      let cached = ImageCache.shared.cached(for: url)
    {
      image = AvatarImages.rounded(cached, cacheKey: url.absoluteString)
    } else {
      // Identicon now; `kickAvatarLoads` re-renders the row if a server
      // avatar arrives later.
      image = AvatarImages.identicon(nick: message.sender, colorIndex: message.senderColor)
    }
    let attachment = NSTextAttachment()
    attachment.image = image
    let size: CGFloat = 14
    attachment.bounds = CGRect(
      x: 0, y: (font.capHeight - size) / 2, width: size, height: size)
    let run = NSMutableAttributedString(attachment: attachment)
    run.append(NSAttributedString(string: " ", attributes: [.font: font]))
    run.addAttribute(
      .paragraphStyle, value: RowStyles.message, range: NSRange(location: 0, length: run.length))
    return run
  }

  @MainActor
  private func systemIconAttachment(for message: Message, font: NSFont) -> NSAttributedString {
    let attachment = NSTextAttachment()
    let size: CGFloat = 12
    attachment.image = AvatarImages.symbol(
      systemMessageSymbol(message), pointSize: size, color: .secondaryLabelColor)
    attachment.bounds = CGRect(
      x: 0, y: (font.capHeight - size) / 2, width: size, height: size)
    let run = NSMutableAttributedString(attachment: attachment)
    run.addAttribute(
      .paragraphStyle, value: RowStyles.message, range: NSRange(location: 0, length: run.length))
    return run
  }

  /// One paragraph per preview: a live SwiftUI card hosted through
  /// `NSTextAttachmentViewProvider`.
  @MainActor
  private func previewParagraph(_ preview: Preview, model: AppModel) -> NSAttributedString {
    let attachment = PreviewTextAttachment(preview: preview, model: model)
    let paragraph = NSMutableAttributedString(attachment: attachment)
    paragraph.append(NSAttributedString(string: "\n"))
    paragraph.addAttributes(
      [
        .paragraphStyle: RowStyles.indented,
        .font: TimelineNSFonts.message,
        .lurkerCopyExclude: true,
      ], range: NSRange(location: 0, length: paragraph.length))
    return paragraph
  }

  // MARK: - Avatar / symbol bitmaps

  /// Cached NSImage renderings for inline attachments. All images use
  /// drawing-handler blocks, so dynamic colors resolve at draw time and adapt
  /// to appearance changes without cache invalidation.
  @MainActor
  private enum AvatarImages {
    private static let identicons = NSCache<NSString, NSImage>()
    private static let avatars = NSCache<NSString, NSImage>()
    private static let symbols = NSCache<NSString, NSImage>()

    static func identicon(nick: String, colorIndex: Int?, size: CGFloat = 14) -> NSImage {
      let key = "\(nick)#\(colorIndex ?? -1)" as NSString
      if let hit = identicons.object(forKey: key) { return hit }
      let rows = nickIdenticonRows(nick)
      let color = NSColor(nickPaletteColor(colorIndex))
      let image = NSImage(
        size: NSSize(width: size, height: size), flipped: true
      ) { _ in
        NSBezierPath(
          roundedRect: NSRect(x: 0, y: 0, width: size, height: size), xRadius: 2, yRadius: 2
        ).addClip()
        let cell = size / 5
        color.setFill()
        for (y, row) in rows.enumerated() {
          for (x, on) in row.enumerated() where on {
            NSRect(x: CGFloat(x) * cell, y: CGFloat(y) * cell, width: cell, height: cell).fill()
          }
        }
        return true
      }
      identicons.setObject(image, forKey: key)
      return image
    }

    /// Rounded-rect scaledToFill crop of a fetched avatar, mirroring
    /// NickAvatar's clipShape.
    static func rounded(_ source: NSImage, cacheKey: String, size: CGFloat = 14) -> NSImage {
      let key = cacheKey as NSString
      if let hit = avatars.object(forKey: key) { return hit }
      let image = NSImage(
        size: NSSize(width: size, height: size), flipped: false
      ) { rect in
        NSBezierPath(roundedRect: rect, xRadius: 2, yRadius: 2).addClip()
        let sourceSize = source.size
        guard sourceSize.width > 0, sourceSize.height > 0 else { return true }
        let scale = max(size / sourceSize.width, size / sourceSize.height)
        let drawSize = NSSize(width: sourceSize.width * scale, height: sourceSize.height * scale)
        let origin = NSPoint(x: (size - drawSize.width) / 2, y: (size - drawSize.height) / 2)
        source.draw(
          in: NSRect(origin: origin, size: drawSize), from: .zero, operation: .sourceOver,
          fraction: 1)
        return true
      }
      avatars.setObject(image, forKey: key)
      return image
    }

    /// SF Symbol tinted for attributed-string use (attachment images don't
    /// pick up .foregroundColor on macOS).
    static func symbol(_ name: String, pointSize: CGFloat, color: NSColor) -> NSImage {
      let key = "\(name)#\(pointSize)" as NSString
      if let hit = symbols.object(forKey: key) { return hit }
      let image = NSImage(
        size: NSSize(width: pointSize, height: pointSize), flipped: false
      ) { rect in
        guard
          let base = NSImage(systemSymbolName: name, accessibilityDescription: nil)?
            .withSymbolConfiguration(.init(pointSize: pointSize, weight: .regular))
        else { return true }
        base.draw(in: rect, from: .zero, operation: .sourceOver, fraction: 1)
        color.set()
        rect.fill(using: .sourceAtop)
        return true
      }
      symbols.setObject(image, forKey: key)
      return image
    }
  }

  // MARK: - Preview attachments

  /// Attachment whose view is the shared SwiftUI `PreviewCard` (OpenGraph
  /// card or inline image) hosted in an NSHostingView.
  final class PreviewTextAttachment: NSTextAttachment {
    let preview: Preview
    let model: AppModel

    init(preview: Preview, model: AppModel) {
      self.preview = preview
      self.model = model
      super.init(data: nil, ofType: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
      fatalError("not decodable")
    }

    override func viewProvider(
      for parentView: NSView?, location: NSTextLocation, textContainer: NSTextContainer?
    ) -> NSTextAttachmentViewProvider? {
      let provider = PreviewAttachmentViewProvider(
        textAttachment: self, parentView: parentView,
        textLayoutManager: textContainer?.textLayoutManager, location: location)
      provider.tracksTextAttachmentViewBounds = true
      return provider
    }
  }

  /// An inline image growing from its placeholder to the loaded bitmap is the
  /// one post-insertion size change; this relay invalidates layout so TextKit
  /// re-queries attachmentBounds. Guarded against invalidation loops. A
  /// MainActor class so the hosted SwiftUI view can hold it across the
  /// Sendable boundary into NSHostingView.
  @MainActor
  final class PreviewAttachmentResizeRelay {
    weak var layoutManager: NSTextLayoutManager?
    weak var hostView: NSView?
    var location: NSTextLocation?
    private var lastSize: CGSize = .zero

    func fire() {
      guard let hostView else { return }
      let size = hostView.fittingSize
      guard size != lastSize, lastSize != .zero else {
        lastSize = size
        return
      }
      lastSize = size
      if let location {
        layoutManager?.invalidateLayout(for: NSTextRange(location: location))
      }
    }
  }

  final class PreviewAttachmentViewProvider: NSTextAttachmentViewProvider {
    private weak var layoutManager: NSTextLayoutManager?

    override init(
      textAttachment: NSTextAttachment, parentView: NSView?,
      textLayoutManager: NSTextLayoutManager?, location: NSTextLocation
    ) {
      self.layoutManager = textLayoutManager
      super.init(
        textAttachment: textAttachment, parentView: parentView,
        textLayoutManager: textLayoutManager, location: location)
    }

    // NSTextAttachmentViewProvider's overrides are declared nonisolated, but
    // TextKit view hosting always calls them on the main thread — hence the
    // assumeIsolated + unsafe self smuggling (assumeIsolated traps off-main,
    // so a wrong assumption fails loudly, not racily).

    override func loadView() {
      nonisolated(unsafe) let unsafeSelf = self
      MainActor.assumeIsolated {
        guard let attachment = unsafeSelf.textAttachment as? PreviewTextAttachment else { return }
        let relay = PreviewAttachmentResizeRelay()
        relay.layoutManager = unsafeSelf.layoutManager
        relay.location = unsafeSelf.location
        let host = NSHostingView(
          rootView: PreviewAttachmentRoot(
            preview: attachment.preview, model: attachment.model, relay: relay))
        relay.hostView = host
        host.sizingOptions = [.intrinsicContentSize]
        unsafeSelf.view = host
      }
    }

    override func attachmentBounds(
      for attributes: [NSAttributedString.Key: Any], location: NSTextLocation,
      textContainer: NSTextContainer?, proposedLineFragment: CGRect, position: CGPoint
    ) -> CGRect {
      nonisolated(unsafe) let unsafeSelf = self
      return MainActor.assumeIsolated {
        unsafeSelf.view?.layoutSubtreeIfNeeded()
        var size = unsafeSelf.view?.fittingSize ?? .zero
        let available = proposedLineFragment.width - RowMetrics.contentLeft - RowMetrics.inset
        if available > 50 { size.width = min(size.width, available) }
        return CGRect(origin: .zero, size: size)
      }
    }
  }

  /// Hosted SwiftUI root for a preview attachment: the shared PreviewCard
  /// plus a geometry probe that tells the relay when the content resized.
  private struct PreviewAttachmentRoot: View {
    let preview: Preview
    let model: AppModel
    let relay: PreviewAttachmentResizeRelay

    var body: some View {
      PreviewCard(preview: preview)
        .environment(model)
        .onGeometryChange(for: CGSize.self, of: \.size) { _ in
          MainActor.assumeIsolated { relay.fire() }
        }
    }
  }

  /// NSAttributedString mirror of `attributedBody` (ConversationView.swift):
  /// mIRC segment formatting plus NSDataDetector link runs. SwiftUI-scoped
  /// attributes do not bridge to AppKit, so the builder is duplicated rather
  /// than converted; keep the two in sync.
  @MainActor
  func nsMessageBody(_ message: Message, baseColor: NSColor = .labelColor) -> NSAttributedString {
    let result = NSMutableAttributedString()
    let segments =
      message.segments?.isEmpty == false ? message.segments! : [MircSegment(text: message.content)]
    let baseFont = TimelineNSFonts.message
    for segment in segments {
      var attributes: [NSAttributedString.Key: Any] = [
        .font: baseFont,
        .foregroundColor: baseColor,
      ]
      var traits: NSFontDescriptor.SymbolicTraits = []
      if segment.bold == true { traits.insert(.bold) }
      if segment.italic == true { traits.insert(.italic) }
      if !traits.isEmpty,
        let styled = NSFont(
          descriptor: baseFont.fontDescriptor.withSymbolicTraits(traits), size: baseFont.pointSize)
      {
        attributes[.font] = styled
      }
      if segment.underline == true {
        attributes[.underlineStyle] = NSUnderlineStyle.single.rawValue
      }
      if segment.strike == true {
        attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue
      }
      if let foreground = segment.fg {
        attributes[.foregroundColor] = NSColor(mircColor(foreground))
      }
      result.append(NSAttributedString(string: segment.text, attributes: attributes))
    }
    // Detector ranges are UTF-16 offsets into the joined segment text, which
    // is exactly `result.string` — they apply directly.
    if let detector = TimelineFormatters.linkDetector {
      let plainText = result.string
      for match in detector.matches(
        in: plainText, range: NSRange(plainText.startIndex..., in: plainText))
      {
        guard let url = match.url else { continue }
        result.addAttributes(
          [
            .link: url,
            .foregroundColor: NSColor.linkColor,
            .underlineStyle: NSUnderlineStyle.single.rawValue,
          ], range: match.range)
      }
    }
    return result
  }

#endif
