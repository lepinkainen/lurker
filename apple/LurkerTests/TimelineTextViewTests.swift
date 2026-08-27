import Foundation
import Testing

@testable import Lurker

#if os(macOS)

  import AppKit

  private func makeMessage(
    id: UUID = UUID(),
    networkID: UUID = UUID(),
    sender: String = "tove",
    kind: String = "privmsg",
    content: String,
    displayKind: String = "message",
    segments: [MircSegment]? = nil,
    previews: [Lurker.Preview]? = nil
  ) -> Message {
    Message(
      id: id,
      networkID: networkID,
      bufferID: UUID(),
      ts: "2026-07-23T08:12:00Z",
      sender: sender,
      kind: kind,
      content: content,
      displayKind: displayKind,
      segments: segments,
      previews: previews
    )
  }

  private func makeBuffer(markerID: UUID? = nil, collapsePresence: Bool = false) -> Buffer {
    Buffer(
      id: UUID(),
      networkID: UUID(),
      name: "#lurker",
      kind: "channel",
      joined: true,
      markerID: markerID,
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: collapsePresence,
      pinned: false,
      unread: 0,
      mentions: 0
    )
  }

  struct TimelineDiffTests {
    @Test func identicalListsAreNoOp() {
      let items: [TimelineItem] = [.message(makeMessage(content: "hello"))]
      #expect(TimelineDiff.compute(old: items, new: items) == .none)
    }

    @Test func strictSuffixIsAnAppend() {
      let first = TimelineItem.message(makeMessage(content: "one"))
      let second = TimelineItem.message(makeMessage(content: "two"))
      let diff = TimelineDiff.compute(old: [first], new: [first, second])
      #expect(diff == .incremental(replacements: [], appendFrom: 1))
    }

    @Test func previewArrivalReplacesInPlace() {
      var message = makeMessage(content: "see https://example.com")
      let before = TimelineItem.message(message)
      message.previews = [
        Lurker.Preview(url: "https://example.com", kind: "opengraph", title: "Example")
      ]
      let after = TimelineItem.message(message)
      let diff = TimelineDiff.compute(old: [before], new: [after])
      #expect(diff == .incremental(replacements: [0], appendFrom: nil))
    }

    @Test func presenceRunGrowthReplacesTheLastBlockAndAppends() {
      let join1 = makeMessage(sender: "a", kind: "join", content: "", displayKind: "sys")
      let join2 = makeMessage(sender: "b", kind: "join", content: "", displayKind: "sys")
      let chat = makeMessage(content: "hi")
      // Collapsed run keyed by its first member: growing it keeps the id but
      // changes the content, so the block is replaced, and the new message
      // appends after it.
      let old: [TimelineItem] = [.presence(join1.id, [join1, join2])]
      let join3 = makeMessage(sender: "c", kind: "join", content: "", displayKind: "sys")
      let new: [TimelineItem] = [.presence(join1.id, [join1, join2, join3]), .message(chat)]
      let diff = TimelineDiff.compute(old: old, new: new)
      #expect(diff == .incremental(replacements: [0], appendFrom: 1))
    }

    @Test func historyPrependForcesRebuild() {
      let older = TimelineItem.message(makeMessage(content: "older"))
      let newer = TimelineItem.message(makeMessage(content: "newer"))
      #expect(TimelineDiff.compute(old: [newer], new: [older, newer]) == .rebuild)
    }

    @Test func markerMoveForcesRebuild() {
      let message = makeMessage(content: "hello")
      let old: [TimelineItem] = [.message(message)]
      let new: [TimelineItem] = [.unread("unread-\(message.id.uuidString)"), .message(message)]
      #expect(TimelineDiff.compute(old: old, new: new) == .rebuild)
    }
  }

  struct NSMessageBodyTests {
    // Mirror of WireModelTests.linkRunsCoverOnlyTheURLText for the AppKit
    // builder: clickability is driven by which runs carry `.link`.
    @Test @MainActor func linkRunsCoverOnlyTheURLText() {
      let message = makeMessage(
        content:
          "inline links: https://example.com/lurker and https://news.ycombinator.com/item?id=1"
      )
      let rendered = nsMessageBody(message)
      var linkRuns: [(text: String, url: String)] = []
      rendered.enumerateAttribute(
        .link, in: NSRange(location: 0, length: rendered.length)
      ) { value, range, _ in
        guard let url = value as? URL else { return }
        linkRuns.append(((rendered.string as NSString).substring(with: range), url.absoluteString))
      }
      #expect(
        linkRuns.map(\.text) == [
          "https://example.com/lurker", "https://news.ycombinator.com/item?id=1",
        ])
      #expect(
        linkRuns.map(\.url) == [
          "https://example.com/lurker", "https://news.ycombinator.com/item?id=1",
        ])
    }

    @Test @MainActor func linksUseTheStrippedSegmentText() {
      let message = makeMessage(
        content: "\u{02}bold\u{02} https://example.com",
        segments: [
          MircSegment(text: "bold", bold: true),
          MircSegment(text: " https://example.com"),
        ]
      )
      let rendered = nsMessageBody(message)
      #expect(rendered.string == "bold https://example.com")
      var boldFont: NSFont?
      rendered.enumerateAttribute(.font, in: NSRange(location: 0, length: 4)) { value, _, _ in
        boldFont = value as? NSFont
      }
      #expect(boldFont?.fontDescriptor.symbolicTraits.contains(.bold) == true)
    }

    @Test @MainActor func plainMessagesHaveNoLinkRuns() {
      let rendered = nsMessageBody(makeMessage(content: "no links here"))
      var found = false
      rendered.enumerateAttribute(
        .link, in: NSRange(location: 0, length: rendered.length)
      ) { value, _, _ in
        if value != nil { found = true }
      }
      #expect(!found)
    }
  }

  @MainActor
  private func makeContext(
    buffer: Buffer? = nil, expandedGroups: Set<UUID> = []
  ) -> TimelineRenderContext {
    let defaults = UserDefaults(suiteName: "xyz.endymion.lurker.tests.\(UUID().uuidString)")!
    let model = AppModel(transport: FixtureTransport(), defaults: defaults)
    return TimelineRenderContext(
      buffer: buffer ?? makeBuffer(), model: model, expandedGroups: expandedGroups)
  }

  struct TimelineBlockTests {
    @Test @MainActor func messageBlockCarriesGutterNickBodyAndID() {
      let message = makeMessage(content: "hello world")
      let block = timelineBlockText(.message(message), context: makeContext())
      #expect(block.string.hasSuffix("tove hello world\n"))
      #expect(block.string.hasPrefix("\t"))
      let id = block.attribute(.lurkerMessageID, at: 0, effectiveRange: nil) as? String
      #expect(id == message.id.uuidString)
    }

    @Test @MainActor func unreadSeparatorIsExcludedFromCopyAndCarriesRule() {
      let block = timelineBlockText(.unread("unread-x"), context: makeContext())
      #expect(block.string == "New Messages\n")
      #expect(block.attribute(.lurkerCopyExclude, at: 0, effectiveRange: nil) != nil)
      #expect(block.attribute(.lurkerSeparatorRule, at: 0, effectiveRange: nil) is NSColor)
    }

    @Test @MainActor func previewsRenderAsExcludedAttachmentParagraphs() {
      let message = makeMessage(
        content: "see https://example.com",
        previews: [
          Lurker.Preview(url: "https://example.com", kind: "opengraph", title: "Example Site")
        ]
      )
      let block = timelineBlockText(.message(message), context: makeContext())
      var attachments: [PreviewTextAttachment] = []
      block.enumerateAttribute(
        .attachment, in: NSRange(location: 0, length: block.length)
      ) { value, range, _ in
        if let attachment = value as? PreviewTextAttachment {
          attachments.append(attachment)
          #expect(
            block.attribute(.lurkerCopyExclude, at: range.location, effectiveRange: nil) != nil)
        }
      }
      #expect(attachments.map(\.preview.url) == ["https://example.com"])
    }

    @Test @MainActor func embedsHiddenWhenBufferDisablesThem() {
      var buffer = makeBuffer()
      buffer.showEmbeds = false
      let message = makeMessage(
        content: "see https://example.com",
        previews: [
          Lurker.Preview(url: "https://example.com", kind: "opengraph", title: "Example Site")
        ]
      )
      let block = timelineBlockText(.message(message), context: makeContext(buffer: buffer))
      var found = false
      block.enumerateAttribute(
        .attachment, in: NSRange(location: 0, length: block.length)
      ) { value, _, _ in
        if value is PreviewTextAttachment { found = true }
      }
      #expect(!found)
    }

    @Test @MainActor func mentionRowsCarryTheFullWidthHighlightTag() {
      var message = makeMessage(content: "hey shrike")
      message.mentionsMe = true
      let block = timelineBlockText(.message(message), context: makeContext())
      #expect(block.attribute(.lurkerRowHighlight, at: 0, effectiveRange: nil) is NSColor)
    }

    @Test @MainActor func presenceSummaryTogglesArrowAndCarriesInternalLink() {
      let join1 = makeMessage(sender: "a", kind: "join", content: "", displayKind: "sys")
      let join2 = makeMessage(sender: "b", kind: "join", content: "", displayKind: "sys")
      let item = TimelineItem.presence(join1.id, [join1, join2])

      let collapsed = timelineBlockText(item, context: makeContext())
      #expect(collapsed.string.hasPrefix("▸ "))
      let link = collapsed.attribute(.link, at: 0, effectiveRange: nil) as? URL
      #expect(link?.scheme == "lurker-presence")
      #expect(link?.host()?.lowercased() == join1.id.uuidString.lowercased())

      let expanded = timelineBlockText(
        item, context: makeContext(expandedGroups: [join1.id]))
      #expect(expanded.string.hasPrefix("▾ "))
    }

    @Test @MainActor func expandedPresenceGroupEmitsMemberRows() {
      let join1 = makeMessage(sender: "a", kind: "join", content: "", displayKind: "sys")
      let join2 = makeMessage(sender: "b", kind: "join", content: "", displayKind: "sys")
      let buffer = makeBuffer(collapsePresence: true)

      let collapsed = timelineItems([join1, join2], buffer: buffer)
      #expect(collapsed.count == 2)  // day separator + summary

      let expanded = timelineItems([join1, join2], buffer: buffer, expandedGroups: [join1.id])
      #expect(expanded.count == 4)  // day + summary + two member rows
      #expect(expanded[2] == .message(join1))
      #expect(expanded[3] == .message(join2))
    }
  }

  /// A live coordinator wired to real (headless) AppKit views, fed from a
  /// directly-mutated AppModel — the same objects updateNSView hands it.
  @MainActor
  private struct CoordinatorHarness {
    let coordinator = TimelineCoordinator()
    let textView: TimelineNSTextView
    let scrollView: NSScrollView
    let model: AppModel
    let buffer: Buffer

    init(buffer: Buffer, messages: [Message]) {
      let defaults = UserDefaults(suiteName: "xyz.endymion.lurker.tests.\(UUID().uuidString)")!
      model = AppModel(transport: nil, defaults: defaults, runsConnectionLoop: false)
      self.buffer = buffer
      model.buffers[buffer.id] = buffer
      model.messages[buffer.id] = messages
      model.selectedBufferID = buffer.id

      textView = TimelineNSTextView(usingTextLayoutManager: true)
      textView.isEditable = false
      textView.textContainer?.widthTracksTextView = true
      textView.textContainer?.lineFragmentPadding = 0
      textView.isVerticallyResizable = true
      textView.isHorizontallyResizable = false
      textView.autoresizingMask = [.width]
      textView.minSize = .zero
      textView.maxSize = NSSize(
        width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
      scrollView = TimelineScrollView(frame: NSRect(x: 0, y: 0, width: 400, height: 200))
      scrollView.documentView = textView
      coordinator.install(
        textView: textView, scrollView: scrollView, axHost: TimelineAXHostView())
    }

    /// Mirrors updateNSView: derive items from the model, hand them to sync.
    func sync() {
      coordinator.sync(
        items: timelineItems(
          model.selectedMessages, buffer: buffer,
          expandedGroups: coordinator.expandedPresenceGroups),
        buffer: buffer, model: model)
    }

    var renderedText: String { textView.textStorage?.string ?? "" }

    /// Test-side mirror of the coordinator's forceFullLayout so assertions
    /// about pinning see the document's true height even when the code under
    /// test never triggered a layout pass.
    func forceLayout() {
      guard let layout = textView.textLayoutManager else { return }
      layout.enumerateTextLayoutFragments(from: nil, options: [.ensuresLayout]) { _ in true }
      let height = layout.usageBoundsForTextContainer.maxY + textView.textContainerInset.height * 2
      if abs(textView.frame.height - height) > 0.5 {
        textView.setFrameSize(NSSize(width: textView.frame.width, height: height))
      }
    }

    var isPinnedToBottom: Bool {
      scrollView.documentVisibleRect.maxY >= textView.frame.maxY - 40
    }
  }

  struct TimelineCoordinatorTests {
    @Test @MainActor func botFlagArrivingAfterRenderRefreshesRows() {
      let buffer = makeBuffer()
      let message = makeMessage(networkID: buffer.networkID, content: "beep boop")
      let harness = CoordinatorHarness(buffer: buffer, messages: [message])
      harness.sync()
      #expect(!harness.renderedText.contains("🤖"))

      // Bot mode lands via a member-list snapshot after the row rendered; the
      // message list itself is unchanged, so the diff sees nothing to do.
      harness.model.apply(
        .members(
          MemberListEvent(
            networkID: buffer.networkID, bufferID: buffer.id,
            members: [Member(nick: "tove", away: false, self: false, bot: true)])))
      harness.sync()
      #expect(harness.renderedText.contains("🤖"))
    }

    @Test @MainActor func replacementGrowthKeepsViewportPinnedToBottom() {
      let buffer = makeBuffer()
      var messages = (0..<40).map {
        makeMessage(networkID: buffer.networkID, content: "line \($0)")
      }
      let harness = CoordinatorHarness(buffer: buffer, messages: messages)
      harness.sync()
      harness.forceLayout()
      #expect(harness.isPinnedToBottom)

      // Same id, taller content: a replacement-only diff, like a late preview.
      messages[39].content = String(repeating: "a much longer wrapped line ", count: 40)
      harness.model.messages[buffer.id] = messages
      harness.sync()
      harness.forceLayout()
      #expect(harness.isPinnedToBottom)
    }

    @Test @MainActor func expandingTerminalPresenceGroupRedrawsItsArrow() {
      let buffer = makeBuffer(collapsePresence: true)
      let join1 = makeMessage(
        networkID: buffer.networkID, sender: "a", kind: "join", content: "", displayKind: "sys")
      let join2 = makeMessage(
        networkID: buffer.networkID, sender: "b", kind: "join", content: "", displayKind: "sys")
      let harness = CoordinatorHarness(buffer: buffer, messages: [join1, join2])
      harness.sync()
      #expect(harness.renderedText.contains("▸"))

      let toggle = URL(string: "lurker-presence://\(join1.id.uuidString)")!
      _ = harness.coordinator.textView(harness.textView, clickedOnLink: toggle, at: 0)
      #expect(harness.renderedText.contains("▾"))
      #expect(!harness.renderedText.contains("▸"))
    }
  }

#endif
