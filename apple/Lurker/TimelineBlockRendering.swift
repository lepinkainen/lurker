#if os(macOS)

import AppKit
import SwiftUI

// MARK: - Block rendering

/// AppKit mirrors of `Theme.Fonts`, derived from the semantic text styles so
/// OS metric changes keep working (Theme rule: no hardcoded point sizes).
enum TimelineNSFonts {
  static var message: NSFont {
    .monospacedSystemFont(
      ofSize: NSFont.preferredFont(forTextStyle: .body).pointSize,
      weight: .regular,
    )
  }

  static var timestamp: NSFont {
    .monospacedDigitSystemFont(
      ofSize: NSFont.preferredFont(forTextStyle: .caption1).pointSize,
      weight: .regular,
    )
  }

  static var presenceSummary: NSFont {
    .monospacedSystemFont(
      ofSize: NSFont.preferredFont(forTextStyle: .footnote).pointSize,
      weight: .regular,
    )
  }

  static func nick(isSelf: Bool) -> NSFont {
    .monospacedSystemFont(
      ofSize: NSFont.preferredFont(forTextStyle: .body).pointSize,
      weight: isSelf ? .bold : .medium,
    )
  }

  static func footnote(_ weight: NSFont.Weight) -> NSFont {
    .systemFont(ofSize: NSFont.preferredFont(forTextStyle: .footnote).pointSize, weight: weight)
  }
}

/// Row geometry shared by every block: an 11pt leading inset, a 42pt
/// right-aligned timestamp gutter, then the nick/body column — the same
/// metrics as the SwiftUI `MessageRow`.
enum RowMetrics {
  static let inset: CGFloat = 11
  static let gutterRight: CGFloat = inset + 42 // 53
  static let contentLeft: CGFloat = gutterRight + 8 // 61
}

@MainActor
private enum RowStyles {

  // MARK: Internal

  static let message: NSParagraphStyle = {
    let style = NSMutableParagraphStyle()
    style.tabStops = [
      NSTextTab(textAlignment: .right, location: RowMetrics.gutterRight, options: [:]),
      NSTextTab(textAlignment: .left, location: RowMetrics.contentLeft, options: [:]),
    ]
    style.headIndent = RowMetrics.contentLeft
    style.tailIndent = -RowMetrics.inset
    style.paragraphSpacing = 4
    return style
  }()

  static let centered: NSParagraphStyle = separator(spacing: 9)
  static let unread: NSParagraphStyle = separator(spacing: 5)

  static let indented: NSParagraphStyle = {
    let style = NSMutableParagraphStyle()
    style.firstLineHeadIndent = RowMetrics.contentLeft
    style.headIndent = RowMetrics.contentLeft
    style.tailIndent = -RowMetrics.inset
    style.paragraphSpacing = 4
    return style
  }()

  // MARK: Private

  private static func separator(spacing: CGFloat) -> NSParagraphStyle {
    let style = NSMutableParagraphStyle()
    style.alignment = .center
    style.paragraphSpacingBefore = spacing
    style.paragraphSpacing = spacing
    return style
  }

}

/// Renders one `TimelineItem` as an attributed paragraph (or several, for a
/// message with previews). Every builder appends a final "\n" separator;
/// the coordinator omits it at the document end and restores it on append.
@MainActor
func timelineBlockText(
  _ item: TimelineItem,
  context: TimelineRenderContext,
) -> NSAttributedString {
  switch item {
  case .day(_, let title):
    return separatorLine(
      title,
      color: .secondaryLabelColor,
      rule: .separatorColor,
      style: RowStyles.centered,
      weight: .medium,
    )

  case .unread:
    let line = separatorLine(
      "New Messages",
      color: .systemOrange,
      rule: NSColor.systemOrange.withAlphaComponent(0.35),
      style: RowStyles.unread,
      weight: .semibold,
    )
    let excluded = NSMutableAttributedString(attributedString: line)
    excluded.addAttribute(
      .lurkerCopyExclude,
      value: true,
      range: NSRange(location: 0, length: excluded.length),
    )
    return excluded

  case .presence(let id, let messages):
    let expanded = context.expandedGroups.contains(id)
    let arrow = expanded ? "▾" : "▸"
    let text = NSMutableAttributedString(
      string: "\(arrow) \(presenceSummaryText(messages))",
      attributes: [
        .font: TimelineNSFonts.presenceSummary,
        .foregroundColor: NSColor.secondaryLabelColor,
        .paragraphStyle: RowStyles.indented,
        // Internal toggle link: activation + hand cursor for free; styling
        // stays secondary because linkTextAttributes only sets the cursor.
        .link: URL(string: "lurker-presence://\(id.uuidString)")!,
      ],
    )
    text.append(
      NSAttributedString(
        string: "\n",
        attributes: [
          .font: TimelineNSFonts.presenceSummary,
          .paragraphStyle: RowStyles.indented,
        ],
      )
    )
    return text

  case .message(let message):
    return messageBlock(message, context: context)
  }
}

@MainActor
private func separatorLine(
  _ title: String,
  color: NSColor,
  rule: NSColor,
  style: NSParagraphStyle,
  weight: NSFont.Weight,
) -> NSAttributedString {
  NSAttributedString(
    string: title + "\n",
    attributes: [
      .font: TimelineNSFonts.footnote(weight),
      .foregroundColor: color,
      .paragraphStyle: style,
      .lurkerSeparatorRule: rule,
    ],
  )
}

