import SwiftUI

/// Centralized layout metrics + type system, shared by the sidebar, timeline,
/// composer, and inspector.
///
/// Two rules keep this honest:
/// - **Scale** comes from semantic SwiftUI text styles only (`.caption` →
///   `.footnote` → `.body` → `.title3`…), never hardcoded point sizes, so
///   Dynamic Type and OS metric updates keep working.
/// - **Emphasis is weight; de-emphasis is color** (`.secondary` /
///   `.tertiary`), never a lighter font weight.
///
/// When you find yourself typing the same number a third time, it belongs
/// here as a named metric. New UI should reach for these tokens rather than
/// hardcoding a font or padding value that already has a name here.
enum Theme {
  ///
  /// The whole app's typography lives here so hierarchy stays consistent and
  /// a future restyle lands in one place.
  enum Fonts {
    /// Monospaced identifier text: the nick beside a message, the composer's
    /// own-nick label, member-list nicks, channel-list entries, and the
    /// nick/command rows in composer autocomplete. Weight and color are
    /// layered per call site for emphasis (self, selection).
    static let nick = Font.body.monospaced()

    /// Monospaced message/content text: message bodies, system-event text,
    /// the composer's live input, and autocomplete emoji/argument text.
    static let message = Font.body.monospaced()

    /// Small monospaced-digit timestamp shown in the message row's fixed
    /// gutter.
    static let timestamp = Font.caption.monospacedDigit()

    /// The unread/mention count pill's label (`CountBadge`).
    static let badge = Font.caption.monospacedDigit().weight(.semibold)

    /// Muted, semibold caption used for sidebar section labels: "Pinned",
    /// "Disabled", and the "Archives" fold row.
    static let sectionHeader = Font.caption.weight(.semibold)

    /// Small glyph size for inline row icons: the network row's
    /// collapse chevron and overflow-menu ellipsis.
    static let smallIcon = Font.caption.weight(.semibold)
  }

  /// Horizontal inset for a full-width header/separator row: the sidebar's
  /// bottom status bar and section headers, and the conversation's header,
  /// day separator, unread separator, and presence summary. Previously drifted
  /// between 12 (sidebar) and 14 (conversation); standardized on 12, which
  /// narrows the conversation header/separators by 2pt.
  static let rowHorizontalInset: CGFloat = 12

  /// Leading indent for a sidebar row nested under a network header: joined
  /// channels, queries, and the "Archives" fold row.
  static let sidebarChildIndent: CGFloat = 14

  /// Horizontal padding shared by the unread/mention count pill (`CountBadge`)
  /// and the "ARCHIVED" chip in the conversation header.
  static let badgeHorizontalPadding: CGFloat = 5
}
