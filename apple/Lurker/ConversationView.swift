import Foundation
import SwiftUI

struct ConversationView: View {
  @Environment(AppModel.self) private var model

  var body: some View {
    if let buffer = model.selectedBuffer {
      VStack(spacing: 0) {
        ConversationHeader(buffer: buffer, network: model.selectedNetwork)
        Divider()
        TimelineView(buffer: buffer)
        Divider()
        ComposerView(buffer: buffer)
      }
      .navigationTitle(
        buffer.kind == "status" ? "\(model.selectedNetwork?.name ?? "") Status" : buffer.name)
    } else {
      ContentUnavailableView {
        Label("No Conversation Selected", systemImage: "bubble.left.and.bubble.right")
      } description: {
        Text(
          model.connectionState == .notConfigured
            ? "Choose a Lurker server to begin."
            : "Select a channel or conversation in the sidebar.")
      } actions: {
        if model.configuredURL == nil {
          Button("Set Up Connection") {
            model.showingConnectionEditor = true
          }
        }
      }
    }
  }
}

private struct ConversationHeader: View {
  let buffer: Buffer
  let network: Network?

  var body: some View {
    HStack(alignment: .firstTextBaseline, spacing: 10) {
      VStack(alignment: .leading, spacing: 3) {
        HStack(spacing: 6) {
          Text(buffer.kind == "status" ? network?.name ?? "Status" : buffer.name)
            .font(.title3)
          if buffer.kind == "channel", !buffer.joined {
            Text("ARCHIVED")
              .font(.caption.weight(.bold))
              .foregroundStyle(.secondary)
              .padding(.horizontal, 5)
              .padding(.vertical, 2)
              .background(.quaternary, in: .rect(cornerRadius: 3))
          }
        }
        Text(subtitle)
          .font(.footnote)
          .foregroundStyle(.secondary)
          .lineLimit(1)
          .textSelection(.enabled)
      }
      Spacer()
    }
    .padding(.horizontal, 14)
    .padding(.vertical, 9)
    .background(.bar)
  }

  private var subtitle: String {
    if buffer.kind == "status" {
      return [network?.host, network?.status].compactMap(\.self).joined(separator: " • ")
    }
    return buffer.topic?.isEmpty == false ? buffer.topic! : network?.name ?? ""
  }
}

private struct TimelineView: View {
  @Environment(AppModel.self) private var model
  let buffer: Buffer

  var body: some View {
    ScrollViewReader { proxy in
      ScrollView {
        LazyVStack(spacing: 0) {
          if model.historyLoading.contains(buffer.id) {
            ProgressView()
              .controlSize(.small)
              .padding(10)
          }
          ForEach(items) { item in
            switch item {
            case .day(let id, let title):
              DaySeparator(title: title).id(id)
            case .unread(let id):
              UnreadSeparator().id(id)
            case .message(let message):
              MessageRow(message: message, buffer: buffer).id(message.id)
                .onAppear {
                  if message.id == model.selectedMessages.first?.id {
                    model.loadOlderHistory()
                  }
                }
            case .presence(let id, let messages):
              PresenceSummary(messages: messages).id(id)
            }
          }
        }
        .padding(.vertical, 5)
      }
      .defaultScrollAnchor(.bottom)
      .onChange(of: model.selectedMessages.last?.id) { old, new in
        guard old != nil, let new else { return }
        withAnimation(.snappy(duration: 0.18)) {
          proxy.scrollTo(new, anchor: .bottom)
        }
      }
    }
    // Rebuild the scroll container per buffer so switching channels always
    // re-applies the bottom anchor and lands at the end of the backlog.
    .id(buffer.id)
    .background(Color.lurkerTimelineBackground)
  }

