import XCTest

@MainActor
final class LurkerUITests: XCTestCase {
  private var app: XCUIApplication!

  override func setUpWithError() throws {
    continueAfterFailure = false
    app = XCUIApplication()
    app.launchArguments = ["-ui-testing"]
    app.launch()
  }

  func testDailyDriverLayout() {
    XCTAssertTrue(app.staticTexts["#lurker"].waitForExistence(timeout: 5))
    XCTAssertTrue(app.staticTexts["Native client development"].exists)
    XCTAssertTrue(app.staticTexts["Members"].exists)
    XCTAssertTrue(app.staticTexts["The native client is connected."].exists)
    XCTAssertTrue(app.textFields["Message #lurker"].exists)
  }

  func testChannelSwitcherOpens() {
    app.typeKey("k", modifierFlags: .command)
    XCTAssertTrue(app.textFields["Jump to a channel or conversation"].waitForExistence(timeout: 2))
    XCTAssertTrue(app.staticTexts["Libera"].exists)
  }
}