@MainActor
private func messageBlock(
  _ message: Message,
  context: TimelineRenderContext,
) -> NSAttributedString {
  let block = NSMutableAttributedString()
  let time = displayTime(message.ts)

  block.append(
    NSAttributedString(
      string: "\t\(time)\t",
      attributes: [
        .font: TimelineNSFonts.timestamp,
        .foregroundColor: NSColor.tertiaryLabelColor,
        .paragraphStyle: RowStyles.message,
      ],
    )
  )

  if message.displayKind == "sys" {
    block.append(
      systemIconAttachment(for: message, font: TimelineNSFonts.message)
    )
    block.append(
      NSAttributedString(
        string: " " + systemMessageText(message),
        attributes: [
          .font: TimelineNSFonts.message,
          .foregroundColor: NSColor.secondaryLabelColor,
          .paragraphStyle: RowStyles.message,
        ],
      )
    )
  } else {
    let nickFont = TimelineNSFonts.nick(isSelf: message.isSelf == true)
    block.append(avatarRun(for: message, model: context.model, font: nickFont))
    var nickAttributes: [NSAttributedString.Key: Any] = [
      .font: nickFont,
      .foregroundColor: NSColor(nickPaletteColor(message.senderColor)),
      .paragraphStyle: RowStyles.message,
      .toolTip: message.userhost ?? message.sender,
    ]
    if message.userhost == nil && message.sender.isEmpty {
      nickAttributes.removeValue(forKey: .toolTip)
    }
    block.append(NSAttributedString(string: message.sender, attributes: nickAttributes))
    block.append(
      NSAttributedString(
        string: " ",
        attributes: [.font: TimelineNSFonts.message, .paragraphStyle: RowStyles.message],
      )
    )

    let baseColor: NSColor =
      message.displayKind == "action" ? .systemPurple : .labelColor
    let body = NSMutableAttributedString(
      attributedString: nsMessageBody(
        message,
        baseColor: baseColor,
      )
    )
    body.addAttribute(
      .paragraphStyle,
      value: RowStyles.message,
      range: NSRange(location: 0, length: body.length),
    )
    block.append(body)
  }

  block.append(
    NSAttributedString(
      string: "\n",
      attributes: [.font: TimelineNSFonts.message, .paragraphStyle: RowStyles.message],
    )
  )

  // Preview cards render as their own attachment paragraphs inside the same
  // block, so a `.preview` event is a plain block replacement. Excluded from
  // Copy — the raw URL is already in the message text.
  if context.buffer?.showEmbeds != false {
    for preview in message.previews ?? []
      where preview.kind == "image" || preview.kind == "opengraph"
    {
      block.append(previewParagraph(preview, model: context.model))
    }
  }

  if message.mentionsMe == true || message.highlight == true {
    // Painted at full container width by LurkerLayoutFragment; covers the
    // preview paragraphs too, like the SwiftUI row background did.
    block.addAttribute(
      .lurkerRowHighlight,
      value: NSColor.systemOrange.withAlphaComponent(0.10),
      range: NSRange(location: 0, length: block.length),
    )
  }

  block.addAttribute(
    .lurkerMessageID,
    value: message.id.uuidString,
    range: NSRange(location: 0, length: block.length),
  )
  return block
}

/// 14×14 avatar square before the nick (bot glyph / server avatar /
/// identicon), aligned like the SwiftUI row's firstTextBaseline guide.
@MainActor
private func avatarRun(for message: Message, model: AppModel, font: NSFont) -> NSAttributedString {
  if model.isBot(message.sender) {
    return NSAttributedString(
      string: "🤖 ",
      attributes: [.font: font, .paragraphStyle: RowStyles.message],
    )
  }
  let image: NSImage =
    if
      model.hasAvatar(message.sender),
      let url = model.avatarURL(networkID: message.networkID, nick: message.sender),
      let cached = ImageCache.shared.cached(for: url)
    {
      AvatarImages.rounded(cached, cacheKey: url.absoluteString)
    } else {
      // Identicon now; `kickAvatarLoads` re-renders the row if a server
      // avatar arrives later.
      AvatarImages.identicon(nick: message.sender, colorIndex: message.senderColor)
    }
  let attachment = NSTextAttachment()
  attachment.image = image
  let size: CGFloat = 14
  attachment.bounds = CGRect(
    x: 0,
    y: (font.capHeight - size) / 2,
    width: size,
    height: size,
  )
  let run = NSMutableAttributedString(attachment: attachment)
  run.append(NSAttributedString(string: " ", attributes: [.font: font]))
  run.addAttribute(
    .paragraphStyle,
    value: RowStyles.message,
    range: NSRange(location: 0, length: run.length),
  )
  return run
}

@MainActor
private func systemIconAttachment(for message: Message, font: NSFont) -> NSAttributedString {
  let attachment = NSTextAttachment()
  let size: CGFloat = 12
  attachment.image = AvatarImages.symbol(
    systemMessageSymbol(message),
    pointSize: size,
    color: .secondaryLabelColor,
  )
  attachment.bounds = CGRect(
    x: 0,
    y: (font.capHeight - size) / 2,
    width: size,
    height: size,
  )
  let run = NSMutableAttributedString(attachment: attachment)
  run.addAttribute(
    .paragraphStyle,
    value: RowStyles.message,
    range: NSRange(location: 0, length: run.length),
  )
  return run
}

/// One paragraph per preview: a live SwiftUI card hosted through
/// `NSTextAttachmentViewProvider`.
@MainActor
private func previewParagraph(_ preview: Preview, model: AppModel) -> NSAttributedString {
  let attachment = PreviewTextAttachment(preview: preview, model: model)
  let paragraph = NSMutableAttributedString(attachment: attachment)
  paragraph.append(NSAttributedString(string: "\n"))
  paragraph.addAttributes(
    [
      .paragraphStyle: RowStyles.indented,
      .font: TimelineNSFonts.message,
      .lurkerCopyExclude: true,
    ],
    range: NSRange(location: 0, length: paragraph.length),
  )
  return paragraph
}

#endif
