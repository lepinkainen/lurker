import AppKit
import XCTest

@MainActor
final class LurkerUITests: XCTestCase {
  private var app: XCUIApplication!

  override func setUp() async throws {
    continueAfterFailure = false
    app = XCUIApplication()
    app.launchArguments = ["-ui-testing"]
    app.launch()
  }

  func testDailyDriverLayout() {
    // Sidebar shows the pinned channel.
    XCTAssertTrue(app.staticTexts["#lurker"].waitForExistence(timeout: 5))

    // Conversation renders the fixture message. `MessageRow` combines its
    // sender/time/content into one accessibility element (an `Other` with a
    // "sender, time, content" label), so match that label by substring rather than
    // a bare `StaticText`. The wait also covers the conversation painting a frame
    // after the sidebar.
    let message = app.otherElements.matching(
      NSPredicate(format: "label CONTAINS %@", "The native client is connected.")
    ).firstMatch
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

  // Archived fixtures (#old-project channel, driveby query) render behind a
  // folded per-network Archives row instead of inline in the channel list.
  func testArchivesFoldHidesAndRevealsArchivedBuffers() throws {
    XCTAssertTrue(app.staticTexts["#lurker"].waitForExistence(timeout: 5))

    let archivesRow = app.buttons.matching(
      NSPredicate(format: "label BEGINSWITH %@", "Archives")
    ).firstMatch
    XCTAssertTrue(archivesRow.waitForExistence(timeout: 3), "missing Archives fold row")

    // Folded by default: archived buffers are not in the sidebar.
    XCTAssertFalse(app.staticTexts["#old-project"].exists)
    XCTAssertFalse(app.staticTexts["driveby"].exists)
    screenshot(named: "apple-archives-folded")

    archivesRow.click()
    XCTAssertTrue(app.staticTexts["#old-project"].waitForExistence(timeout: 3))
    XCTAssertTrue(app.staticTexts["driveby"].exists)
    screenshot(named: "apple-archives-open")

    // Context menu on the archived channel: Unarchive + Delete….
    app.staticTexts["#old-project"].rightClick()
    XCTAssertTrue(app.menuItems["Delete…"].waitForExistence(timeout: 3))
    XCTAssertTrue(app.menuItems["Unarchive"].exists)
    screenshot(named: "apple-archived-context-menu")
    app.menuItems["Delete…"].click()

    // Destructive confirmation alert, then cancel (fixture transport would
    // ignore the send anyway; the UI contract is what we verify).
    let deleteForever = app.buttons["Delete Forever"]
    XCTAssertTrue(deleteForever.waitForExistence(timeout: 3), "missing confirmation alert")
    screenshot(named: "apple-delete-alert")
    app.buttons["Cancel"].click()

    // Joined channels offer Archive instead.
    app.staticTexts["#lurker"].firstMatch.rightClick()
    XCTAssertTrue(app.menuItems["Archive"].waitForExistence(timeout: 3))
    XCTAssertFalse(app.menuItems["Delete…"].exists)
    // Dismiss the menu.
    app.typeKey(.escape, modifierFlags: [])

    // Fold back so persisted archivesOpen state doesn't leak into other runs.
    archivesRow.click()
  }

  // Loading an older history page must keep the viewport anchored on the
  // previously-oldest message; without that the scroll position stays at the
  // top of the grown content and pagination runs away page after page.
  func testHistoryLoadAnchorsScrollPosition() {
    // The sidebar row is a Button whose label folds in the unread badge
    // ("#lurker-full, 10 unread messages").
    let fullRow = app.buttons.matching(
      NSPredicate(format: "label BEGINSWITH %@", "#lurker-full")
    ).firstMatch
    XCTAssertTrue(fullRow.waitForExistence(timeout: 5))
    fullRow.click()

    // Initial page is the newest 50 of 400 fixture messages (#350–#399).
    XCTAssertTrue(messageRow(containing: "backlog line #399:").waitForExistence(timeout: 5))

    // `scrollViews.firstMatch` is the sidebar; the timeline is the widest one.
    let scrollView = app.scrollViews.allElementsBoundByIndex
      .max(by: { $0.frame.width < $1.frame.width })!
    let oldestLoaded = messageRow(containing: "backlog line #350:")
    var attempts = 0
    while !oldestLoaded.exists && attempts < 60 {
      scrollView.scroll(byDeltaX: 0, deltaY: attempts < 40 ? 30 : -30)
      attempts += 1
    }
    XCTAssertTrue(oldestLoaded.waitForExistence(timeout: 2), "never reached the oldest loaded row")

    // Reaching #350 triggers the older-page fetch (instant in fixtures); the
    // anchor restore should pin #350 back to the top edge of the viewport.
    sleep(2)
    screenshot(named: "apple-history-anchor")
    XCTAssertTrue(oldestLoaded.exists, "anchored row left the hierarchy")
    let offset = oldestLoaded.frame.minY - scrollView.frame.minY
    XCTAssertLessThan(offset, 150, "previously-oldest row not anchored near the top")
    XCTAssertGreaterThan(offset, -50, "previously-oldest row scrolled above the viewport")

    // Runaway pagination would have loaded all pages and left the viewport at
    // the very start of the backlog.
    let veryFirst = messageRow(containing: "backlog line #0:")
    XCTAssertFalse(veryFirst.exists && veryFirst.isHittable, "pagination ran away to the start")

    // The older page really merged in: one nudge up reveals #349.
    scrollView.scroll(byDeltaX: 0, deltaY: 30)
    XCTAssertTrue(
      messageRow(containing: "backlog line #349:").waitForExistence(timeout: 2),
      "older page missing below the anchor")
  }

  private func screenshot(named name: String) {
    let shot = XCUIScreen.main.screenshot()
    try? shot.pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/\(name).png"))
  }

  func testChannelSwitcherOpens() {
    app.typeKey("k", modifierFlags: .command)
    XCTAssertTrue(app.textFields["Jump to a channel or conversation"].waitForExistence(timeout: 2))
    XCTAssertTrue(app.staticTexts["Libera"].exists)
  }

  // The preview card is one clickable control, but the cursor change happens at
  // arbitrary points inside it, so the test sweeps the pointer across the row and
  // samples the system cursor at each stop.
  func testPointerBecomesHandOverPreviewCard() {
    let cardRow = messageRow(containing: "with a preview card:")
    XCTAssertTrue(cardRow.waitForExistence(timeout: 5))
    XCTAssertTrue(
      sweepFindsPointingHand(in: cardRow),
      "expected the pointing-hand cursor over the preview card or its link; samples: \(sweepLog.joined(separator: " | "))"
    )
  }

  private var sweepLog: [String] = []

  private func messageRow(containing text: String) -> XCUIElement {
    app.descendants(matching: .any)
      .matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
  }

  private func sweepFindsPointingHand(in element: XCUIElement) -> Bool {
    let hand = NSCursor.pointingHand.image.tiffRepresentation
    var found = false
    sweepLog = []
    for dy in stride(from: 0.2, through: 0.8, by: 0.3) {
      for dx in stride(from: 0.02, through: 0.98, by: 0.06) {
        element.coordinate(withNormalizedOffset: CGVector(dx: dx, dy: dy)).hover()
        usleep(30_000)
        let current = NSCursor.currentSystem
        let isHand = current?.image.tiffRepresentation == hand
        sweepLog.append(
          "(\(String(format: "%.2f", dx)),\(String(format: "%.2f", dy)))"
            + " cur=\(current == nil ? "nil" : NSStringFromSize(current!.image.size))"
            + " hot=\(current.map { NSStringFromPoint($0.hotSpot) } ?? "-")"
            + (isHand ? " HAND" : ""))
        if isHand {
          found = true
        }
      }
    }
    return found
  }
}
