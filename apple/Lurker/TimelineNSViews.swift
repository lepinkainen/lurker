#if os(macOS)

import AppKit
import SwiftUI

/// Read-only text view adding the per-message context menu, Esc-to-ack, and
/// copy sanitization on top of stock NSTextView behavior.
final class TimelineNSTextView: NSTextView {

  // MARK: Internal

  weak var coordinator: TimelineCoordinator?

  override func menu(for event: NSEvent) -> NSMenu? {
    guard let storage = textStorage, storage.length > 0 else {
      return super.menu(for: event)
    }
    let point = convert(event.locationInWindow, from: nil)
    // Hit-testing returns insertion positions, including one past the last
    // character when clicking to the right of the terminal message.
    let index = min(characterIndexForInsertion(at: point), storage.length - 1)
    guard let message = coordinator?.message(atCharacterIndex: index) else {
      return super.menu(for: event)
    }
    let menu = NSMenu()
    menu.addItem(item("Copy Message", #selector(TimelineCoordinator.copyMessage), message))
    if !message.sender.isEmpty {
      menu.addItem(item("Copy Nickname", #selector(TimelineCoordinator.copyNickname), message))
      menu.addItem(.separator())
      menu.addItem(
        item("Mute \(message.sender)", #selector(TimelineCoordinator.muteSender), message)
      )
      menu.addItem(
        item("Unmute \(message.sender)", #selector(TimelineCoordinator.unmuteSender), message)
      )
    }
    if selectedRange().length > 0 {
      menu.addItem(.separator())
      menu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "")
    }
    return menu
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
    var pieces = [String]()
    for range in ranges {
      let sub = storage.attributedSubstring(from: range)
      var out = ""
      sub.enumerateAttributes(in: NSRange(location: 0, length: sub.length)) { attrs, r, _ in
        if attrs[.lurkerCopyExclude] != nil {
          return
        }
        if attrs[.attachment] != nil {
          return
        }
        out += (sub.string as NSString).substring(with: r)
      }
      let lines = out.components(separatedBy: "\n")
        .map { $0.replacingOccurrences(of: "\t", with: " ").trimmingCharacters(in: .whitespaces) }
        .filter { !$0.isEmpty }
      if !lines.isEmpty {
        pieces.append(lines.joined(separator: "\n"))
      }
    }
    guard !pieces.isEmpty else { return }
    Clipboard.copy(pieces.joined(separator: "\n"))
  }

  // MARK: Private

  private func item(_ title: String, _ action: Selector, _ message: Message) -> NSMenuItem {
    let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
    item.target = coordinator
    item.representedObject = message
    return item
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

  override func hitTest(_: NSPoint) -> NSView? {
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

#endif
