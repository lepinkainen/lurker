import Foundation
import Testing

@testable import Lurker

struct InputHistoryTests {
  let buffer = UUID()
  let other = UUID()

  @Test func appendingSpacePadsFromExistingContent() {
    #expect(InputHistory.appending("url", to: "") == "url ")
    #expect(InputHistory.appending("url", to: "text") == "text url ")
    #expect(InputHistory.appending("url", to: "text ") == "text url ")
  }

  @Test func appendToDraftTargetsBufferWithoutSelectingIt() {
    var history = InputHistory()
    history.stashDraft("draft", buffer: buffer)
    history.appendToDraft("https://example.com/a.jpg", buffer: buffer)
    history.appendToDraft("solo", buffer: other)

    #expect(history.restoreDraft(buffer: buffer) == "draft https://example.com/a.jpg ")
    #expect(history.restoreDraft(buffer: other) == "solo ")
  }

  @Test func upRecallsNewestAndStashesDraft() {
    var history = InputHistory()
    history.record("first", buffer: buffer)
    history.record("second", buffer: buffer)

    #expect(history.navigateUp(buffer: buffer, current: "typing…") == "second")
    #expect(history.navigateUp(buffer: buffer, current: "ignored") == "first")
    // Clamped at the oldest entry (still consumed).
    #expect(history.navigateUp(buffer: buffer, current: "ignored") == "first")

    #expect(history.navigateDown(buffer: buffer) == "second")
    // Past the newest: the stashed draft returns and browsing ends.
    #expect(history.navigateDown(buffer: buffer) == "typing…")
    #expect(history.navigateDown(buffer: buffer) == nil)
  }

  @Test func upWithNoHistoryFallsThrough() {
    var history = InputHistory()
    #expect(history.navigateUp(buffer: buffer, current: "draft") == nil)
    #expect(history.navigateDown(buffer: buffer) == nil)
  }

  @Test func capTrimsOldestEntries() {
    var history = InputHistory()
    for i in 0..<(InputHistory.maxEntries + 10) {
      history.record("line \(i)", buffer: buffer)
    }
    var seen: [String] = []
    while let entry = history.navigateUp(buffer: buffer, current: "") {
      if seen.last == entry { break }  // clamped at oldest
      seen.append(entry)
    }
    #expect(seen.count == InputHistory.maxEntries)
    #expect(seen.first == "line 109")
    #expect(seen.last == "line 10")
  }

  @Test func perBufferIsolation() {
    var history = InputHistory()
    history.record("here", buffer: buffer)
    #expect(history.navigateUp(buffer: other, current: "") == nil)
    #expect(history.navigateUp(buffer: buffer, current: "") == "here")
  }

  @Test func recordResetsBrowseIndex() {
    var history = InputHistory()
    history.record("first", buffer: buffer)
    #expect(history.navigateUp(buffer: buffer, current: "draft") == "first")
    history.record("second", buffer: buffer)
    // Browsing restarted from the newest entry.
    #expect(history.navigateUp(buffer: buffer, current: "") == "second")
  }

  @Test func draftStashRestoreRoundTrip() {
    var history = InputHistory()
    history.stashDraft("wip message", buffer: buffer)
    #expect(history.restoreDraft(buffer: buffer) == "wip message")
    #expect(history.restoreDraft(buffer: other) == "")
  }

  @Test func stashIgnoredWhileBrowsing() {
    var history = InputHistory()
    history.record("sent", buffer: buffer)
    #expect(history.navigateUp(buffer: buffer, current: "the draft") == "sent")
    // A buffer switch mid-browse must not clobber the stashed draft with the
    // recalled history entry sitting in the field.
    history.stashDraft("sent", buffer: buffer)
    #expect(history.restoreDraft(buffer: buffer) == "the draft")
  }
}
