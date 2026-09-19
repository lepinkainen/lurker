import AppKit
import XCTest

@MainActor
final class LurkerUITests: XCTestCase {

  // MARK: Internal

  override func setUp() async throws {
    continueAfterFailure = false
    app = XCUIApplication()
    app.launchArguments = ["-ui-testing"]
    if name.contains("LiveIRC") {
      app.launchArguments += [
        "-ui-testing-live",
        "-mac.serverURL",
        "http://127.0.0.1:18081",
      ]
    }
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

  func testLiveIRCIncomingMessageScrollsAtBottom() throws {
    selectBuffer("#timeline-scroll")
    let lastRow = messageRow(containing: "incoming IRC line #49")
    XCTAssertTrue(lastRow.waitForExistence(timeout: 5))
    let scrollView = try XCTUnwrap(app.scrollViews.allElementsBoundByIndex
      .max(by: { $0.frame.width < $1.frame.width }))
    let firstRow = messageRow(containing: "incoming IRC line #0")
    XCTAssertTrue(firstRow.exists)
    XCTAssertLessThan(firstRow.frame.maxY, scrollView.frame.minY, "fixture must overflow the viewport")
    XCTAssertTrue(scrollView.scrollBars.firstMatch.exists, "overflowing timeline needs a scrollbar")

    // Physically reach the bottom, as a reader would. AX existence alone is
    // insufficient: the timeline exposes even offscreen message rows.
    for _ in 0..<40 {
      if scrollView.frame.insetBy(dx: -1, dy: -1).contains(lastRow.frame) {
        break
      }
      timeline.scroll(byDeltaX: 0, deltaY: -20)
    }
    let atBottom = NSPredicate { _, _ in
      scrollView.frame.insetBy(dx: -1, dy: -1).contains(lastRow.frame)
    }
    expectation(for: atBottom, evaluatedWith: app)
    waitForExpectations(timeout: 5)

    // Reading through the backlog clears its marker bar. The next incoming
    // message recreates that bar while the reader is at the bottom; this
    // viewport resize is part of the regression scenario.
    let unreadBar = app.buttons.matching(
      NSPredicate(format: "label CONTAINS[c] %@ OR label CONTAINS[c] %@", "new since", "new messages")
    ).firstMatch
    XCTAssertTrue(unreadBar.exists, "seeded IRC backlog should start unread")
    timeline.click()
    app.typeKey(.escape, modifierFlags: [])
    let cleared = XCTNSPredicateExpectation(
      predicate: NSPredicate(format: "exists == false"),
      object: unreadBar,
    )
    XCTAssertEqual(XCTWaiter.wait(for: [cleared], timeout: 5), .completed)
    for _ in 0..<40 {
      if scrollView.frame.insetBy(dx: -1, dy: -1).contains(lastRow.frame) {
        break
      }
      timeline.scroll(byDeltaX: 0, deltaY: -20)
    }
    XCTAssertTrue(
      scrollView.frame.insetBy(dx: -1, dy: -1).contains(lastRow.frame),
      "last backlog row must be visible after catching up",
    )

    func injectFromIRC(_ content: String) throws {
      let inject = Process()
      inject.executableURL = URL(fileURLWithPath: "/usr/bin/nc")
      inject.arguments = ["127.0.0.1", "16668"]
      let input = Pipe()
      inject.standardInput = input
      try inject.run()
      input.fileHandleForWriting.write(Data("#timeline-scroll :\(content)\n".utf8))
      try input.fileHandleForWriting.close()
      inject.waitUntilExit()
    }

    // Receive a message from bob without typing or sending anything.
    // fakeircd streams it through the production backend into the client.
    let content = "incoming scroll regression from another IRC user"
    try injectFromIRC(content)
    let incoming = messageRow(containing: content)
    XCTAssertTrue(incoming.waitForExistence(timeout: 5), "incoming event never reached the timeline")
    XCTAssertTrue(incoming.label.localizedCaseInsensitiveContains("bob"), "fixture message must be from another user")
    // The app intentionally follows after the safe-area + TextKit layout.
    // Check once that transition has settled, rather than as soon as text
    // storage exposes the row.
    Thread.sleep(forTimeInterval: 0.5)
    let incomingFrame = incoming.frame
    let currentScrollView = try XCTUnwrap(app.scrollViews.allElementsBoundByIndex
      .max(by: { $0.frame.width < $1.frame.width }))
    let visible = currentScrollView.frame.insetBy(dx: -1, dy: -1).contains(incomingFrame)
    screenshot(named: "apple-incoming-scroll")
    XCTAssertTrue(visible, "Incoming row from bob at \(incomingFrame) stayed outside \(currentScrollView.frame)")

    // Following stops once the reader scrolls away. A second IRC arrival must
    // keep this visible row at the same screen position.
    let readingAnchor = messageRow(containing: "incoming IRC line #35")
    for _ in 0..<8 {
      timeline.scroll(byDeltaX: 0, deltaY: 20)
    }
    XCTAssertTrue(readingAnchor.exists)
    let anchorBefore = readingAnchor.frame
    XCTAssertTrue(currentScrollView.frame.intersects(anchorBefore))
    XCTAssertFalse(currentScrollView.frame.contains(incoming.frame))
    let secondContent = "second incoming message while reader is scrolled up"
    try injectFromIRC(secondContent)
    XCTAssertTrue(messageRow(containing: secondContent).waitForExistence(timeout: 5))
    Thread.sleep(forTimeInterval: 0.5)
    let anchorAfter = messageRow(containing: "incoming IRC line #35").frame
    XCTAssertLessThan(
      abs(anchorAfter.minY - anchorBefore.minY),
      10,
      "incoming IRC message moved the reader's viewport away from the current row",
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

  func testComposerCompletionAndMultilineEditing() {
    selectBuffer("#lurker")
    let unreadBar = app.buttons.matching(
      NSPredicate(format: "label CONTAINS %@ OR label BEGINSWITH %@", "new message", "new since")
    ).firstMatch
    XCTAssertTrue(unreadBar.waitForExistence(timeout: 5))
    let composer = app.textFields.matching(
      NSPredicate(format: "placeholderValue == %@", "#lurker")
    ).firstMatch
    XCTAssertTrue(composer.waitForExistence(timeout: 5))
    app.typeKey("l", modifierFlags: .command)
    composer.typeText("tov")
    app.typeKey(.tab, modifierFlags: [])
    composer.typeText("hello")
    XCTAssertEqual(composer.value as? String, "tove: hello")

    let singleLineHeight = composer.frame.height
    app.typeKey(.return, modifierFlags: .option)
    composer.typeText("second line")
    XCTAssertEqual(composer.value as? String, "tove: hello\nsecond line")
    XCTAssertGreaterThan(composer.frame.height, singleLineHeight)
    app.typeKey(.escape, modifierFlags: [])
    XCTAssertFalse(unreadBar.exists)
    composer.typeText(".")
    XCTAssertEqual(composer.value as? String, "tove: hello\nsecond line.")
  }

  func testComposerPastesImagesAndText() throws {
    selectBuffer("#lurker")
    let composer = app.textFields.matching(
      NSPredicate(format: "placeholderValue == %@", "#lurker")
    ).firstMatch
    XCTAssertTrue(composer.waitForExistence(timeout: 5))
    composer.click()
    composer.typeText("draft")
    XCTAssertEqual(composer.value as? String, "draft")

    let pasteboard = NSPasteboard.general
    let savedItems = pasteboard.pasteboardItems?.map { item in
      item.types.compactMap { type in item.data(forType: type).map { (type, $0) } }
    } ?? []
    defer {
      pasteboard.clearContents()
      pasteboard.writeObjects(savedItems.map { values in
        let item = NSPasteboardItem()
        for (type, data) in values { item.setData(data, forType: type) }
        return item
      })
    }

    let bitmap = try XCTUnwrap(NSBitmapImageRep(
      bitmapDataPlanes: nil,
      pixelsWide: 2,
      pixelsHigh: 2,
      bitsPerSample: 8,
      samplesPerPixel: 4,
      hasAlpha: true,
      isPlanar: false,
      colorSpaceName: .deviceRGB,
      bytesPerRow: 0,
      bitsPerPixel: 0,
    ))
    let url = "https://fixture.local/uploads/test.jpg"
    for (index, format) in [NSBitmapImageRep.FileType.png, .tiff].enumerated() {
      pasteboard.clearContents()
      pasteboard.setData(
        try XCTUnwrap(bitmap.representation(using: format, properties: [:])),
        forType: index == 0 ? .png : .tiff,
      )
      if index == 0 {
        app.typeKey("v", modifierFlags: .command)
      } else {
        app.menuBars.menuBarItems["Edit"].click()
        let paste = app.menuItems["Paste"]
        XCTAssertTrue(paste.isEnabled)
        paste.click()
      }
      let expected = "draft " + String(repeating: url + " ", count: index + 1)
      expectation(for: NSPredicate(format: "value == %@", expected), evaluatedWith: composer)
      waitForExpectations(timeout: 5)
    }

    // Native text paste must still replace the selection and support Undo.
    pasteboard.clearContents()
    pasteboard.setString("ordinary text", forType: .string)
    composer.click()
    app.typeKey("a", modifierFlags: .command)
    app.typeKey("v", modifierFlags: .command)
    XCTAssertEqual(composer.value as? String, "ordinary text")
    app.typeKey("z", modifierFlags: .command)
    XCTAssertEqual(composer.value as? String, "draft " + url + " " + url + " ")

    // Paste in another field must not attach anything to the composer.
    app.typeKey("k", modifierFlags: .command)
    let search = app.textFields["Jump to a channel or conversation"]
    XCTAssertTrue(search.waitForExistence(timeout: 2))
    search.click()
    app.typeKey("v", modifierFlags: .command)
    XCTAssertEqual(search.value as? String, "ordinary text")
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
