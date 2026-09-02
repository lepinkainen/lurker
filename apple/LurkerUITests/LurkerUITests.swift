import AppKit
import XCTest

@MainActor
final class LurkerUITests: XCTestCase {

  // MARK: Internal

  override func setUp() async throws {
    continueAfterFailure = false
    app = XCUIApplication()
    app.launchArguments = ["-ui-testing"]
    app.launch()
  }

  func testDailyDriverLayout() {
    // Pinned channels render once in Pinned and once under their network. The
    // occurrences need distinct SwiftUI identities or the lazy stack leaves a
    // blank row where the network copy should be. Rows are Buttons whose
    // label folds in the badge ("#lurker, 1 mentions").
    let lurkerRows = app.buttons.matching(
      NSPredicate(format: "label == %@ OR label BEGINSWITH %@", "#lurker", "#lurker,")
    )
    XCTAssertTrue(lurkerRows.firstMatch.waitForExistence(timeout: 5))
    XCTAssertEqual(lurkerRows.count, 2)

    // The rest of the assertions need #lurker's conversation on screen.
    selectBuffer("#lurker")

    // The macOS timeline is a single NSTextView; each message is exposed as
    // an AX child with the combined "sender, time, content" label.
    XCTAssertTrue(timeline.waitForExistence(timeout: 5))
    let message = messageRow(containing: "The native client is connected.")
    XCTAssertTrue(message.waitForExistence(timeout: 5))

    // The header topic uses `lineLimit(1)`, so its rendered value truncates when the
    // detail column is narrow ("Native client deve…"). Match a prefix that always fits.
    let topic = app.staticTexts.matching(
      NSPredicate(format: "value BEGINSWITH %@", "Native client")
    ).firstMatch
    XCTAssertTrue(topic.exists)

    // Members inspector.
    XCTAssertTrue(app.staticTexts["Members"].exists)

    // Composer for the selected channel (its placeholder is the buffer name).
    let composer = app.textFields.matching(
      NSPredicate(format: "placeholderValue == %@", "#lurker")
    ).firstMatch
    XCTAssertTrue(composer.exists)
  }

  /// Archived fixtures (#old-project channel, driveby query) render behind a
  /// folded per-network Archives row instead of inline in the channel list.
  func testArchivesFoldHidesAndRevealsArchivedBuffers() {
    XCTAssertTrue(sidebarRow("#lurker").waitForExistence(timeout: 5))

    let archivesRow = app.buttons.matching(
      NSPredicate(format: "label BEGINSWITH %@", "Archives")
    ).firstMatch
    XCTAssertTrue(archivesRow.waitForExistence(timeout: 3), "missing Archives fold row")

    // archivesOpen persists across launches; a previously aborted run can
    // leave the fold open. Normalize to folded before asserting the default.
    if sidebarRow("#old-project").exists {
      archivesRow.click()
    }

    // Folded by default: archived buffers are not in the sidebar.
    XCTAssertFalse(sidebarRow("#old-project").exists)
    XCTAssertFalse(sidebarRow("driveby").exists)
    screenshot(named: "apple-archives-folded")

    archivesRow.click()
    XCTAssertTrue(sidebarRow("#old-project").waitForExistence(timeout: 3))
    XCTAssertTrue(sidebarRow("driveby").exists)
    screenshot(named: "apple-archives-open")

    // Context menu on the archived channel: Unarchive + Delete….
    sidebarRow("#old-project").rightClick()
    XCTAssertTrue(app.menuItems["Delete…"].waitForExistence(timeout: 3))
    XCTAssertTrue(app.menuItems["Unarchive"].exists)
    screenshot(named: "apple-archived-context-menu")
    app.menuItems["Delete…"].click()

    // Destructive confirmation alert, then cancel (fixture transport would
    // ignore the send anyway; the UI contract is what we verify).
    let deleteForever = app.buttons["Delete Forever"]
    XCTAssertTrue(deleteForever.waitForExistence(timeout: 3), "missing confirmation alert")
    screenshot(named: "apple-delete-alert")
    // `app.buttons["Cancel"]` is ambiguous — the Touch Bar exposes one too —
    // so dismiss the alert with ⎋ (equivalent to Cancel).
    app.typeKey(.escape, modifierFlags: [])

    // Joined channels offer Archive instead.
    sidebarRow("#lurker").rightClick()
    XCTAssertTrue(app.menuItems["Archive"].waitForExistence(timeout: 3))
    XCTAssertFalse(app.menuItems["Delete…"].exists)
    // Dismiss the menu.
    app.typeKey(.escape, modifierFlags: [])

    // Fold back so persisted archivesOpen state doesn't leak into other runs.
    archivesRow.click()
  }

