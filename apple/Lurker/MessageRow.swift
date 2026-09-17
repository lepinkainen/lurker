import Foundation
import SwiftUI

// MARK: - MessageRow

struct MessageRow: View {

  // MARK: Internal

  let message: Message
  let buffer: Buffer?

  var body: some View {
    layout
      .padding(.horizontal, 11)
      .padding(.vertical, 2)
      .background(highlightColor)
      .contextMenu {
        Button("Copy Message") {
          Clipboard.copy(message.content)
        }
        if !message.sender.isEmpty {
          Button("Copy Nickname") {
            Clipboard.copy(message.sender)
          }
          Divider()
          Button("Mute \(message.sender)") {
            model.mute(nick: message.sender, in: message.networkID)
          }
          Button("Unmute \(message.sender)") {
            model.unmute(nick: message.sender, in: message.networkID)
          }
        }
      }
      .accessibilityElement(children: .combine)
      .accessibilityLabel("\(message.sender), \(displayTime(message.ts)), \(message.content)")
  }

  // MARK: Private

  @Environment(AppModel.self) private var model

  /// iOS-only since the macOS timeline moved to TimelineTextView.swift: the
  /// nick sits on its own line above the body with the timestamp trailing it,
  /// so the body wraps at full width on a compact screen.
  @ViewBuilder
  private var layout: some View {
    if message.displayKind == "sys" {
      HStack(alignment: .firstTextBaseline, spacing: 6) {
        systemIcon
        systemBody
        Spacer(minLength: 4)
        timestamp
      }
    } else {
      VStack(alignment: .leading, spacing: 2) {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
          HStack(alignment: .firstTextBaseline, spacing: 4) {
            NickAvatar(
              nick: message.sender,
              colorIndex: message.senderColor,
              isBot: model.isBot(message.sender),
              networkID: message.networkID,
              hasAvatar: model.hasAvatar(message.sender),
            )
            .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 2 }
            Text(message.sender)
              .font(Theme.Fonts.nick.weight(message.isSelf == true ? .bold : .semibold))
              .foregroundStyle(nickPaletteColor(message.senderColor))
              .lineLimit(1)
              .truncationMode(.tail)
          }
          Spacer(minLength: 4)
          timestamp
        }
        messageBody
        embeds
      }
    }
  }

  private var timestamp: some View {
    Text(displayTime(message.ts))
      .font(Theme.Fonts.timestamp)
      .foregroundStyle(.tertiary)
      .accessibilityHidden(true)
  }

  private var systemIcon: some View {
    Image(systemName: systemSymbol)
      .font(.caption)
      .foregroundStyle(.secondary)
  }

  private var systemBody: some View {
    Text(systemText)
      .font(Theme.Fonts.message)
      .foregroundStyle(.secondary)
      .textSelection(.enabled)
  }

  private var messageBody: some View {
    Text(attributedBody(message))
      .font(Theme.Fonts.message)
      .foregroundStyle(message.displayKind == "action" ? .purple : .primary)
      .textSelection(.enabled)
  }

  @ViewBuilder
  private var embeds: some View {
    if buffer?.showEmbeds != false {
      ForEach(message.previews ?? []) { preview in
        PreviewCard(preview: preview)
      }
    }
  }

  private var highlightColor: Color {
    message.mentionsMe == true || message.highlight == true ? .orange.opacity(0.10) : .clear
  }

  private var systemSymbol: String {
    systemMessageSymbol(message)
  }

  private var systemText: String {
    systemMessageText(message)
  }

}

/// SF Symbol name for a system-event message, shared by the SwiftUI row and
/// the macOS NSTextView timeline.
func systemMessageSymbol(_ message: Message) -> String {
  switch message.kind {
  case "join": "arrow.right"
  case "part",
       "quit": "arrow.left"
  case "kick": "figure.fall"
  case "topic": "text.quote"
  case "connected": "bolt.horizontal.circle"
  case "disconnected": "bolt.slash"
  case "away": "moon.zzz"
  case "back": "sun.max"
  case "nick": "person.text.rectangle"
  case "account": "person.crop.circle.badge.checkmark"
  case "chghost": "at"
  default: "info.circle"
  }
}

