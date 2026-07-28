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

  func testChannelSwitcherOpens() {
    app.typeKey("k", modifierFlags: .command)
    XCTAssertTrue(app.textFields["Jump to a channel or conversation"].waitForExistence(timeout: 2))
    XCTAssertTrue(app.staticTexts["Libera"].exists)
  }
}