  /// Loading an older history page must keep the viewport anchored on the
  /// previously-oldest message; without that the scroll position stays at the
  /// top of the grown content and pagination runs away page after page.
  /// Message AX rows exist for every *loaded* message (the NSTextView exposes
  /// all blocks, rendered or not), so existence asserts loading and frames
  /// assert the viewport position.
  func testHistoryLoadAnchorsScrollPosition() throws {
    // The sidebar row is a Button whose label folds in the unread badge
    // ("#lurker-full, 10 unread messages").
    let fullRow = app.buttons.matching(
      NSPredicate(format: "label BEGINSWITH %@", "#lurker-full")
    ).firstMatch
    XCTAssertTrue(fullRow.waitForExistence(timeout: 5))
    fullRow.click()

    // Initial page is the newest 50 of 400 fixture messages (#350–#399).
    XCTAssertTrue(messageRow(containing: "backlog line #399:").waitForExistence(timeout: 5))
    XCTAssertFalse(
      messageRow(containing: "backlog line #349:").exists,
      "older page loaded prematurely",
    )

    // Scroll to the top edge; crossing the threshold triggers the older-page
    // fetch (instant in fixtures) and merges #300–#349. Delta size matters:
    // the final gesture keeps coasting after the anchor restore, so a big
    // delta (40) drags the viewport hundreds of points past the anchored row
    // and fails the frame assert, while a tiny one (5) never reaches the top.
    let olderRow = messageRow(containing: "backlog line #349:")
    var attempts = 0
    while !olderRow.exists, attempts < 150 {
      timeline.scroll(byDeltaX: 0, deltaY: 15)
      attempts += 1
    }
    XCTAssertTrue(olderRow.waitForExistence(timeout: 2), "older page never merged")

    // The anchor restore pins the previously-oldest visible message (#350)
    // back to the top edge of the viewport.
    sleep(2)
    screenshot(named: "apple-history-anchor")
    let scrollView = try XCTUnwrap(app.scrollViews.allElementsBoundByIndex
      .max(by: { $0.frame.width < $1.frame.width }))
    let anchored = messageRow(containing: "backlog line #350:")
    XCTAssertTrue(anchored.exists, "anchored row missing")
    let offset = anchored.frame.minY - scrollView.frame.minY
    XCTAssertLessThan(offset, 150, "previously-oldest row not anchored near the top")
    XCTAssertGreaterThan(offset, -50, "previously-oldest row scrolled above the viewport")

    // Runaway pagination would keep fetching page after page all the way to
    // the very start of the backlog.
    XCTAssertFalse(
      messageRow(containing: "backlog line #0:").exists,
      "pagination ran away to the start",
    )
  }

  /// Bare ↑ in the composer recalls the last sent message (per-buffer input
  /// history). Guards the key-event seam: the macOS field editor must not
  /// swallow the arrow before the history handler sees it.
  func testComposerArrowUpRecallsSentMessage() {
    selectBuffer("#lurker")

    let composer = app.textFields.matching(
      NSPredicate(format: "placeholderValue == %@", "#lurker")
    ).firstMatch
    XCTAssertTrue(composer.waitForExistence(timeout: 5))
    composer.click()
    composer.typeText("hello history")
    app.typeKey(.return, modifierFlags: [])

    // The send clears the composer (FixtureTransport accepts it).
    let cleared = NSPredicate(format: "value == %@ OR value == %@", "", "#lurker")
    expectation(for: cleared, evaluatedWith: composer)
    waitForExpectations(timeout: 3)

    app.typeKey(.upArrow, modifierFlags: [])
    XCTAssertEqual(
      composer.value as? String,
      "hello history",
      "arrow-up did not recall the sent message from input history",
    )
  }

