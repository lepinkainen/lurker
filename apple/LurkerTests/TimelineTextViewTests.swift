import Foundation
import Testing

@testable import Lurker

#if os(macOS)

  import AppKit

  private func makeMessage(
    id: UUID = UUID(),
    sender: String = "tove",
    kind: String = "privmsg",
    content: String,
    displayKind: String = "message",
    segments: [MircSegment]? = nil,
    previews: [Lurker.Preview]? = nil
  ) -> Message {
    Message(
      id: id,
      networkID: UUID(),
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

  struct TimelineBlockTests {
    @Test @MainActor func messageBlockCarriesGutterNickBodyAndID() {
      let message = makeMessage(content: "hello world")
      let block = timelineBlockText(.message(message), buffer: makeBuffer())
      #expect(block.string.hasSuffix("tove hello world\n"))
      #expect(block.string.hasPrefix("\t"))
      let id = block.attribute(.lurkerMessageID, at: 0, effectiveRange: nil) as? String
      #expect(id == message.id.uuidString)
    }

    @Test @MainActor func unreadSeparatorIsExcludedFromCopy() {
      let block = timelineBlockText(.unread("unread-x"), buffer: makeBuffer())
      #expect(block.string == "New Messages\n")
      let excluded = block.attribute(.lurkerCopyExclude, at: 0, effectiveRange: nil)
      #expect(excluded != nil)
    }

    @Test @MainActor func previewsRenderAsExcludedLinkLines() {
      let message = makeMessage(
        content: "see https://example.com",
        previews: [
          Lurker.Preview(url: "https://example.com", kind: "opengraph", title: "Example Site")
        ]
      )
      let block = timelineBlockText(.message(message), buffer: makeBuffer())
      #expect(block.string.contains("↗ Example Site"))
      let previewStart = (block.string as NSString).range(of: "↗").location
      let excluded = block.attribute(.lurkerCopyExclude, at: previewStart, effectiveRange: nil)
      #expect(excluded != nil)
      let link = block.attribute(.link, at: previewStart, effectiveRange: nil) as? URL
      #expect(link?.absoluteString == "https://example.com")
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
      let block = timelineBlockText(.message(message), buffer: buffer)
      #expect(!block.string.contains("↗"))
    }
  }

#endif
