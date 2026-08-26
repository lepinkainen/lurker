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
    /// Runs excluded from Copy (unread separator, preview placeholders).
    static let lurkerCopyExclude = NSAttributedString.Key("lurkerCopyExclude")
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

    func makeNSView(context: Context) -> NSScrollView {
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
      textView.linkTextAttributes = [
        .foregroundColor: NSColor.linkColor,
        .underlineStyle: NSUnderlineStyle.single.rawValue,
        .cursor: NSCursor.pointingHand,
      ]

      let scrollView = NSScrollView()
      scrollView.documentView = textView
      scrollView.hasVerticalScroller = true
      scrollView.drawsBackground = true
      scrollView.backgroundColor = .textBackgroundColor
      scrollView.contentView.postsBoundsChangedNotifications = true

      context.coordinator.install(textView: textView, scrollView: scrollView)
      return scrollView
    }

    func updateNSView(_ scrollView: NSScrollView, context: Context) {
      // Reading `selectedMessages` (and, inside sync, `historyAnchor`) here
      // registers SwiftUI observation, so model mutations re-invoke this.
      let items = timelineItems(model.selectedMessages, buffer: buffer)
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
    private var blocks: [RenderedBlock] = []
    private var renderedBufferID: UUID?
    private var renderedFingerprint: Fingerprint?
    private(set) var buffer: Buffer?
    private(set) var model: AppModel?

    func install(textView: TimelineNSTextView, scrollView: NSScrollView) {
      self.textView = textView
      self.scrollView = scrollView
      textView.coordinator = self
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
        rebuild(items)
        scrollToBottom()
        // An anchor addressed to another buffer is stale: drop it without
        // scrolling rather than leaving it to block further load-older calls.
        consumeAnchor(model, ownedBy: nil)
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
    }

    // MARK: Storage edits

    private func rebuild(_ items: [TimelineItem]) {
      guard let textView, let storage = textView.textStorage else { return }
      let document = NSMutableAttributedString()
      blocks = items.map { item in
        let block = timelineBlockText(item, buffer: buffer)
        document.append(block)
        return RenderedBlock(item: item, length: block.length)
      }
      storage.setAttributedString(document)
    }

    private func replaceBlock(at index: Int, with item: TimelineItem) {
      guard let textView, let storage = textView.textStorage else { return }
      let block = timelineBlockText(item, buffer: buffer)
      let range = NSRange(location: offset(of: index), length: blocks[index].length)
      textView.textContentStorage?.performEditingTransaction {
        storage.replaceCharacters(in: range, with: block)
      }
      blocks[index] = RenderedBlock(item: item, length: block.length)
    }

    private func appendBlocks(_ items: ArraySlice<TimelineItem>) {
      guard let textView, let storage = textView.textStorage else { return }
      let appended = NSMutableAttributedString()
      for item in items {
        let block = timelineBlockText(item, buffer: buffer)
        appended.append(block)
        blocks.append(RenderedBlock(item: item, length: block.length))
      }
      textView.textContentStorage?.performEditingTransaction {
        storage.append(appended)
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

    private func scrollToBottom(animated: Bool = false) {
      guard let textView, let layout = textView.textLayoutManager else { return }
      // Full layout so the document height is exact, not estimated; rebuilds
      // are rare (buffer switch, history page) and appends are cheap.
      layout.ensureLayout(for: layout.documentRange)
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
      layout.ensureLayout(for: layout.documentRange)
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
  func timelineBlockText(_ item: TimelineItem, buffer: Buffer?) -> NSAttributedString {
    switch item {
    case .day(_, let title):
      return separatorLine(
        title, color: .secondaryLabelColor, style: RowStyles.centered,
        weight: .medium)
    case .unread:
      let line = separatorLine(
        "New Messages", color: .systemOrange, style: RowStyles.unread,
        weight: .semibold)
      let excluded = NSMutableAttributedString(attributedString: line)
      excluded.addAttribute(
        .lurkerCopyExclude, value: true,
        range: NSRange(location: 0, length: excluded.length))
      return excluded
    case .presence(_, let messages):
      let text = NSMutableAttributedString(
        string: presenceSummaryText(messages) + "\n",
        attributes: [
          .font: TimelineNSFonts.presenceSummary,
          .foregroundColor: NSColor.secondaryLabelColor,
          .paragraphStyle: RowStyles.indented,
        ])
      return text
    case .message(let message):
      return messageBlock(message, buffer: buffer)
    }
  }

  @MainActor
  private func separatorLine(
    _ title: String, color: NSColor, style: NSParagraphStyle, weight: NSFont.Weight
  ) -> NSAttributedString {
    NSAttributedString(
      string: title + "\n",
      attributes: [
        .font: TimelineNSFonts.footnote(weight),
        .foregroundColor: color,
        .paragraphStyle: style,
      ])
  }

  @MainActor
  private func messageBlock(_ message: Message, buffer: Buffer?) -> NSAttributedString {
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
        NSAttributedString(
          string: systemMessageText(message),
          attributes: [
            .font: TimelineNSFonts.message,
            .foregroundColor: NSColor.secondaryLabelColor,
            .paragraphStyle: RowStyles.message,
          ]))
    } else {
      var nickAttributes: [NSAttributedString.Key: Any] = [
        .font: TimelineNSFonts.nick(isSelf: message.isSelf == true),
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

    if message.mentionsMe == true || message.highlight == true {
      // ponytail: glyph-only tint; full-row width needs a custom
      // NSTextLayoutFragment, planned with the rich-content pass.
      block.addAttribute(
        .backgroundColor, value: NSColor.systemOrange.withAlphaComponent(0.10),
        range: NSRange(location: 0, length: block.length))
    }

    // Interim preview rendering: one link line per preview until the
    // attachment-based cards land. Excluded from Copy — the raw URL is
    // already in the message text.
    if buffer?.showEmbeds != false {
      for preview in message.previews ?? []
      where preview.kind == "image" || preview.kind == "opengraph" {
        block.append(previewLine(preview))
      }
    }

    block.addAttribute(
      .lurkerMessageID, value: message.id.uuidString,
      range: NSRange(location: 0, length: block.length))
    return block
  }

  @MainActor
  private func previewLine(_ preview: Preview) -> NSAttributedString {
    let title = preview.title ?? preview.siteName ?? preview.url
    var attributes: [NSAttributedString.Key: Any] = [
      .font: TimelineNSFonts.footnote(.regular),
      .foregroundColor: NSColor.linkColor,
      .paragraphStyle: RowStyles.indented,
      .lurkerCopyExclude: true,
    ]
    if let url = URL(string: preview.url) {
      attributes[.link] = url
    }
    let line = NSMutableAttributedString(string: "↗ \(title)", attributes: attributes)
    line.append(
      NSAttributedString(
        string: "\n",
        attributes: [
          .font: TimelineNSFonts.footnote(.regular),
          .paragraphStyle: RowStyles.indented,
          .lurkerCopyExclude: true,
        ]))
    return line
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