  func testChannelSwitcherOpens() {
    app.typeKey("k", modifierFlags: .command)
    XCTAssertTrue(app.textFields["Jump to a channel or conversation"].waitForExistence(timeout: 2))
    XCTAssertTrue(app.staticTexts["Libera"].exists)
  }

  /// Inline links flip the cursor to the pointing hand (NSTextView
  /// linkTextAttributes). Link-run geometry is not exposed to accessibility,
  /// so the test sweeps the pointer across the timeline and samples the
  /// system cursor at each stop.
  func testPointerBecomesHandOverInlineLink() {
    selectBuffer("#lurker")
    XCTAssertTrue(timeline.waitForExistence(timeout: 5))
    expectation(
      for: NSPredicate(format: "value CONTAINS %@", "with a preview card:"),
      evaluatedWith: timeline,
    )
    waitForExpectations(timeout: 5)
    XCTAssertTrue(
      sweepFindsPointingHand(in: timeline),
      "expected the pointing-hand cursor over an inline link; samples: \(sweepLog.joined(separator: " | "))",
    )
  }

  // MARK: Private

  private var app: XCUIApplication!

  private var sweepLog = [String]()

  /// The macOS timeline NSTextView — the only text area in the window (the
  /// composer is a text field).
  private var timeline: XCUIElement {
    app.textViews.firstMatch
  }

  private func screenshot(named name: String) {
    let shot = XCUIScreen.main.screenshot()
    try? shot.pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/\(name).png"))
  }

  /// A message's AX row (label "sender, time, content"), exposed as an
  /// accessibility child of the timeline text view.
  private func messageRow(containing text: String) -> XCUIElement {
    app.descendants(matching: .any)
      .matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
  }

  /// Sidebar rows are Buttons whose label folds in the badge
  /// ("#lurker, 1 mentions"), so match the bare name or a "name," prefix.
  private func sidebarRow(_ name: String) -> XCUIElement {
    app.buttons.matching(
      NSPredicate(format: "label == %@ OR label BEGINSWITH %@", name, name + ",")
    ).firstMatch
  }

  /// Clicks a sidebar row. The previous selection persists across launches,
  /// so tests asserting on conversation content must select explicitly.
  private func selectBuffer(_ name: String) {
    let row = sidebarRow(name)
    XCTAssertTrue(row.waitForExistence(timeout: 5), "missing sidebar row \(name)")
    row.click()
  }

  private func sweepFindsPointingHand(in element: XCUIElement) -> Bool {
    let hand = NSCursor.pointingHand.image.tiffRepresentation
    var found = false
    sweepLog = []
    // Dense vertical grid: a single link run is one text line of a full
    // timeline, so coarse rows would step right over it.
    for dy in stride(from: 0.05, through: 0.95, by: 0.05) {
      for dx in stride(from: 0.02, through: 0.98, by: 0.06) {
        element.coordinate(withNormalizedOffset: CGVector(dx: dx, dy: dy)).hover()
        usleep(30_000)
        let current = NSCursor.currentSystem
        let isHand = current?.image.tiffRepresentation == hand
        sweepLog.append(
          "(\(String(format: "%.2f", dx)),\(String(format: "%.2f", dy)))"
            + " cur=\(current == nil ? "nil" : NSStringFromSize(current!.image.size))"
            + " hot=\(current.map { NSStringFromPoint($0.hotSpot) } ?? "-")"
            + (isHand ? " HAND" : "")
        )
        if isHand {
          found = true
        }
      }
    }
    return found
  }

}