  private var items: [TimelineItem] {
    var result: [TimelineItem] = []
    var lastDay: String?
    var presence: [Message] = []

    func flushPresence() {
      guard !presence.isEmpty else { return }
      if buffer.collapsePresenceEvents, presence.count > 1 {
        result.append(.presence(presence[0].id, presence))
      } else {
        result.append(contentsOf: presence.map(TimelineItem.message))
      }
      presence.removeAll(keepingCapacity: true)
    }

    for message in model.selectedMessages {
      let day = dayKey(message.ts)
      if day != lastDay {
        flushPresence()
        result.append(.day("day-\(day)", displayDay(message.ts)))
        lastDay = day
      }
      if model.markerAnchors[buffer.id] == message.id {
        flushPresence()
        result.append(.unread("unread-\(message.id.uuidString)"))
      }
      if isPresence(message) {
        presence.append(message)
      } else {
        flushPresence()
        result.append(.message(message))
      }
    }
    flushPresence()
    return result
  }
}

private enum TimelineItem: Identifiable {
  case day(String, String)
  case unread(String)
  case message(Message)
  case presence(UUID, [Message])

  var id: String {
    switch self {
    case .day(let id, _), .unread(let id): id
    case .message(let message): message.id.uuidString
    case .presence(let id, _): "presence-\(id.uuidString)"
    }
  }
}

private struct DaySeparator: View {
  let title: String

  var body: some View {
    HStack {
      line
      Text(title)
        .font(.footnote.weight(.medium))
        .foregroundStyle(.secondary)
      line
    }
    .padding(.horizontal, 14)
    .padding(.vertical, 9)
    .accessibilityElement(children: .combine)
  }

  private var line: some View {
    Rectangle()
      .fill(Color.lurkerSeparator)
      .frame(height: 1)
  }
}

private struct UnreadSeparator: View {
  var body: some View {
    HStack {
      Rectangle().frame(height: 1)
      Text("New Messages")
        .font(.footnote.weight(.semibold))
      Rectangle().frame(height: 1)
    }
    .foregroundStyle(.orange)
    .padding(.horizontal, 14)
    .padding(.vertical, 5)
  }
}

private struct PresenceSummary: View {
  let messages: [Message]
  @State private var expanded = false

  var body: some View {
    DisclosureGroup(isExpanded: $expanded) {
      ForEach(messages) { message in
        MessageRow(message: message, buffer: nil)
      }
    } label: {
      Label(summary, systemImage: "person.2.wave.2")
        .font(.footnote.monospaced())
        .foregroundStyle(.secondary)
        .padding(.vertical, 3)
    }
    .padding(.horizontal, 14)
  }

  private var summary: String {
    if let split = messages.compactMap(\.netsplit).first {
      return "\(messages.count) users affected by netsplit \(split.serverA) ↔ \(split.serverB)"
    }
    let kinds = Dictionary(grouping: messages, by: \.kind).mapValues(\.count)
    return kinds.sorted(by: { $0.key < $1.key })
      .map { "\($0.value) \($0.key)" }
      .joined(separator: " • ")
  }
}

