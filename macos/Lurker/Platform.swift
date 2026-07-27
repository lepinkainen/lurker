import SwiftUI

#if os(macOS)
  import AppKit
#else
  import UIKit
#endif

// Cross-platform shims so the shared SwiftUI sources compile for both the macOS
// app and the iOS app. macOS draws on AppKit (`NSColor`, `NSPasteboard`); iOS on
// UIKit (`UIColor`, `UIPasteboard`). Keep all `#if os(...)` color/clipboard
// branching confined to this file so the views stay platform-agnostic.

extension Color {
  /// Background behind the message timeline (the document/content surface).
  static var lurkerTimelineBackground: Color {
    #if os(macOS)
      Color(nsColor: .textBackgroundColor)
    #else
      Color(uiColor: .systemBackground)
    #endif
  }

  /// Hairline separator matching the platform's list/table separators.
  static var lurkerSeparator: Color {
    #if os(macOS)
      Color(nsColor: .separatorColor)
    #else
      Color(uiColor: .separator)
    #endif
  }

  /// Field/control background used by the composer input.
  static var lurkerControlBackground: Color {
    #if os(macOS)
      Color(nsColor: .controlBackgroundColor)
    #else
      Color(uiColor: .secondarySystemBackground)
    #endif
  }

  /// Link tint for detected URLs in message bodies.
  static var lurkerLink: Color {
    #if os(macOS)
      Color(nsColor: .linkColor)
    #else
      Color(uiColor: .link)
    #endif
  }
}

/// Cross-platform clipboard write.
enum Clipboard {
  static func copy(_ string: String) {
    #if os(macOS)
      NSPasteboard.general.clearContents()
      NSPasteboard.general.setString(string, forType: .string)
    #else
      UIPasteboard.general.string = string
    #endif
  }
}