/// Human-readable text for a system-event message, shared by the SwiftUI row
/// and the macOS NSTextView timeline.
func systemMessageText(_ message: Message) -> String {
  let target = message.target ?? ""
  switch message.kind {
  case "away":
    return message.content.isEmpty
      ? "\(message.sender) is away"
      : "\(message.sender) is away (\(message.content))"

  case "back":
    return "\(message.sender) is back"

  case "nick" where !target.isEmpty:
    return "\(message.sender) is now known as \(target)"

  case "account":
    return message.content.isEmpty
      ? "\(message.sender) logged out"
      : "\(message.sender) logged in as \(message.content)"

  case "chghost":
    return "\(message.sender) changed host to \(message.content)"

  default:
    return [message.sender, message.content.isEmpty ? message.kind : message.content]
      .filter { !$0.isEmpty }
      .joined(separator: " ")
  }
}

// MARK: - PreviewCard

struct PreviewCard: View {

  // MARK: Internal

  let preview: Preview

  var body: some View {
    // Backend preview kinds are "image" (render the URL itself inline) and
    // "opengraph" (card); anything else is dropped (web parity: preview.ts).
    switch preview.kind {
    case "image":
      // Image URLs the client refuses to load inline (plain http) still get
      // the card so the preview isn't silently dropped.
      if let imageURL = model.inlineImageURL(preview) {
        linked { InlineImageView(url: imageURL) }
      } else {
        linked { card }
      }

    case "opengraph":
      linked { card }

    default:
      EmptyView()
    }
  }

  // MARK: Private

  @Environment(AppModel.self) private var model

  private var card: some View {
    HStack(spacing: 10) {
      if let imageURL = model.previewImageURL(preview) {
        CachedAsyncImage(url: imageURL) { image in
          image.resizable().scaledToFill()
        } placeholder: {
          Color.secondary.opacity(0.08)
        }
        .frame(width: 72, height: 54)
        .clipped()
        .clipShape(.rect(cornerRadius: 6))
      }
      VStack(alignment: .leading, spacing: 2) {
        Text(preview.siteName ?? URL(string: preview.url)?.host() ?? "Link")
          .font(.caption.weight(.semibold))
          .foregroundStyle(.secondary)
        Text(preview.title ?? preview.description ?? preview.url)
          .font(.body)
          .foregroundStyle(.primary)
          .lineLimit(2)
      }
      Spacer(minLength: 0)
    }
    .padding(8)
    .frame(maxWidth: 430)
    .background(.quaternary.opacity(0.5), in: .rect(cornerRadius: 8))
    .overlay {
      RoundedRectangle(cornerRadius: 8)
        .stroke(.separator, lineWidth: 0.5)
    }
  }

  @ViewBuilder
  private func linked(@ViewBuilder content: () -> some View) -> some View {
    if let destination = URL(string: preview.url) {
      Link(destination: destination) {
        content()
      }
      .buttonStyle(.plain)
      #if os(macOS)
      .pointerStyle(.link)
      #endif
    } else {
      content()
    }
  }

}

// MARK: - InlineImageView

/// Full inline rendering for kind == "image" previews: the preview URL is
/// the image (web parity: renderImagePreview, max 480×320, contain-fit).
struct InlineImageView: View {
  let url: URL

  var body: some View {
    CachedAsyncImage(
      url: url,
      content: { image in
        image
          .resizable()
          .scaledToFit()
          .frame(maxWidth: 480, maxHeight: 320, alignment: .leading)
          .clipShape(.rect(cornerRadius: 8))
          .overlay {
            RoundedRectangle(cornerRadius: 8)
              .stroke(.separator, lineWidth: 0.5)
          }
      },
      placeholder: {
        // Fixed-size placeholder: server width/height are usually 0 for
        // image previews, so they can't drive layout.
        Color.secondary.opacity(0.08)
          .frame(width: 240, height: 135)
          .clipShape(.rect(cornerRadius: 8))
      },
      failure: {
        // Broken image: nothing — the raw link stays in the message text.
        EmptyView()
      },
    )
  }
}