private struct MessageRow: View {
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
        }
      }
      .accessibilityElement(children: .combine)
      .accessibilityLabel("\(message.sender), \(displayTime(message.ts)), \(message.content)")
  }

  // macOS: aligned gutter columns (time, sender, body). iOS: those columns eat
  // ~160pt of a 402pt screen, so the nick moves to its own line above the body
  // with the timestamp trailing it, and the body wraps at full width.
  @ViewBuilder private var layout: some View {
    #if os(macOS)
      HStack(alignment: .firstTextBaseline, spacing: 8) {
        timestamp
          .frame(width: 42, alignment: .trailing)
        if message.displayKind == "sys" {
          systemIcon
            .frame(width: 14)
          systemBody
        } else {
          Text(message.sender)
            .font(.body.monospaced().weight(message.isSelf == true ? .bold : .medium))
            .foregroundStyle(nickColor(message.senderColor))
            .frame(width: 104, alignment: .trailing)
            .lineLimit(1)
            .help(message.userhost ?? message.sender)
          VStack(alignment: .leading, spacing: 6) {
            messageBody
            embeds
          }
        }
        Spacer(minLength: 4)
      }
    #else
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
            Text(message.sender)
              .font(.body.monospaced().weight(message.isSelf == true ? .bold : .semibold))
              .foregroundStyle(nickColor(message.senderColor))
              .lineLimit(1)
            Spacer(minLength: 4)
            timestamp
          }
          messageBody
          embeds
        }
      }
    #endif
  }

  private var timestamp: some View {
    Text(displayTime(message.ts))
      .font(.caption.monospacedDigit())
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
      .font(.body.monospaced())
      .foregroundStyle(.secondary)
      .textSelection(.enabled)
  }

  private var messageBody: some View {
    Text(attributedBody(message))
      .font(.body.monospaced())
      .foregroundStyle(message.displayKind == "action" ? .purple : .primary)
      .textSelection(.enabled)
  }

  @ViewBuilder private var embeds: some View {
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
    switch message.kind {
    case "join": "arrow.right"
    case "part", "quit": "arrow.left"
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

  private var systemText: String {
    let target = message.target ?? ""
    switch message.kind {
    case "away":
      return message.content.isEmpty
        ? "\(message.sender) is away" : "\(message.sender) is away (\(message.content))"
    case "back":
      return "\(message.sender) is back"
    case "nick" where !target.isEmpty:
      return "\(message.sender) is now known as \(target)"
    case "account":
      return message.content.isEmpty
        ? "\(message.sender) logged out" : "\(message.sender) logged in as \(message.content)"
    case "chghost":
      return "\(message.sender) changed host to \(message.content)"
    default:
      return [message.sender, message.content.isEmpty ? message.kind : message.content]
        .filter { !$0.isEmpty }
        .joined(separator: " ")
    }
  }
}

private struct PreviewCard: View {
  @Environment(AppModel.self) private var model
  let preview: Preview

  @ViewBuilder
  var body: some View {
    if let destination = URL(string: preview.url) {
      Link(destination: destination) {
        card
      }
      .buttonStyle(.plain)
      #if os(macOS)
        .pointerStyle(.link)
      #endif
    } else {
      card
    }
  }

  private var card: some View {
    HStack(spacing: 10) {
      if let imageURL = model.previewImageURL(preview) {
        AsyncImage(url: imageURL) { image in
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
          .font(.footnote)
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
}

private struct ComposerView: View {
  @Environment(AppModel.self) private var model
  let buffer: Buffer
  @FocusState private var focused: Bool

  var body: some View {
    @Bindable var model = model
    VStack(alignment: .leading, spacing: 4) {
      if let error = model.composerError {
        Text(error)
          .font(.footnote)
          .foregroundStyle(.red)
          .transition(.move(edge: .bottom).combined(with: .opacity))
      }
      HStack(alignment: .bottom, spacing: 8) {
        Text(model.selectedNetwork?.nick ?? "you")
          .font(.body.monospaced().weight(.semibold))
          .foregroundStyle(.white)
        //          .padding(.bottom, 6)
        TextField(placeholder, text: $model.composerText, axis: .vertical)
          .textFieldStyle(.plain)
          .font(.body.monospaced())
          .lineLimit(1...5)
          .focused($focused)
          .onSubmit(model.sendComposer)
          .disabled(!canSend)
        Button(action: model.sendComposer) {
          Image(systemName: "arrow.up.circle.fill")
            .font(.title2)
        }
        .buttonStyle(.plain)
        .disabled(
          !canSend || model.composerText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        )
        .help("Send message")
      }
      .padding(.horizontal, 10)
      .padding(.vertical, 7)
      .background(Color.lurkerControlBackground, in: .rect(cornerRadius: 8))
      .overlay {
        RoundedRectangle(cornerRadius: 8)
          .stroke(
            focused ? Color.accentColor : Color.lurkerSeparator,
            lineWidth: focused ? 1.5 : 0.5)
      }
    }
    .padding(10)
    .background(.bar)
    .onChange(of: model.focusComposerRequest) { _, _ in focused = true }
    #if os(macOS)
      // Autofocus on buffer switch is desktop-only: on iOS programmatic focus
      // raises the software keyboard over the timeline the user came to read.
      .onChange(of: buffer.id) { _, _ in focused = true }
    #endif
  }

  private var canSend: Bool {
    model.connectionState == .connected && buffer.kind != "status"
      && (buffer.kind != "channel" || buffer.joined)
  }

  private var placeholder: String {
    if !canSend {
      return model.connectionState == .connected
        ? "This conversation is read-only" : "Waiting for connection…"
    }
    return "\(buffer.name)"
  }
}

private func isPresence(_ message: Message) -> Bool {
  presenceKinds.contains(message.kind)
}

private func nickColor(_ index: Int?) -> Color {
  guard let index else { return .primary }
  return Color(hue: Double((index * 137) % 360) / 360, saturation: 0.58, brightness: 0.78)
}

@MainActor
func attributedBody(_ message: Message) -> AttributedString {
  var result = AttributedString()
  let segments =
    message.segments?.isEmpty == false ? message.segments! : [MircSegment(text: message.content)]
  let plainText = segments.map(\.text).joined()
  for segment in segments {
    var value = AttributedString(segment.text)
    if segment.bold == true { value.font = .body.monospaced().bold() }
    if segment.italic == true { value.font = .body.monospaced().italic() }
    if segment.underline == true { value.underlineStyle = .single }
    if segment.strike == true { value.strikethroughStyle = .single }
    if let foreground = segment.fg { value.foregroundColor = mircColor(foreground) }
    result.append(value)
  }
  if let detector = TimelineFormatters.linkDetector {
    for match in detector.matches(
      in: plainText, range: NSRange(plainText.startIndex..., in: plainText))
    {
      guard let url = match.url,
        let stringRange = Range(match.range, in: plainText),
        let attributedRange = Range(stringRange, in: result)
      else {
        continue
      }
      result[attributedRange].link = url
      result[attributedRange].foregroundColor = Color.lurkerLink
      result[attributedRange].underlineStyle = .single
    }
  }
  return result
}

private func mircColor(_ value: Int) -> Color {
  let palette: [Color] = [
    .white, .black, .blue, .green, .red, .brown, .purple, .orange,
    .yellow, .green, .teal, .cyan, .blue, .pink, .gray, .secondary,
  ]
  return palette.indices.contains(value) ? palette[value] : .primary
}

private func dayKey(_ raw: String) -> String {
  String(raw.prefix(10))
}

@MainActor
private func displayDay(_ raw: String) -> String {
  guard let date = parseTimestamp(raw) else { return dayKey(raw) }
  return date.formatted(.dateTime.weekday(.wide).month(.wide).day().year())
}

@MainActor
private func displayTime(_ raw: String) -> String {
  guard let date = parseTimestamp(raw) else {
    return String(raw.dropFirst(11).prefix(5))
  }
  return date.formatted(date: .omitted, time: .shortened)
}

@MainActor
private func parseTimestamp(_ raw: String) -> Date? {
  if let date = try? Date(raw, strategy: .iso8601) {
    return date
  }
  return TimelineFormatters.iso8601.date(from: raw)
}

@MainActor
private enum TimelineFormatters {
  static let linkDetector = try? NSDataDetector(
    types: NSTextCheckingResult.CheckingType.link.rawValue)
  static let iso8601 = ISO8601DateFormatter()
}

#Preview {
  ConversationView()
    .environment(AppModel.preview())
    .tint(.mint)
    #if os(macOS)
      .frame(width: 700, height: 600)
    #endif
}
